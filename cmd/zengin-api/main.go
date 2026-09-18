// zengin-api は全銀フォーマット（金融機関・支店コード）マスタデータをJSON APIとして提供する。
//
// サブコマンド:
//   - serve       : HTTP APIサーバーを起動する（常駐プロセス）
//   - update       : zengin-code/source-data から最新データを取得してディスクに書き出し、終了する
//   - healthcheck : /healthz を1回だけ叩いて終了する（Dockerヘルスチェック用、curl不要）
//
// 定期実行(update)はアプリ自身のcronで持たず、systemd timer 等の外側のスケジューラから
// `zengin-api update` を都度キックする前提にしている(SPEC参照)。serveプロセスは別プロセスが
// 書き換えたデータをRELOAD_INTERVALごとにポーリングして取り込む。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/growstar-jp/zengin-api/internal/httpapi"
	"github.com/growstar-jp/zengin-api/internal/logging"
	"github.com/growstar-jp/zengin-api/internal/zengindata"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: zengin-api <serve|update|healthcheck>")
		os.Exit(2)
	}

	logger := logging.New(logging.ParseLevel(os.Getenv("LOG_LEVEL")))

	var err error
	switch os.Args[1] {
	case "serve":
		err = runServe(logger)
	case "update":
		err = runUpdate(logger)
	case "healthcheck":
		err = runHealthcheck()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[1])
		os.Exit(2)
	}

	if err != nil {
		logger.Error("終了しました", "error", err)
		os.Exit(1)
	}
}

func runServe(logger *slog.Logger) error {
	dataDir := envOr("DATA_DIR", "./data")
	reloadInterval := envDurationOr("RELOAD_INTERVAL", 5*time.Minute)

	store := zengindata.NewStore(dataDir)
	if err := store.Reload(); err != nil {
		logger.Warn("初回データ読み込みに失敗しました。updateの実行を待ちます", "error", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go zengindata.WatchReload(ctx, store, reloadInterval, logger)

	handler := httpapi.NewHandler(store, logger)

	listener, cleanup, err := newListener(logger)
	if err != nil {
		return fmt.Errorf("リスナー作成に失敗しました: %w", err)
	}
	defer cleanup()

	server := &http.Server{Handler: handler}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("サーバーを起動しました", "addr", listener.Addr().String())
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		logger.Info("シャットダウンします")
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("サーバーエラー: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

// newListener は SOCKET_PATH が設定されていればUNIXドメインソケット、
// なければ ADDR (デフォルト :8080) のTCPソケットでリッスンする。
//
// UNIXソケットは Caddy 側が別uidで読み書きする前提のため 0666 にする
// (growstar-docker-deploy skill の caddy-network-and-persistence.md 参照)。
func newListener(logger *slog.Logger) (net.Listener, func(), error) {
	if socketPath := os.Getenv("SOCKET_PATH"); socketPath != "" {
		if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("既存ソケットの削除に失敗しました: %w", err)
		}
		l, err := net.Listen("unix", socketPath)
		if err != nil {
			return nil, nil, err
		}
		if err := os.Chmod(socketPath, 0o666); err != nil {
			l.Close()
			return nil, nil, fmt.Errorf("ソケットのパーミッション設定に失敗しました: %w", err)
		}
		cleanup := func() {
			l.Close()
			os.Remove(socketPath)
		}
		return l, cleanup, nil
	}

	addr := envOr("ADDR", ":8080")
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	return l, func() { l.Close() }, nil
}

func runUpdate(logger *slog.Logger) error {
	dataDir := envOr("DATA_DIR", "./data")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	result, err := zengindata.Update(ctx, dataDir)
	if err != nil {
		return err
	}

	logger.Info("データ更新が完了しました",
		"bank_count", result.BankCount,
		"branch_file_count", result.BranchFileCount,
		"branch_count", result.BranchCount,
		"source_updated_at", result.SourceUpdatedAt,
	)
	return nil
}

func runHealthcheck() error {
	client := &http.Client{Timeout: 3 * time.Second}
	url := "http://healthcheck/healthz"

	if socketPath := os.Getenv("SOCKET_PATH"); socketPath != "" {
		client.Transport = &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		}
	} else {
		url = "http://localhost" + envOr("ADDR", ":8080") + "/healthz"
	}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("healthcheckリクエストに失敗しました: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheckが異常ステータスを返しました: %s", resp.Status)
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envDurationOr(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
