package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/notification"
)

// TestIntegration_ListPreviewsCutRawAndStructuredData checks the list
// projection: raw_message and structured_data come back cut in SQL to a
// window just past the preview limit, message comes back whole, and the full
// list method still returns every byte.
func TestIntegration_ListPreviewsCutRawAndStructuredData(t *testing.T) {
	pool := testPool(t)
	store := NewStore(pool)
	ctx := context.Background()

	// Two-byte runes: left() counts characters, so the window is up to
	// twice the limit in bytes here.
	big := strings.Repeat("é", 4000)
	msg := strings.Repeat("m", 70000)

	for _, feed := range syslogFeeds {
		t.Run(feed, func(t *testing.T) {
			truncate(t, pool, feed+"_events")
			if _, err := pool.Exec(ctx,
				`INSERT INTO `+feed+`_events (received_at, reported_at, hostname, fromhost_ip, severity, facility, message, raw_message, structured_data)
				 VALUES (now(), now(), 'big', '10.0.0.1'::inet, 6, 1, $1, $2, $2)`, msg, big); err != nil {
				t.Fatalf("insert: %v", err)
			}

			var preview, full struct{ msg, raw, sd string }
			var cutTruncated bool
			var cutMsg, cutRaw, cutSD string
			switch feed {
			case "srvlog":
				p, _, err := store.ListSrvlogPreviews(ctx, model.SrvlogFilter{}, nil, 10)
				if err != nil || len(p) != 1 {
					t.Fatalf("ListSrvlogPreviews: %d rows, %v", len(p), err)
				}
				f, _, err := store.ListSrvlogs(ctx, model.SrvlogFilter{}, nil, 10)
				if err != nil || len(f) != 1 {
					t.Fatalf("ListSrvlogs: %d rows, %v", len(f), err)
				}
				preview.msg, preview.raw, preview.sd = p[0].Message, *p[0].RawMessage, *p[0].StructuredData
				full.msg, full.raw, full.sd = f[0].Message, *f[0].RawMessage, *f[0].StructuredData
				c := p[0].WithPreview()
				cutMsg, cutRaw, cutSD, cutTruncated = c.Message, *c.RawMessage, *c.StructuredData, c.Truncated
			case "netlog":
				p, _, err := store.ListNetlogPreviews(ctx, model.NetlogFilter{}, nil, 10)
				if err != nil || len(p) != 1 {
					t.Fatalf("ListNetlogPreviews: %d rows, %v", len(p), err)
				}
				f, _, err := store.ListNetlogs(ctx, model.NetlogFilter{}, nil, 10)
				if err != nil || len(f) != 1 {
					t.Fatalf("ListNetlogs: %d rows, %v", len(f), err)
				}
				preview.msg, preview.raw, preview.sd = p[0].Message, *p[0].RawMessage, *p[0].StructuredData
				full.msg, full.raw, full.sd = f[0].Message, *f[0].RawMessage, *f[0].StructuredData
				c := p[0].WithPreview()
				cutMsg, cutRaw, cutSD, cutTruncated = c.Message, *c.RawMessage, *c.StructuredData, c.Truncated
			}

			if preview.msg != msg {
				t.Errorf("preview message is %d bytes, want the whole %d for mute marking", len(preview.msg), len(msg))
			}
			window := (model.RawPreviewLimit + 1) * len("é")
			if len(preview.raw) != window || len(preview.sd) != window {
				t.Errorf("preview raw/sd = %d/%d bytes, want the %d-byte SQL window", len(preview.raw), len(preview.sd), window)
			}
			if full.msg != msg || full.raw != big || full.sd != big {
				t.Errorf("full list cut text: %d/%d/%d bytes", len(full.msg), len(full.raw), len(full.sd))
			}
			if len(cutMsg) != model.MessagePreviewLimit || len(cutRaw) != model.RawPreviewLimit ||
				len(cutSD) != model.RawPreviewLimit || !cutTruncated {
				t.Errorf("WithPreview: %d/%d/%d bytes, truncated %v", len(cutMsg), len(cutRaw), len(cutSD), cutTruncated)
			}
		})
	}
}

// TestIntegration_NotificationLogLeavesOutLargePayloads checks the log
// listing returns a small payload whole and leaves a large one out, flagged.
func TestIntegration_NotificationLogLeavesOutLargePayloads(t *testing.T) {
	pool := testPool(t)
	truncate(t, pool, "notification_log")
	store := NewStore(pool)
	ctx := context.Background()

	small := json.RawMessage(`{"kind":"srvlog","rule_name":"small"}`)
	large, err := json.Marshal(map[string]string{"kind": "srvlog", "blob": strings.Repeat("<", notification.LogPayloadPreviewLimit)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for i, payload := range []json.RawMessage{small, large} {
		if err := store.InsertNotificationLog(ctx, notification.LogEntry{
			RuleID: 1, ChannelID: 1, EventKind: "srvlog", EventID: int64(i + 1), Status: "sent", Payload: payload,
		}); err != nil {
			t.Fatalf("insert log %d: %v", i, err)
		}
	}

	entries, err := store.ListNotificationLog(ctx, notification.LogFilter{})
	if err != nil {
		t.Fatalf("ListNotificationLog: %v", err)
	}
	byEvent := map[int64]notification.LogEntry{}
	for _, e := range entries {
		byEvent[e.EventID] = e
	}
	if e := byEvent[1]; e.PayloadTruncated || !strings.Contains(string(e.Payload), `"rule_name"`) {
		t.Errorf("small payload: %s, truncated %v; want it whole", e.Payload, e.PayloadTruncated)
	}
	if e := byEvent[2]; !e.PayloadTruncated || e.Payload != nil {
		t.Errorf("large payload: %d bytes, truncated %v; want none, flagged", len(e.Payload), e.PayloadTruncated)
	}
}
