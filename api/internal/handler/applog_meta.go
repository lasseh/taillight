package handler

import (
	"net/http"
)

// AppLogMetaHandler handles REST metadata endpoints for log event filter UI dropdowns.
type AppLogMetaHandler struct {
	store AppLogStore
}

// NewAppLogMetaHandler creates a new AppLogMetaHandler.
func NewAppLogMetaHandler(store AppLogStore) *AppLogMetaHandler {
	return &AppLogMetaHandler{store: store}
}

// Services handles GET /api/v1/applog/meta/services.
func (h *AppLogMetaHandler) Services(w http.ResponseWriter, r *http.Request) {
	services, err := h.store.ListServices(r.Context())
	if err != nil {
		LoggerFromContext(r.Context()).Error("list services failed", "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to list services")
		return
	}
	writeJSON(w, itemResponse{Data: emptySlice(services)})
}

// Hosts handles GET /api/v1/applog/meta/hosts.
func (h *AppLogMetaHandler) Hosts(w http.ResponseWriter, r *http.Request) {
	hosts, err := h.store.ListAppLogHosts(r.Context())
	if err != nil {
		LoggerFromContext(r.Context()).Error("list applog hosts failed", "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to list hosts")
		return
	}
	writeJSON(w, itemResponse{Data: emptySlice(hosts)})
}

// Components handles GET /api/v1/applog/meta/components.
func (h *AppLogMetaHandler) Components(w http.ResponseWriter, r *http.Request) {
	components, err := h.store.ListComponents(r.Context())
	if err != nil {
		LoggerFromContext(r.Context()).Error("list components failed", "err", err)
		writeError(w, http.StatusInternalServerError, "query_failed", "failed to list components")
		return
	}
	writeJSON(w, itemResponse{Data: emptySlice(components)})
}
