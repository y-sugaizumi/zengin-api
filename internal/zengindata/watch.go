package zengindata

import (
	"context"
	"log/slog"
	"time"
)

// WatchReload は interval ごとに Store.Reload を呼び直すバックグラウンドループを起動する。
//
// update サブコマンドは serve プロセスとは別プロセス(systemd timer 起動)として動くため、
// 同一プロセス内シグナルでの即時反映はできない。その代わりディスク上の banks.json/branches/*.json
// を定期的に読み直すことで、別プロセスが書き換えたデータを取り込む。zengin-code のデータは
// 日次更新程度の頻度なので、数分間隔のポーリングで実用上十分。
func WatchReload(ctx context.Context, store *Store, interval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := store.Reload(); err != nil {
				logger.Error("データ再読み込みに失敗しました", "error", err)
				continue
			}
			logger.Debug("データを再読み込みしました")
		}
	}
}
