// Package httpapi は全銀フォーマットマスタデータを提供するHTTP APIハンドラを定義する。
package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/growstar-jp/zengin-api/internal/zengindata"
)

// NewHandler は zengindata.Store を参照する http.Handler を組み立てる。
func NewHandler(store *zengindata.Store, logger *slog.Logger) http.Handler {
	h := &handlers{store: store, logger: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /v1/banks", h.listBanks)
	mux.HandleFunc("GET /v1/banks/{code}", h.getBank)
	mux.HandleFunc("GET /v1/banks/{code}/branches", h.listBranches)
	mux.HandleFunc("GET /v1/banks/{code}/branches/{branchCode}", h.getBranch)
	mux.HandleFunc("GET /v1/branches", h.listAllBranches)

	// ドキュメント/ディスカバリ用エンドポイント。別ワークスペースのREADMEを読みに行かなくても、
	// このAPI自身の情報だけで連携アプリを実装できるようにする(OpenAPI仕様・人間向けdocs・
	// AIエージェント向けのconcepts/guides/examples)。
	mux.HandleFunc("GET /openapi.json", h.asset("assets/openapi.json", "application/json; charset=utf-8"))
	mux.HandleFunc("GET /docs", h.asset("assets/docs.html", "text/html; charset=utf-8"))
	mux.HandleFunc("GET /ai", h.asset("assets/ai/index.json", "application/json; charset=utf-8"))
	mux.HandleFunc("GET /ai/concepts", h.asset("assets/ai/concepts.json", "application/json; charset=utf-8"))
	mux.HandleFunc("GET /ai/guides", h.asset("assets/ai/guides.json", "application/json; charset=utf-8"))
	mux.HandleFunc("GET /ai/examples", h.asset("assets/ai/examples.json", "application/json; charset=utf-8"))

	return withLogging(logger, withCORS(mux))
}

// withCORS は読み取り専用の公開マスタデータAPIとして、どのOriginからのGETも許可する。
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
