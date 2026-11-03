package analyzer

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

// hugeHostname is far longer than any real hostname. A syslog sender sets the
// header hostname, and nothing on the way to the prompt shortens it.
var hugeHostname = strings.Repeat("h", 60000)

// hugeMsgID pads signatures the same way. Its slots were already cut to 80
// or 60 characters; the padding keeps them in the worst case.
var hugeMsgID = strings.Repeat("M", 60000)

// syslogBudgetFixture fills every list the syslog gather step caps to its
// limit, puts hugeHostname in every hostname and program slot, and gives each
// of the store's 8 clusters 5000 distinct hosts and signatures. Message text
// sits at the store's 300-character sample cap.
func syslogBudgetFixture(feed string) analysisData {
	now := time.Date(2026, 9, 1, 6, 0, 0, 0, time.UTC)
	message := strings.Repeat("bgp peer 10.0.0.1 (External AS 65001) changed state from Established to Idle ", 4)[:300]
	sevs := map[int]int64{0: 1, 1: 2, 2: 3, 3: 400, 4: 500, 5: 600, 6: 7000, 7: 80000}
	sample := func(i int) model.SampleMessage {
		return model.SampleMessage{Hostname: hugeHostname, ReceivedAt: now.Add(-time.Duration(i) * time.Minute), Severity: 3, Message: message}
	}

	data := analysisData{
		Feed: feed, Period: 24 * time.Hour, PeriodLabel: "24 hours", PeriodStart: now.Add(-24 * time.Hour), PeriodEnd: now,
		NewMsgIDSamples: map[string]model.SampleMessage{}, JuniperRefs: map[string]model.JuniperNetlogRef{}, Unavailable: map[string]bool{},
		VolumeBucketLabel: "1 hour",
		VolumeSparkline:   strings.Repeat("▁▂▃▅▇█▆▄", 3),
		ErrorSparkline:    strings.Repeat("▁▁▁▂▄█▆▃", 3),
		VolumePeaks:       []string{"09-01 03:00 (4000 err / 600000 total)", "09-01 04:00 (900 err / 500000 total)", "09-01 05:00 (300 err / 400000 total)"},
	}
	for i := range topMsgIDLimit {
		mc := model.MsgIDCount{MsgID: fmt.Sprintf("RPD_SIGNATURE_%02d_", i) + hugeMsgID, Count: 90000, HostCount: 5000, SeverityCounts: sevs}
		for range 3 { // the store's top hosts per signature
			mc.TopHosts = append(mc.TopHosts, model.HostCount{Hostname: hugeHostname, Count: 30000})
		}
		for j := range topMsgIDSampleCount {
			mc.Samples = append(mc.Samples, sample(j))
		}
		data.TopMsgIDs = append(data.TopMsgIDs, mc)
	}
	for sev := range 8 {
		data.SeverityComparison.Levels = append(data.SeverityComparison.Levels, model.SeverityLevelComparison{
			Severity: sev, Label: model.SeverityLabel(sev), Current: 123456.7, BaselineAvg: 12345.6, ChangePct: 900.1,
		})
	}
	if feed == feedSrvlog {
		for range topProgramLimit {
			data.TopPrograms = append(data.TopPrograms, model.ProgramCount{Programname: hugeHostname, Count: 90000, ErrorCount: 400, SeverityCounts: sevs})
		}
		for f := range topFacilityLimit {
			data.TopFacilities = append(data.TopFacilities, model.FacilityCount{Facility: f, Label: model.FacilityLabel(f), Count: 90000, ErrorCount: 400})
		}
	}
	for i := range topHostLimit {
		data.TopErrorHosts = append(data.TopErrorHosts, model.HostErrorCount{Hostname: hugeHostname, Count: 9000, TopMsgID: fmt.Sprintf("ERR_%02d_", i) + hugeMsgID})
	}
	for i := range newMsgIDLimit {
		id := fmt.Sprintf("NEW_SIGNATURE_%02d_", i) + hugeMsgID
		data.NewMsgIDs = append(data.NewMsgIDs, id)
		data.NewMsgIDSamples[id] = sample(i)
	}
	data.NewMsgIDsOmitted = 4980

	// 5000 distinct names per list, the busiest (the padded one) first, as
	// the store ranks them. The slices are shared across clusters to keep the
	// fixture small.
	hosts, msgids := []string{hugeHostname}, []string{hugeMsgID}
	for i := 1; i < 5000; i++ {
		hosts = append(hosts, fmt.Sprintf("spoof-%057d", i))
		msgids = append(msgids, fmt.Sprintf("S%031d", i))
	}
	for i := range 8 { // the store's cluster limit
		data.EventClusters = append(data.EventClusters, model.EventCluster{
			Bucket: now.Add(-time.Duration(i) * time.Hour), Total: 90000,
			Hosts: hosts, HostCount: len(hosts), MsgIDs: msgids, MsgIDCount: len(msgids),
		})
	}
	return data
}

// syslogPromptBudgetBytes is the ceiling for the rendered syslog user prompt
// with every list at its cap. The worst case is about 25 signatures × 1.3 KB
// (three top hosts and two 300-character samples each), 20 new signatures ×
// 0.5 KB, 8 clusters × 1.5 KB and 15 error hosts × 0.2 KB, about 60 KB in
// all. 72 KB is about 18k tokens at 4 bytes per token, which with the
// largest system prompt (about 5k tokens) leaves a 32k window room for the
// reply. Sender-chosen hostnames must not move this number.
const syslogPromptBudgetBytes = 72 * 1024

// TestSyslogPromptBudget renders every syslog feed and mode at its caps with
// an oversized hostname in every slot, scoped and unscoped, and checks the
// prompt stays under the budget with every hostname cut to 64 characters.
func TestSyslogPromptBudget(t *testing.T) {
	for _, feed := range []string{feedNetlog, feedSrvlog} {
		spec, _ := model.AnalysisFeedSpecFor(feed)
		for _, mode := range spec.Modes {
			for _, scoped := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/scoped=%t", feed, mode, scoped), func(t *testing.T) {
					data := syslogBudgetFixture(feed)
					if scoped {
						data.Hosts = []string{hugeHostname}
					}
					sys, usr, err := buildPrompt(data, "", mode)
					if err != nil {
						t.Fatalf("buildPrompt: %v", err)
					}
					t.Logf("system %d bytes, user %d bytes (~%d tokens)", len(sys), len(usr), (len(sys)+len(usr))/4)
					if len(usr) > syslogPromptBudgetBytes {
						t.Errorf("user prompt is %d bytes, over the %d-byte budget", len(usr), syslogPromptBudgetBytes)
					}
					if strings.Contains(usr, strings.Repeat("h", 65)) {
						t.Errorf("a hostname or program slot renders more than 64 characters")
					}
					want := []string{"`" + strings.Repeat("h", 64) + "…`"}
					if scoped {
						want = append(want, "Scope: "+strings.Repeat("h", 64)+"… (1 host)")
					} else {
						// The cluster lines keep the true counts after the cut.
						want = append(want, "events across 5000 hosts [", fmt.Sprintf(", +%d more]", 5000-clusterNameLimit))
					}
					for _, w := range want {
						if !strings.Contains(usr, w) {
							t.Errorf("rendered prompt missing %q", w)
						}
					}
				})
			}
		}
	}
}
