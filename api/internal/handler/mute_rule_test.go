package handler

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/auth"
	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/mute"
)

var muteT0 = time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)

// fakeMuteStore is an in-memory MuteRuleStore that follows the SQL: list
// filters expires_at > after and orders expires_at DESC, id DESC; end keeps
// the earlier of the two times; unknown ids give pgx.ErrNoRows.
type fakeMuteStore struct {
	rules       []mute.Rule
	nextID      int64
	listErr     error
	listedAfter time.Time
}

func (f *fakeMuteStore) ListMuteRules(_ context.Context, after time.Time) ([]mute.Rule, error) {
	f.listedAfter = after
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []mute.Rule
	for _, r := range f.rules {
		if r.ExpiresAt.After(after) {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b mute.Rule) int {
		if c := b.ExpiresAt.Compare(a.ExpiresAt); c != 0 {
			return c
		}
		return cmp.Compare(b.ID, a.ID)
	})
	return out, nil
}

func (f *fakeMuteStore) CreateMuteRule(_ context.Context, r mute.Rule) (mute.Rule, error) {
	f.nextID++
	r.ID = f.nextID
	r.CreatedAt = muteT0.Add(-time.Minute)
	f.rules = append(f.rules, r)
	return r, nil
}

func (f *fakeMuteStore) find(id int64) *mute.Rule {
	for i := range f.rules {
		if f.rules[i].ID == id {
			return &f.rules[i]
		}
	}
	return nil
}

func (f *fakeMuteStore) SetMuteRuleExpiry(_ context.Context, id int64, at time.Time) (mute.Rule, error) {
	r := f.find(id)
	if r == nil {
		return mute.Rule{}, pgx.ErrNoRows
	}
	r.ExpiresAt = at
	return *r, nil
}

func (f *fakeMuteStore) EndMuteRule(_ context.Context, id int64, at time.Time) error {
	r := f.find(id)
	if r == nil {
		return pgx.ErrNoRows
	}
	if at.Before(r.ExpiresAt) {
		r.ExpiresAt = at
	}
	return nil
}

// muteFixture wires the handler to a fake store and a real Set over that
// store, both on one movable clock, behind a router that injects user lasse.
type muteFixture struct {
	store  *fakeMuteStore
	set    *mute.Set
	now    time.Time
	router chi.Router
}

func newMuteFixture() *muteFixture {
	f := &muteFixture{store: &fakeMuteStore{}, now: muteT0}
	clock := func() time.Time { return f.now }
	f.set = mute.NewSet(f.store, clock, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewMuteRuleHandler(f.store, f.set, clock)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			user := &model.User{Username: "lasse", IsAdmin: true, IsActive: true}
			next.ServeHTTP(w, req.WithContext(auth.WithUser(req.Context(), user)))
		})
	})
	r.Get("/api/v1/mute-rules", h.List)
	r.Post("/api/v1/mute-rules", h.Create)
	r.Patch("/api/v1/mute-rules/{id}", h.ExpireIn)
	r.Delete("/api/v1/mute-rules/{id}", h.Unmute)
	f.router = r
	return f
}

