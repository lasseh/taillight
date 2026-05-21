package analyzer

import (
	"strings"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

// fixtureData returns a representative analysisData suitable for rendering any
// of the prompt modes. The data deliberately mixes a routing event with a
// hardware fault and a cross-host cluster so each mode has something to talk
// about.
func fixtureData(t *testing.T) analysisData {
	t.Helper()
	now := time.Date(2025, 5, 21, 12, 0, 0, 0, time.UTC)
	return analysisData{
		Feed:        feedNetlog,
		Period:      24 * time.Hour,
		PeriodLabel: "24 hours",
		PeriodStart: now.Add(-24 * time.Hour),
		PeriodEnd:   now,
		TopMsgIDs: []model.MsgIDCount{
			{
				MsgID:     "RPD_BGP_NEIGHBOR_STATE_CHANGED",
				Count:     42,
				HostCount: 2,
				TopHosts: []model.HostCount{
					{Hostname: "edge1-syd", Count: 25},
					{Hostname: "edge2-syd", Count: 17},
				},
				SeverityCounts: map[int]int64{3: 30, 4: 12},
				Samples: []model.SampleMessage{
					{Hostname: "edge1-syd", ReceivedAt: now.Add(-30 * time.Minute), Severity: 3, Message: "bgp peer 10.0.0.1 (External AS 65001) changed state from Established to Idle"},
				},
			},
			{
				MsgID:          "CHASSISD_PSU_FAILURE",
				Count:          3,
				SeverityCounts: map[int]int64{1: 3},
				Samples: []model.SampleMessage{
					{Hostname: "core2-osl", ReceivedAt: now.Add(-90 * time.Minute), Severity: 1, Message: "PSU 1 input feed lost; chassis on redundant feed"},
				},
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
		NewMsgIDSamples: map[string]model.SampleMessage{
			"KERN_ARP_ADDR_CHANGE": {Hostname: "edge3-osl", ReceivedAt: now.Add(-15 * time.Minute), Severity: 4, Message: "arp address change for 10.1.2.3 from aa:bb:cc:dd:ee:ff to 11:22:33:44:55:66"},
		},
		VolumeBucketLabel: "1 hour",
		VolumeSparkline:   "▁▂▃▅▇█▆▄▂▁",
		ErrorSparkline:    "▁▁▁▂▄█▆▃▁▁",
		VolumePeaks:       []string{"05-21 11:00 (240 err / 1200 total)"},
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
}

// TestBuildPromptAllModes verifies every embedded mode parses and renders
// without error, and that each one carries the persona / verdict cue
// appropriate to its framing. Without this, a template typo or a renamed
// mode would only surface when the worker runs an actual report.
func TestBuildPromptAllModes(t *testing.T) {
	data := fixtureData(t)

	cases := []struct {
		mode           string
		systemContains string // a phrase distinctive to this mode's system prompt
	}{
		{mode: modeDaily, systemContains: "on-call team"},
		{mode: modeWeekly, systemContains: "trend review"},
		{mode: modeIncident, systemContains: "live triage"},
	}

	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			sys, usr, err := buildPrompt(data, "", tc.mode)
			if err != nil {
				t.Fatalf("buildPrompt(%s): %v", tc.mode, err)
			}
			if sys == "" || usr == "" {
				t.Fatalf("buildPrompt(%s) returned empty prompt(s)", tc.mode)
			}
			if !strings.Contains(sys, tc.systemContains) {
				t.Errorf("system prompt for %s missing %q distinctive phrase; got:\n%s", tc.mode, tc.systemContains, sys)
			}
			// All modes must inject the fixture data into the user prompt.
			if !strings.Contains(usr, "RPD_BGP_NEIGHBOR_STATE_CHANGED") {
				t.Errorf("user prompt for %s missing injected msgid; got:\n%s", tc.mode, usr)
			}
			if !strings.Contains(usr, "edge1-syd") {
				t.Errorf("user prompt for %s missing injected hostname; got:\n%s", tc.mode, usr)
			}
			// Sample message text must reach the prompt — this is the
			// whole point of attaching samples in gather.
			if !strings.Contains(usr, "bgp peer 10.0.0.1") {
				t.Errorf("user prompt for %s missing top-msgid sample text; got:\n%s", tc.mode, usr)
			}
			if !strings.Contains(usr, "arp address change") {
				t.Errorf("user prompt for %s missing new-msgid sample text; got:\n%s", tc.mode, usr)
			}
			// Volume sparkline branch must render — exercising the
			// {{ if .VolumeSparkline }} guard at least once across modes.
			if !strings.Contains(usr, "▁▂▃▅▇█▆▄▂▁") {
				t.Errorf("user prompt for %s missing volume sparkline; got:\n%s", tc.mode, usr)
			}
			if !strings.Contains(usr, "Peaks:") {
				t.Errorf("user prompt for %s missing peaks line; got:\n%s", tc.mode, usr)
			}
			// Host distribution: hostcount + top contributors must render.
			if !strings.Contains(usr, "2 hosts") || !strings.Contains(usr, "edge2-syd") {
				t.Errorf("user prompt for %s missing msgid host distribution; got:\n%s", tc.mode, usr)
			}
		})
	}
}

// TestBuildPromptDefaultsToDaily covers the back-compat path: an empty mode
// string should be treated as "daily" so old callers keep working until the
// rest of the plumbing lands.
func TestBuildPromptDefaultsToDaily(t *testing.T) {
	data := fixtureData(t)
	sys, _, err := buildPrompt(data, "", "")
	if err != nil {
		t.Fatalf("buildPrompt with empty mode: %v", err)
	}
	if !strings.Contains(sys, "on-call team") {
		t.Errorf("empty mode did not resolve to daily; system prompt:\n%s", sys)
	}
}

// TestBuildPromptUnknownMode verifies that an unknown mode errors out
// instead of silently using a default — the whole point of the contract
// is "no silent fallback".
func TestBuildPromptUnknownMode(t *testing.T) {
	data := fixtureData(t)
	_, _, err := buildPrompt(data, "", "monthly-snapshot")
	if err == nil {
		t.Fatal("buildPrompt with unknown mode returned nil error")
	}
	if !strings.Contains(err.Error(), "unknown prompt mode") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestTruncatePromptString covers the template-func used to clip long
// msg_pattern signatures so they don't dominate the data block.
func TestTruncatePromptString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"short stays intact", "RPD_BGP_NEIGHBOR_STATE_CHANGED", 80, "RPD_BGP_NEIGHBOR_STATE_CHANGED"},
		{"exact length stays intact", "abcdef", 6, "abcdef"},
		{"long is truncated with ellipsis", "abcdefghij", 5, "abcde…"},
		{"n zero is a no-op", "anything", 0, "anything"},
		{"n negative is a no-op", "anything", -3, "anything"},
		{"empty stays empty", "", 10, ""},
		{"counts runes not bytes", "ééééééé", 3, "ééé…"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := truncatePromptString(tc.in, tc.n)
			if got != tc.want {
				t.Errorf("truncatePromptString(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
			}
		})
	}
}

