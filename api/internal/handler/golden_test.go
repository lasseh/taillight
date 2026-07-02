package handler

// Golden-fixture contract tests (architecture review X7). The JSON files
// under testdata/golden/ are the shared wire-contract fixtures for the
// highest-churn API surfaces: the three event shapes, the list/detail
// envelopes, and the applog ingest request. This test generates and verifies
// them from the real Go types; the frontend asserts the exact same files
// against its TypeScript types in
// frontend/src/types/__tests__/contract-goldens.test.ts, so a contract
// change breaks whichever side wasn't updated.
//
// Regenerate after an intentional contract change (then re-run the frontend
// suite):
//
//	go test ./internal/handler -run TestGolden -update

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/lasseh/taillight/internal/broker"
	"github.com/lasseh/taillight/internal/model"
)

var updateGoldens = flag.Bool("update", false, "rewrite golden files under testdata/golden")

// checkGolden marshals v and compares it byte-for-byte against the named
// golden file, rewriting the file instead when -update is set.
func checkGolden(t *testing.T, name string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal fixture for %s: %v", name, err)
	}
	got = append(got, '\n')

	path := filepath.Join("testdata", "golden", name)
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to generate)", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("golden mismatch for %s\n--- got ---\n%s--- want ---\n%s", name, got, want)
	}
}

func goldenPtr[T any](v T) *T { return &v }

func goldenSrvlogEvent() model.SrvlogEvent {
	return model.SrvlogEvent{
		ID:             1001,
		ReceivedAt:     time.Date(2026, 6, 15, 12, 0, 0, 500000000, time.UTC),
		ReportedAt:     time.Date(2026, 6, 15, 11, 59, 59, 0, time.UTC),
		Hostname:       "web01.example.net",
		FromhostIP:     "192.0.2.10",
		Programname:    "sshd",
		MsgID:          "SSHD_LOGIN_FAILED",
		Severity:       model.SeverityWarning,
		SeverityLabel:  model.SeverityLabel(model.SeverityWarning),
		Facility:       4,
		FacilityLabel:  model.FacilityLabel(4),
		SyslogTag:      "sshd[4242]:",
		StructuredData: goldenPtr(`[timeQuality tzKnown="1" isSynced="1"]`),
		Message:        "Failed password for invalid user admin from 198.51.100.7 port 52344 ssh2",
		RawMessage:     goldenPtr("<38>Jun 15 12:00:00 web01 sshd[4242]: Failed password"),
	}
}

func goldenNetlogEvent() model.NetlogEvent {
	return model.NetlogEvent{
		ID:             2001,
		ReceivedAt:     time.Date(2026, 6, 15, 12, 0, 1, 250000000, time.UTC),
		ReportedAt:     time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC),
		Hostname:       "edge-rtr01.example.net",
		FromhostIP:     "192.0.2.1",
		Programname:    "rpd",
		MsgID:          "RPD_BGP_NEIGHBOR_STATE_CHANGED",
		Severity:       model.SeverityErr,
		SeverityLabel:  model.SeverityLabel(model.SeverityErr),
		Facility:       23,
		FacilityLabel:  model.FacilityLabel(23),
		SyslogTag:      "rpd[1523]:",
		StructuredData: goldenPtr(`[junos@2636.1.1.1.2.29 peer-name="203.0.113.9"]`),
		Message:        "BGP peer 203.0.113.9 (External AS 64511) changed state from Established to Idle",
		RawMessage:     goldenPtr("<187>Jun 15 12:00:00 edge-rtr01 rpd[1523]: RPD_BGP_NEIGHBOR_STATE_CHANGED"),
	}
}

func goldenAppLogEvent() model.AppLogEvent {
	return model.AppLogEvent{
		ID:         3001,
		ReceivedAt: time.Date(2026, 6, 15, 12, 0, 2, 0, time.UTC),
		Timestamp:  time.Date(2026, 6, 15, 12, 0, 1, 750000000, time.UTC),
		Level:      "ERROR",
		Service:    "billing-api",
		Component:  "worker",
		Host:       "app01",
		Msg:        "payment reconciliation failed",
		Source:     "reconcile.go:87",
		Attrs:      json.RawMessage(`{"attempt":3,"invoice_id":"inv_123"}`),
		SourceIP:   goldenPtr("203.0.113.40"),
		APIKeyID: pgtype.UUID{
			Bytes: [16]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
			Valid: true,
		},
	}
}