func (f *muteFixture) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), method, "/api/v1/mute-rules"+path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func decodeMuteRule(t *testing.T, rec *httptest.ResponseRecorder) mute.Rule {
	t.Helper()
	var resp struct {
		Data mute.Rule `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return resp.Data
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
	}
	return resp.Error.Code
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, status, rec.Body.String())
	}
}

// matchX returns the rule id the set marks a netlog event from host x with.
func (f *muteFixture) matchX() *int64 {
	return f.set.Active().MatchNetlog(model.NetlogEvent{Hostname: "x", Message: "m"})
}

const spaBody = `{"feed":"netlog","hostname":"x","reason":"r","duration_ms":3600000}`

func TestMuteRuleCreate_SPABody(t *testing.T) {
	f := newMuteFixture()
	rec := f.do(t, http.MethodPost, "", spaBody)
	wantStatus(t, rec, http.StatusCreated)

	got := decodeMuteRule(t, rec)
	if got.ID != 1 || got.Feed != mute.FeedNetlog || got.Hostname != "x" || got.Reason != "r" || got.CreatedBy != "lasse" {
		t.Errorf("created rule = %+v", got)
	}
	if want := muteT0.Add(time.Hour); !got.ExpiresAt.Equal(want) {
		t.Errorf("expires_at = %v, want %v", got.ExpiresAt, want)
	}
	// The write reloaded the set: the rule marks events right away.
	if id := f.matchX(); id == nil || *id != 1 {
		t.Fatalf("set match after create = %v, want 1", id)
	}
}

func TestMuteRuleCreate_OmittedDurationIs24h(t *testing.T) {
	f := newMuteFixture()
	rec := f.do(t, http.MethodPost, "", `{"feed":"srvlog","programname":"cron","reason":"noisy"}`)
	wantStatus(t, rec, http.StatusCreated)
	if got := decodeMuteRule(t, rec).ExpiresAt; !got.Equal(muteT0.Add(24 * time.Hour)) {
		t.Fatalf("expires_at = %v, want now+24h", got)
	}
}

func TestMuteRuleCreate_Rejects(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode string
	}{
		{name: "duration over 7d", body: `{"feed":"netlog","hostname":"x","reason":"r","duration_ms":604800001}`, wantCode: "validation_failed"},
		{name: "negative duration", body: `{"feed":"netlog","hostname":"x","reason":"r","duration_ms":-1}`, wantCode: "validation_failed"},
		{name: "no match field", body: `{"feed":"netlog","reason":"r"}`, wantCode: "validation_failed"},
		{name: "star-only hostname", body: `{"feed":"netlog","hostname":" * ","reason":"r"}`, wantCode: "validation_failed"},
		{name: "blank reason", body: `{"feed":"netlog","hostname":"x","reason":"  "}`, wantCode: "validation_failed"},
		{name: "bad feed", body: `{"feed":"applog","hostname":"x","reason":"r"}`, wantCode: "validation_failed"},
		{name: "hostname over 500 bytes", body: `{"feed":"netlog","hostname":"` + strings.Repeat("h", 501) + `","reason":"r"}`, wantCode: "validation_failed"},
		{name: "malformed json", body: `{"feed":`, wantCode: "invalid_json"},
		{name: "duration as a string", body: `{"feed":"netlog","hostname":"x","reason":"r","duration_ms":"1h"}`, wantCode: "invalid_json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newMuteFixture()
			rec := f.do(t, http.MethodPost, "", tt.body)
			wantStatus(t, rec, http.StatusBadRequest)
			if code := errorCode(t, rec); code != tt.wantCode {
				t.Errorf("error code = %q, want %q", code, tt.wantCode)
			}
			if len(f.store.rules) != 0 {
				t.Errorf("rejected request stored %+v", f.store.rules)
			}
		})
	}
}

func TestMuteRuleCreate_IgnoresClientIDCreatorAndExpiry(t *testing.T) {
	f := newMuteFixture()
	body := `{"id":99,"feed":"netlog","hostname":"x","reason":"r","created_by":"mallory",` +
		`"created_at":"2020-01-01T00:00:00Z","expires_at":"2030-01-01T00:00:00Z"}`
	rec := f.do(t, http.MethodPost, "", body)
	wantStatus(t, rec, http.StatusCreated)

	got := decodeMuteRule(t, rec)
	if got.ID != 1 {
		t.Errorf("id = %d, want 1 (store-assigned)", got.ID)
	}
	if got.CreatedBy != "lasse" {
		t.Errorf("created_by = %q, want the authenticated user", got.CreatedBy)
	}
	if !got.ExpiresAt.Equal(muteT0.Add(24 * time.Hour)) {
		t.Errorf("expires_at = %v, want server now+24h", got.ExpiresAt)
	}
	if !got.CreatedAt.Equal(muteT0.Add(-time.Minute)) {
		t.Errorf("created_at = %v, want the store's", got.CreatedAt)
	}
}

func TestMuteRuleCreate_TrimsStrings(t *testing.T) {
	f := newMuteFixture()
	rec := f.do(t, http.MethodPost, "",
		`{"feed":" netlog ","hostname":"  x ","programname":" rpd\t","search":" ae30 ","reason":"  known flap "}`)
	wantStatus(t, rec, http.StatusCreated)

	got := f.store.rules[0]
	if got.Feed != mute.FeedNetlog || got.Hostname != "x" || got.Programname != "rpd" || got.Search != "ae30" || got.Reason != "known flap" {
		t.Fatalf("stored rule = %+v, want every string trimmed", got)
	}
}

func TestMuteRuleCreate_ReloadFailureStill201(t *testing.T) {
	f := newMuteFixture()
	f.store.listErr = errors.New("db down")
	rec := f.do(t, http.MethodPost, "", spaBody)
	wantStatus(t, rec, http.StatusCreated)
	if len(f.store.rules) != 1 {
		t.Fatalf("stored %d rules, want 1", len(f.store.rules))
	}
}

func TestMuteRuleExpireIn_RetimesAndRevives(t *testing.T) {
	f := newMuteFixture()
	wantStatus(t, f.do(t, http.MethodPost, "", spaBody), http.StatusCreated)

	// Shorten an active rule.
	rec := f.do(t, http.MethodPatch, "/1", `{"duration_ms":60000}`)
	wantStatus(t, rec, http.StatusOK)
	if got := decodeMuteRule(t, rec).ExpiresAt; !got.Equal(muteT0.Add(time.Minute)) {
		t.Fatalf("expires_at after PATCH = %v, want now+1m", got)
	}

	// End it, then revive it.
	wantStatus(t, f.do(t, http.MethodDelete, "/1", ""), http.StatusNoContent)
	if id := f.matchX(); id != nil {
		t.Fatalf("ended rule still matches: %d", *id)
	}
	f.now = muteT0.Add(time.Hour)
	rec = f.do(t, http.MethodPatch, "/1", `{"duration_ms":3600000}`)
	wantStatus(t, rec, http.StatusOK)

	got := decodeMuteRule(t, rec)
	if !got.ExpiresAt.Equal(f.now.Add(time.Hour)) {
		t.Errorf("revived expires_at = %v, want now+1h", got.ExpiresAt)
	}
	if got.ID != 1 || got.CreatedBy != "lasse" || !got.CreatedAt.Equal(muteT0.Add(-time.Minute)) {
		t.Errorf("revive changed id/created_by/created_at: %+v", got)
	}
	if id := f.matchX(); id == nil || *id != 1 {
		t.Fatalf("revived rule match = %v, want 1", id)
	}
}

func TestMuteRuleExpireIn_OmittedDurationIs24h(t *testing.T) {
	f := newMuteFixture()
	wantStatus(t, f.do(t, http.MethodPost, "", spaBody), http.StatusCreated)
	rec := f.do(t, http.MethodPatch, "/1", `{}`)
	wantStatus(t, rec, http.StatusOK)
	if got := decodeMuteRule(t, rec).ExpiresAt; !got.Equal(muteT0.Add(24 * time.Hour)) {
		t.Fatalf("expires_at = %v, want now+24h", got)
	}
}

func TestMuteRuleExpireIn_Errors(t *testing.T) {
	tests := []struct {
		name       string
		path, body string
		wantStatus int
		wantCode   string
	}{
		{name: "unknown id", path: "/999", body: `{"duration_ms":1000}`, wantStatus: http.StatusNotFound, wantCode: "not_found"},
		{name: "malformed id", path: "/abc", body: `{"duration_ms":1000}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_id"},
		{name: "duration over 7d", path: "/1", body: `{"duration_ms":604800001}`, wantStatus: http.StatusBadRequest, wantCode: "validation_failed"},
		{name: "malformed json", path: "/1", body: `{`, wantStatus: http.StatusBadRequest, wantCode: "invalid_json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newMuteFixture()
			wantStatus(t, f.do(t, http.MethodPost, "", spaBody), http.StatusCreated)
			rec := f.do(t, http.MethodPatch, tt.path, tt.body)
			wantStatus(t, rec, tt.wantStatus)
			if code := errorCode(t, rec); code != tt.wantCode {
				t.Errorf("error code = %q, want %q", code, tt.wantCode)
			}
			if got := f.store.rules[0].ExpiresAt; !got.Equal(muteT0.Add(time.Hour)) {
				t.Errorf("failed PATCH changed expires_at to %v", got)
			}
		})
	}
}

