package handler

import (
	"context"
	"net/http"

	"github.com/lasseh/taillight/internal/model"
)

// JuniperStore defines the data access interface for Juniper reference lookups.
type JuniperStore interface {
	LookupJuniperRef(ctx context.Context, name string) ([]model.JuniperNetlogRef, error)
}

// JuniperHandler handles Juniper syslog reference lookups.
type JuniperHandler struct {
	store JuniperStore
}

// NewJuniperHandler creates a new JuniperHandler.
func NewJuniperHandler(store JuniperStore) *JuniperHandler {
	return &JuniperHandler{store: store}
}

// Lookup handles GET /api/v1/juniper/lookup?name=...
func (h *JuniperHandler) Lookup(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing_name", "query parameter 'name' is required")
		return
	}

	refs, err := h.store.LookupJuniperRef(r.Context(), name)
	if err != nil {
		if isClientGone(r) {
			return
		}
		LoggerFromContext(r.Context()).Error("lookup juniper ref failed", "name", name, "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to lookup juniper reference")
		return
	}
	writeJSON(w, itemResponse{Data: emptySlice(refs)})
}