func TestGoldenEventShapes(t *testing.T) {
	checkGolden(t, "srvlog_event.json", goldenSrvlogEvent())

	srvlogNil := goldenSrvlogEvent()
	srvlogNil.StructuredData = nil
	srvlogNil.RawMessage = nil
	checkGolden(t, "srvlog_event_nil_fields.json", srvlogNil)

	checkGolden(t, "netlog_event.json", goldenNetlogEvent())

	netlogNil := goldenNetlogEvent()
	netlogNil.StructuredData = nil
	netlogNil.RawMessage = nil
	checkGolden(t, "netlog_event_nil_fields.json", netlogNil)

	checkGolden(t, "applog_event.json", goldenAppLogEvent())

	// List/SSE preview: oversized attrs are stripped and flagged.
	truncated := goldenAppLogEvent()
	truncated.Attrs = json.RawMessage(fmt.Sprintf(`{"blob":%q}`, strings.Repeat("x", model.AttrsPreviewLimit+1)))
	truncated = truncated.WithAttrsPreview(model.AttrsPreviewLimit)
	checkGolden(t, "applog_event_attrs_truncated.json", truncated)

	// Rows ingested via session auth or before source_ip/api_key_id existed.
	applogNil := goldenAppLogEvent()
	applogNil.Component = ""
	applogNil.Source = ""
	applogNil.Attrs = nil
	applogNil.SourceIP = nil
	applogNil.APIKeyID = pgtype.UUID{}
	checkGolden(t, "applog_event_nil_fields.json", applogNil)
}

func TestGoldenEnvelopes(t *testing.T) {
	first := goldenSrvlogEvent()
	second := goldenSrvlogEvent()
	second.ID = 1002

	cursor := model.Cursor{ReceivedAt: second.ReceivedAt, ID: second.ID}.Encode()
	checkGolden(t, "list_envelope.json", listResponse{
		Data:    []model.SrvlogEvent{first, second},
		Cursor:  &cursor,
		HasMore: true,
	})

	// Last page: cursor omitted, data serializes as [] (never null).
	checkGolden(t, "list_envelope_last_page.json", listResponse{
		Data:    emptySlice[model.SrvlogEvent](nil),
		HasMore: false,
	})

	checkGolden(t, "detail_envelope.json", itemResponse{Data: goldenSrvlogEvent()})
}

func goldenIngestRequest() AppLogIngestRequest {
	return AppLogIngestRequest{Logs: []AppLogIngestEntry{
		{
			Timestamp: time.Date(2026, 6, 15, 12, 0, 3, 0, time.UTC),
			Level:     "WARN",
			Msg:       "cache miss rate above threshold",
			Service:   "billing-api",
			Component: "cache",
			Host:      "app01",
			Source:    "cache.go:42",
			Attrs:     json.RawMessage(`{"rate":0.42}`),
		},
		{
			// Required fields only.
			Timestamp: time.Date(2026, 6, 15, 12, 0, 4, 0, time.UTC),
			Level:     "INFO",
			Msg:       "startup complete",
			Service:   "billing-api",
			Host:      "app02",
		},
	}}
}

// goldenIngestLimits mirrors the ingest validation rules enforced by
// AppLogIngestHandler.Ingest. The caps come from the same constants the
// handler uses; RequiredFields is proven against the handler below.
type goldenIngestLimitsDoc struct {
	MaxBatchSize   int      `json:"max_batch_size"`
	MaxMsgBytes    int      `json:"max_msg_bytes"`
	RequiredFields []string `json:"required_fields"`
}

func goldenIngestLimits() goldenIngestLimitsDoc {
	return goldenIngestLimitsDoc{
		MaxBatchSize:   applogMaxBatchSize,
		MaxMsgBytes:    applogMaxMsgLen,
		RequiredFields: []string{"timestamp", "level", "msg", "service", "host"},
	}
}

func TestGoldenIngestRequest(t *testing.T) {
	checkGolden(t, "applog_ingest_request.json", goldenIngestRequest())
	checkGolden(t, "applog_ingest_limits.json", goldenIngestLimits())
}

func postGoldenIngest(t *testing.T, body []byte) int {
	t.Helper()
	h := NewAppLogIngestHandler(&mockAppLogStore{}, broker.NewAppLogBroker(slog.Default()), slog.Default(), nil)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/applog/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Ingest(rec, req)
	return rec.Code
}

// TestGoldenIngestRequestSatisfiesServerRules proves the golden request and
// the advertised required-field list against the real handler, so the golden
// cannot drift from the validation code it documents.
func TestGoldenIngestRequestSatisfiesServerRules(t *testing.T) {
	body, err := json.Marshal(goldenIngestRequest())
	if err != nil {
		t.Fatalf("marshal golden request: %v", err)
	}
	if code := postGoldenIngest(t, body); code != http.StatusAccepted {
		t.Fatalf("golden ingest request rejected by the real handler: status %d", code)
	}

	// Each field the limits golden declares required must actually be
	// rejected by the handler when absent.
	for _, field := range goldenIngestLimits().RequiredFields {
		entry := map[string]any{
			"timestamp": "2026-06-15T12:00:04Z",
			"level":     "INFO",
			"msg":       "probe",
			"service":   "billing-api",
			"host":      "app02",
		}
		delete(entry, field)
		body, err := json.Marshal(map[string]any{"logs": []any{entry}})
		if err != nil {
			t.Fatalf("marshal probe request: %v", err)
		}
		if code := postGoldenIngest(t, body); code != http.StatusBadRequest {
			t.Errorf("entry missing %q: got status %d, want %d", field, code, http.StatusBadRequest)
		}
	}
}