func TestMuteRuleUnmute(t *testing.T) {
	f := newMuteFixture()
	wantStatus(t, f.do(t, http.MethodPost, "", spaBody), http.StatusCreated)

	rec := f.do(t, http.MethodDelete, "/1", "")
	wantStatus(t, rec, http.StatusNoContent)
	if rec.Body.Len() != 0 {
		t.Errorf("204 body = %q, want empty", rec.Body.String())
	}
	if got := f.store.rules[0].ExpiresAt; !got.Equal(muteT0) {
		t.Fatalf("expires_at after unmute = %v, want now", got)
	}
	if id := f.matchX(); id != nil {
		t.Fatalf("unmuted rule still matches: %d", *id)
	}

	// A repeat unmute later is 204 and keeps the original end time.
	f.now = muteT0.Add(10 * time.Minute)
	wantStatus(t, f.do(t, http.MethodDelete, "/1", ""), http.StatusNoContent)
	if got := f.store.rules[0].ExpiresAt; !got.Equal(muteT0) {
		t.Fatalf("expires_at after repeat unmute = %v, want the first end %v", got, muteT0)
	}

	rec = f.do(t, http.MethodDelete, "/999", "")
	wantStatus(t, rec, http.StatusNotFound)
	if code := errorCode(t, rec); code != "not_found" {
		t.Errorf("unknown id code = %q, want not_found", code)
	}
	rec = f.do(t, http.MethodDelete, "/abc", "")
	wantStatus(t, rec, http.StatusBadRequest)
	if code := errorCode(t, rec); code != "invalid_id" {
		t.Errorf("malformed id code = %q, want invalid_id", code)
	}
}

