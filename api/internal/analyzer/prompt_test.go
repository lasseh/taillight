package analyzer

import (
	"strings"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

// TestBuildPromptEmbeddedDefaults verifies the embedded default prompts parse
// and render without error against representative data. Without this, a
// template typo would only surface when the worker runs an actual report.
func TestBuildPromptEmbeddedDefaults(t *testing.T) {
	now := time.Date(2025, 5, 21, 12, 0, 0, 0, time.UTC)
	data := analysisData{
		Feed:        feedNetlog,
		Period:      24 * time.Hour,
		PeriodLabel: "24 hours",
		PeriodStart: now.Add(-24 * time.Hour),
		PeriodEnd:   now,
		TopMsgIDs: []model.MsgIDCount{
			{
				MsgID:          "RPD_BGP_NEIGHBOR_STATE_CHANGED",
				Count:          42,
				SeverityCounts: map[int]int64{3: 30, 4: 12},
			},
			{
				MsgID:          "CHASSISD_PSU_FAILURE",
				Count:          3,
				SeverityCounts: map[int]int64{1: 3},
			},
		},
		SeverityComparison: model.SeverityComparison{
			Levels: []model.SeverityLevelComparison{
				{Severity: 3, Label: "err", Current: 60, BaselineAvg: 20, ChangePct: 200},
				{Severity: 4, Label: "warn", Current: 12, BaselineAvg: 15, ChangePct: -20},
			},
		},
		TopErrorHosts: []model.HostErrorCount{
			{Hostname: "edge1-syd", Count: 25, TopMsgID: "RPD_BGP_NEIGHBOR_STATE_CHANGED"},
			{Hostname: "core2-osl", Count: 3, TopMsgID: "CHASSISD_PSU_FAILURE"},
		},
		NewMsgIDs: []string{"KERN_ARP_ADDR_CHANGE"},
		EventClusters: []model.EventCluster{
			{
				Bucket: now.Add(-2 * time.Hour),
				Total:  18,
				Hosts:  []string{"edge1-syd", "edge2-syd"},
				MsgIDs: []string{"RPD_BGP_NEIGHBOR_STATE_CHANGED"},
			},
		},
		JuniperRefs: map[string]model.JuniperNetlogRef{
			"RPD_BGP_NEIGHBOR_STATE_CHANGED": {
				Description: "BGP neighbor state changed",
				Cause:       "Peer reset or link flap",
				Action:      "Inspect peer logs and link state",
			},
		},
	}

	sys, usr, err := buildPrompt(data, "")
	if err != nil {
		t.Fatalf("buildPrompt with embedded defaults: %v", err)
	}

	// Sanity: both prompts non-empty and reference the injected data.
	if sys == "" {
		t.Fatal("system prompt is empty")
	}
	if usr == "" {
		t.Fatal("user prompt is empty")
	}
	if !strings.Contains(usr, "RPD_BGP_NEIGHBOR_STATE_CHANGED") {
		t.Errorf("user prompt missing injected msgid; got:\n%s", usr)
	}
	if !strings.Contains(usr, "edge1-syd") {
		t.Errorf("user prompt missing injected hostname; got:\n%s", usr)
	}
	if !strings.Contains(usr, "BGP neighbor state changed") {
		t.Errorf("user prompt missing Juniper ref description; got:\n%s", usr)
	}
	if !strings.Contains(sys, "network operations engineer") {
		t.Errorf("system prompt missing expected persona text; got:\n%s", sys)
	}
}

// TestBuildPromptEmptyData ensures the templates render cleanly when the
// period is genuinely quiet (no msgids, no hosts, no clusters). The system
// prompt instructs the model to handle this gracefully — make sure the
// templates themselves don't blow up first.
func TestBuildPromptEmptyData(t *testing.T) {
	data := analysisData{
		Feed:        feedSrvlog,
		Period:      24 * time.Hour,
		PeriodLabel: "24 hours",
		PeriodStart: time.Now().Add(-24 * time.Hour),
		PeriodEnd:   time.Now(),
		JuniperRefs: map[string]model.JuniperNetlogRef{},
	}

	if _, _, err := buildPrompt(data, ""); err != nil {
		t.Fatalf("buildPrompt on empty data: %v", err)
	}
}
