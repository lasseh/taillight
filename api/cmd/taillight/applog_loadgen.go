package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/spf13/cobra"
)

var (
	applogLoadgenN        int
	applogLoadgenDelay    time.Duration
	applogLoadgenJitter   time.Duration
	applogLoadgenEndpoint string
	applogLoadgenAPIKey   string
	applogLoadgenBatch    int
	applogLoadgenInsecure bool
)

var applogLoadgenCmd = &cobra.Command{
	Use:   "loadgen-applog",
	Short: "Generate random application log events via the ingest API",
	RunE:  runApplogLoadgen,
}

func init() {
	applogLoadgenCmd.Flags().IntVarP(&applogLoadgenN, "n", "n", 100, "number of events to send")
	applogLoadgenCmd.Flags().DurationVar(&applogLoadgenDelay, "delay", 0, "fixed delay between batches (e.g. 100ms)")
	applogLoadgenCmd.Flags().DurationVar(&applogLoadgenJitter, "jitter", 0, "random jitter added to delay (e.g. 200ms)")
	applogLoadgenCmd.Flags().StringVar(&applogLoadgenEndpoint, "endpoint", "http://localhost:8080/api/v1/applog/ingest", "ingest API endpoint URL")
	applogLoadgenCmd.Flags().StringVar(&applogLoadgenAPIKey, "api-key", "", "bearer token for API authentication")
	applogLoadgenCmd.Flags().IntVar(&applogLoadgenBatch, "batch", 50, "events per API request (max 1000)")
	applogLoadgenCmd.Flags().BoolVarP(&applogLoadgenInsecure, "insecure", "k", false, "skip TLS certificate verification")
}

// serviceWeightTotal is the sum of all service weights, computed at init.
var serviceWeightTotal int

func init() {
	for _, s := range services {
		serviceWeightTotal += s.weight
	}
}

// pickService selects a service profile using weighted random selection.
func pickService() *serviceProfile {
	n := rand.IntN(serviceWeightTotal)
	for i := range services {
		n -= services[i].weight
		if n < 0 {
			return &services[i]
		}
	}
	return &services[len(services)-1]
}

// applogLevelWeightTotal is the sum of all level weights, computed at init.
var applogLevelWeightTotal int

func init() {
	for _, lw := range applogLevelWeights {
		applogLevelWeightTotal += lw.weight
	}
}

// pickApplogLevel selects a log level using weighted random selection.
func pickApplogLevel() string {
	n := rand.IntN(applogLevelWeightTotal)
	for _, lw := range applogLevelWeights {
		n -= lw.weight
		if n < 0 {
			return lw.level
		}
	}
	return levelInfo
}

// pickMsg selects a message from the service profile, optionally filtering by level.
// Falls back to any message if no match is found for the given level.
func pickMsg(svc *serviceProfile, level string) applogMsg {
	// Collect messages matching the desired level.
	var matching []applogMsg
	for _, m := range svc.msgs {
		if m.level == level {
			matching = append(matching, m)
		}
	}
	if len(matching) > 0 {
		return matching[rand.IntN(len(matching))]
	}
	// Fallback: pick any message from this service.
	return svc.msgs[rand.IntN(len(svc.msgs))]
}

// ingestEntry matches the API's AppLogIngestEntry JSON shape.
type ingestEntry struct {
	Timestamp time.Time       `json:"timestamp"`
	Level     string          `json:"level"`
	Msg       string          `json:"msg"`
	Host      string          `json:"host"`
	Service   string          `json:"service"`
	Component string          `json:"component,omitempty"`
	Source    string          `json:"source,omitempty"`
	Attrs     json.RawMessage `json:"attrs,omitempty"`
}

type ingestRequest struct {
	Logs []ingestEntry `json:"logs"`
}

func sendBatch(ctx context.Context, client *http.Client, entries []ingestEntry) error {
	body, err := json.Marshal(ingestRequest{Logs: entries})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, applogLoadgenEndpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if applogLoadgenAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+applogLoadgenAPIKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func runApplogLoadgen(cmd *cobra.Command, _ []string) error {
	if applogLoadgenBatch < 1 || applogLoadgenBatch > 1000 {
		return fmt.Errorf("batch size must be between 1 and 1000")
	}

	ctx := cmd.Context()
	client := &http.Client{Timeout: 30 * time.Second}
	if applogLoadgenInsecure {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // user-requested flag
		}
	}

	fmt.Printf("target: %s\n", applogLoadgenEndpoint)
	if applogLoadgenDelay > 0 || applogLoadgenJitter > 0 {
		fmt.Printf("sending %d applog events (batch=%d delay=%s jitter=%s)...\n", applogLoadgenN, applogLoadgenBatch, applogLoadgenDelay, applogLoadgenJitter)
	} else {
		fmt.Printf("sending %d applog events (batch=%d) as fast as possible...\n", applogLoadgenN, applogLoadgenBatch)
	}

	start := time.Now()
	sent := 0
	batch := make([]ingestEntry, 0, applogLoadgenBatch)

	for i := range applogLoadgenN {
		svc := pickService()
		level := pickApplogLevel()
		msg := pickMsg(svc, level)

		batch = append(batch, ingestEntry{
			Timestamp: time.Now(),
			Level:     level,
			Msg:       msg.msg,
			Host:      svc.hosts[rand.IntN(len(svc.hosts))],
			Service:   svc.service,
			Component: msg.component,
			Source:    msg.source,
			Attrs:     msg.attrs,
		})

		if len(batch) >= applogLoadgenBatch || i == applogLoadgenN-1 {
			if err := sendBatch(ctx, client, batch); err != nil {
				return fmt.Errorf("batch at event %d: %w", i, err)
			}
			sent += len(batch)
			batch = batch[:0]

			fmt.Printf("  %d/%d (%.0f events/sec)\n", sent, applogLoadgenN, float64(sent)/time.Since(start).Seconds())

			if wait := applogLoadgenDelay + time.Duration(rand.Int64N(int64(applogLoadgenJitter+1))); wait > 0 {
				time.Sleep(wait)
			}
		}
	}

	elapsed := time.Since(start)
	fmt.Printf("done: %d applog events in %s (%.0f events/sec)\n", sent, elapsed, float64(sent)/elapsed.Seconds())
	return nil
}
