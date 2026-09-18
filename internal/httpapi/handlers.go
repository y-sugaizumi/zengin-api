package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/growstar-jp/zengin-api/internal/zengindata"
)

type handlers struct {
	store  *zengindata.Store
	logger *slog.Logger
}

func (h *handlers) healthz(w http.ResponseWriter, r *http.Request) {
	if !h.store.Ready() {
		writeError(w, h.logger, http.StatusServiceUnavailable, "データ未取得です")
		return
	}
	writeJSON(w, h.logger, http.StatusOK, map[string]string{
		"status":            "ok",
		"source_updated_at": h.store.UpdatedAt(),
	})
}

// listBanks: GET /v1/banks?q=
func (h *handlers) listBanks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	writeJSON(w, h.logger, http.StatusOK, h.store.ListBanks(q))
}

// getBank: GET /v1/banks/{code}
func (h *handlers) getBank(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	bank, ok := h.store.GetBank(code)
	if !ok {
		writeError(w, h.logger, http.StatusNotFound, "金融機関が見つかりません: "+code)
		return
	}
	writeJSON(w, h.logger, http.StatusOK, bank)
}

// listBranches: GET /v1/banks/{code}/branches?q=
func (h *handlers) listBranches(w http.ResponseWriter, r *http.Request) {
	bankCode := r.PathValue("code")
	if _, ok := h.store.GetBank(bankCode); !ok {
		writeError(w, h.logger, http.StatusNotFound, "金融機関が見つかりません: "+bankCode)
		return
	}
	q := r.URL.Query().Get("q")
	writeJSON(w, h.logger, http.StatusOK, h.store.ListBranches(bankCode, q))
}

// getBranch: GET /v1/banks/{code}/branches/{branchCode}
func (h *handlers) getBranch(w http.ResponseWriter, r *http.Request) {
	bankCode := r.PathValue("code")
	branchCode := r.PathValue("branchCode")
	branch, ok := h.store.GetBranch(bankCode, branchCode)
	if !ok {
		writeError(w, h.logger, http.StatusNotFound, "支店が見つかりません: "+bankCode+"/"+branchCode)
		return
	}
	writeJSON(w, h.logger, http.StatusOK, branch)
}

// listAllBranches: GET /v1/branches?bank_code=&q=
func (h *handlers) listAllBranches(w http.ResponseWriter, r *http.Request) {
	bankCode := r.URL.Query().Get("bank_code")
	q := r.URL.Query().Get("q")
	writeJSON(w, h.logger, http.StatusOK, h.store.ListBranches(bankCode, q))
}