// TestTruncatePromptStrings exercises the slice variant used inside cluster
// msgid lists, where one long element would otherwise dominate the rendered
// line.
func TestTruncatePromptStrings(t *testing.T) {
	t.Parallel()
	got := truncatePromptStrings([]string{"short", "thisIsAVeryLongMsgPatternWithAFunctionSignature", ""}, 10)
	want := []string{"short", "thisIsAVer…", ""}
	if len(got) != len(want) {
		t.Fatalf("len(got)=%d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("element %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestBuildPromptTruncatesLongMsgID verifies the truncate template func is
// actually wired into the user templates — a long msg_pattern in TopMsgIDs
// renders with an ellipsis in the rendered prompt, not as a 200-char wall.
func TestBuildPromptTruncatesLongMsgID(t *testing.T) {
	t.Parallel()
	longLabel := strings.Repeat("X", 200)
	data := analysisData{
		Feed:        feedNetlog,
		Period:      24 * time.Hour,
		PeriodLabel: "24 hours",
		PeriodStart: time.Now().Add(-24 * time.Hour),
		PeriodEnd:   time.Now(),
		TopMsgIDs: []model.MsgIDCount{
			{MsgID: longLabel, Count: 1, SeverityCounts: map[int]int64{3: 1}},
		},
		JuniperRefs: map[string]model.JuniperNetlogRef{},
	}
	_, user, err := buildPrompt(data, "", modeDaily)
	if err != nil {
		t.Fatalf("buildPrompt: %v", err)
	}
	if strings.Contains(user, longLabel) {
		t.Errorf("user prompt contains the full 200-char label; truncate did not run")
	}
	if !strings.Contains(user, "…") {
		t.Errorf("user prompt missing ellipsis after truncation")
	}
}

// TestBuildPromptEmptyData ensures the templates render cleanly when the
// period is genuinely quiet (no msgids, no hosts, no clusters). The system
// prompts instruct the model to handle this gracefully — make sure the
// templates themselves don't blow up first, across every mode.
func TestBuildPromptEmptyData(t *testing.T) {
	for _, mode := range []string{modeDaily, modeWeekly, modeIncident} {
		t.Run(mode, func(t *testing.T) {
			data := analysisData{
				Feed:        feedSrvlog,
				Period:      24 * time.Hour,
				PeriodLabel: "24 hours",
				PeriodStart: time.Now().Add(-24 * time.Hour),
				PeriodEnd:   time.Now(),
				JuniperRefs: map[string]model.JuniperNetlogRef{},
			}
			if _, _, err := buildPrompt(data, "", mode); err != nil {
				t.Fatalf("buildPrompt(%s) on empty data: %v", mode, err)
			}
		})
	}
}