func TestMuteRuleList(t *testing.T) {
	f := newMuteFixture()
	rec := f.do(t, http.MethodGet, "", "")
	wantStatus(t, rec, http.StatusOK)
	if body := strings.TrimSpace(rec.Body.String()); body != `{"data":[]}` {
		t.Errorf("empty list body = %s, want {\"data\":[]}", body)
	}
	if want := muteT0.Add(-7 * 24 * time.Hour); !f.store.listedAfter.Equal(want) {
		t.Errorf("listed rules expiring after %v, want now-7d (%v)", f.store.listedAfter, want)
	}

	// An ended rule is still listed.
	wantStatus(t, f.do(t, http.MethodPost, "", spaBody), http.StatusCreated)
	wantStatus(t, f.do(t, http.MethodDelete, "/1", ""), http.StatusNoContent)
	f.now = muteT0.Add(time.Hour)
	rec = f.do(t, http.MethodGet, "", "")
	wantStatus(t, rec, http.StatusOK)
	var resp struct {
		Data []mute.Rule `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].ID != 1 || !resp.Data[0].ExpiresAt.Equal(muteT0) {
		t.Fatalf("list = %+v, want the ended rule 1", resp.Data)
	}
}

func TestMuteRuleList_StoreError(t *testing.T) {
	f := newMuteFixture()
	f.store.listErr = errors.New("db down")
	rec := f.do(t, http.MethodGet, "", "")
	wantStatus(t, rec, http.StatusInternalServerError)
	if code := errorCode(t, rec); code != "internal_error" {
		t.Errorf("error code = %q, want internal_error", code)
	}
}
