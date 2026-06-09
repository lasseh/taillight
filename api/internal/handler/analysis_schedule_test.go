package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/notification"
)

// stubScheduleStore implements AnalysisScheduleStore. Only the methods exercised
// by Create + channel validation do anything; the rest are inert.
type stubScheduleStore struct {
	channels []notification.Channel
	created  model.AnalysisSchedule
}

func (s *stubScheduleStore) ListAnalysisSchedules(context.Context) ([]model.AnalysisSchedule, error) {
	return nil, nil
}

func (s *stubScheduleStore) GetAnalysisSchedule(context.Context, int64) (model.AnalysisSchedule, error) {
	return model.AnalysisSchedule{}, nil
}

func (s *stubScheduleStore) CreateAnalysisSchedule(_ context.Context, sched model.AnalysisSchedule) (model.AnalysisSchedule, error) {
	s.created = sched
	sched.ID = 1
	return sched, nil
}

func (s *stubScheduleStore) UpdateAnalysisSchedule(_ context.Context, _ int64, sched model.AnalysisSchedule) (model.AnalysisSchedule, error) {
	return sched, nil
}

func (s *stubScheduleStore) DeleteAnalysisSchedule(context.Context, int64) error { return nil }

func (s *stubScheduleStore) ListNotificationChannels(context.Context) ([]notification.Channel, error) {
	return s.channels, nil
}

func postSchedule(t *testing.T, h *AnalysisScheduleHandler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/analysis/schedules", bytes.NewReader(buf))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	return rec
}

func TestCreateScheduleNotifyChannelValidation(t *testing.T) {
	channels := []notification.Channel{
		{ID: 1, Name: "ops-email", Type: notification.ChannelTypeEmail},
		{ID: 2, Name: "ops-slack", Type: notification.ChannelTypeSlack},
	}
	base := map[string]any{
		"name":        "nightly",
		"enabled":     true,
		"feed":        "srvlog",
		"frequency":   "daily",
		"time_of_day": "03:00",
		"timezone":    "UTC",
	}

	tests := []struct {
		name       string
		channelIDs []int64
		wantStatus int
	}{
		{"no channels", nil, http.StatusCreated},
		{"valid email channel", []int64{1}, http.StatusCreated},
		{"non-email channel rejected", []int64{2}, http.StatusBadRequest},
		{"unknown channel rejected", []int64{99}, http.StatusBadRequest},
		{"mix of valid and unknown rejected", []int64{1, 99}, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &stubScheduleStore{channels: channels}
			h := NewAnalysisScheduleHandler(store, nil, true)

			body := make(map[string]any, len(base)+1)
			for k, v := range base {
				body[k] = v
			}
			if tt.channelIDs != nil {
				body["notify_channel_ids"] = tt.channelIDs
			}

			rec := postSchedule(t, h, body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus == http.StatusCreated {
				if got := len(store.created.NotifyChannelIDs); got != len(tt.channelIDs) {
					t.Errorf("persisted %d channel ids, want %d", got, len(tt.channelIDs))
				}
			}
		})
	}
}
