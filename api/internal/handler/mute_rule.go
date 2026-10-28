package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/auth"
	"github.com/lasseh/taillight/internal/mute"
)

// MuteRuleStore defines the data access interface for mute rule CRUD.
type MuteRuleStore interface {
	ListMuteRules(ctx context.Context, expiresAfter time.Time) ([]mute.Rule, error)
	CreateMuteRule(ctx context.Context, r mute.Rule) (mute.Rule, error)
	SetMuteRuleExpiry(ctx context.Context, id int64, at time.Time) (mute.Rule, error)
	EndMuteRule(ctx context.Context, id int64, at time.Time) error
}

// MuteReloader reloads the in-memory mute rule set after a write.
type MuteReloader interface {
	Reload(ctx context.Context) error
}

// muteReloadTimeout bounds the post-write reload.
const muteReloadTimeout = 5 * time.Second

// MuteRuleHandler handles REST endpoints for mute rules.
type MuteRuleHandler struct {
	store MuteRuleStore
	mutes MuteReloader
	now   func() time.Time
}

// NewMuteRuleHandler creates a MuteRuleHandler. now is the clock expiries
// are computed from; the server passes time.Now.
func NewMuteRuleHandler(store MuteRuleStore, mutes MuteReloader, now func() time.Time) *MuteRuleHandler {
	return &MuteRuleHandler{store: store, mutes: mutes, now: now}
}

// muteRuleCreate is the POST body. It is a dedicated type so that id,
// created_by and expires_at sent by a client are dropped, and so that an
// unknown duration key cannot silently become the default.
type muteRuleCreate struct {
	Feed        string `json:"feed"`
	Hostname    string `json:"hostname"`
	Programname string `json:"programname"`
	Search      string `json:"search"`
	Reason      string `json:"reason"`
	DurationMS  int64  `json:"duration_ms"`
}

// muteRuleExpiry is the PATCH body.
type muteRuleExpiry struct {
	DurationMS int64 `json:"duration_ms"`
}

// List handles GET /api/v1/mute-rules: active rules plus rules that ended in
// the last 7 days, newest expiry first.
func (h *MuteRuleHandler) List(w http.ResponseWriter, r *http.Request) {
	rules, err := h.store.ListMuteRules(r.Context(), h.now().Add(-mute.ExpiredListWindow))
	if err != nil {
		LoggerFromContext(r.Context()).Error("list mute rules", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list mute rules")
		return
	}
	writeJSON(w, itemResponse{Data: emptySlice(rules)})
}

// Create handles POST /api/v1/mute-rules. The creator comes from the
// authenticated user and the expiry is computed on the server.
func (h *MuteRuleHandler) Create(w http.ResponseWriter, r *http.Request) {
	var in muteRuleCreate
	if !decodeMuteBody(w, r, &in) {
		return
	}

	expiresAt, err := mute.ExpiresAt(h.now(), in.DurationMS)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}
	rule := mute.Rule{
		Feed:        mute.Feed(strings.TrimSpace(in.Feed)),
		Hostname:    strings.TrimSpace(in.Hostname),
		Programname: strings.TrimSpace(in.Programname),
		Search:      strings.TrimSpace(in.Search),
		Reason:      strings.TrimSpace(in.Reason),
		CreatedBy:   auth.UserFromContext(r.Context()).Username,
		ExpiresAt:   expiresAt,
	}
	if err := rule.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	created, err := h.store.CreateMuteRule(r.Context(), rule)
	if err != nil {
		LoggerFromContext(r.Context()).Error("create mute rule", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create mute rule")
		return
	}
	h.reload(r)
	writeJSONStatus(w, http.StatusCreated, itemResponse{Data: created})
}

// ExpireIn handles PATCH /api/v1/mute-rules/{id}: expires_at = now +
// duration_ms. It extends or shortens an active rule and revives an ended one.
func (h *MuteRuleHandler) ExpireIn(w http.ResponseWriter, r *http.Request) {
	id, ok := parseMuteRuleID(w, r)
	if !ok {
		return
	}
	var in muteRuleExpiry
	if !decodeMuteBody(w, r, &in) {
		return
	}
	expiresAt, err := mute.ExpiresAt(h.now(), in.DurationMS)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	updated, err := h.store.SetMuteRuleExpiry(r.Context(), id, expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "mute rule not found")
		return
	}
	if err != nil {
		LoggerFromContext(r.Context()).Error("set mute rule expiry", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to update mute rule")
		return
	}
	h.reload(r)
	writeJSON(w, itemResponse{Data: updated})
}

// Unmute handles DELETE /api/v1/mute-rules/{id}. It ends the rule now and
// keeps the row; a repeat call is a no-op that still returns 204.
func (h *MuteRuleHandler) Unmute(w http.ResponseWriter, r *http.Request) {
	id, ok := parseMuteRuleID(w, r)
	if !ok {
		return
	}
	err := h.store.EndMuteRule(r.Context(), id, h.now())
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "mute rule not found")
		return
	}
	if err != nil {
		LoggerFromContext(r.Context()).Error("end mute rule", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to unmute")
		return
	}
	h.reload(r)
	w.WriteHeader(http.StatusNoContent)
}

// reload refreshes the rule set after a committed write. The context is
// detached so a client abort or the route timeout cannot cancel it. A
// failure is logged only: the row is stored and the 30s tick catches up.
func (h *MuteRuleHandler) reload(r *http.Request) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), muteReloadTimeout)
	defer cancel()
	if err := h.mutes.Reload(ctx); err != nil {
		LoggerFromContext(r.Context()).Warn("reload mute rules after write", "err", err)
	}
}

func parseMuteRuleID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be an integer")
		return 0, false
	}
	return id, true
}

// decodeMuteBody reads a size-capped JSON body into v, writing the 400 on
// failure.
func decodeMuteBody(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, requestBodyLimit))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_error", "failed to read request body")
		return false
	}
	if err := json.Unmarshal(body, v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "malformed JSON body")
		return false
	}
	return true
}
