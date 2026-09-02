package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/taillight/internal/analyzer"
	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/ollama"
	"github.com/lasseh/taillight/internal/postgres"
)

// scenarioReply is a minimal applog daily reply that passes the validator,
// so the fake model never triggers the corrective retry.
const scenarioReply = "## New errors and warnings\n**Status: WATCH** — fake model reply.\n\n" +
	"## Top recurring errors and warnings\n_fake_\n\n## Volume vs last week\n_fake_\n\n" +
	"## Silent and new services\n_fake_\n\n## Log hygiene\n_fake_\n"

// scenarioRun is what one analyzer run over the seeded scenario produced.
type scenarioRun struct {
	byRole map[string][]string
	system string
	user   string
	report string
}

// TestIntegration_ApplogScenarioAnalysis seeds the analysis scenario into a
// test database and runs the applog analyzer over it against a fake Ollama
// that captures the prompt. Every planted signal must reach the data block
// in the section the report expects it in. Set APPLOG_PROMPT_OUT to a
// directory to keep the captured prompts and report for prompt tuning.
func TestIntegration_ApplogScenarioAnalysis(t *testing.T) {
	run := runScenarioAnalysis(t)
	usr := run.user

	t.Run("ranking", func(t *testing.T) { checkScenarioRanking(t, run) })
	t.Run("silent and new services", func(t *testing.T) {
		for _, svc := range run.byRole[roleSilent] {
			if !strings.Contains(promptSection(t, usr, "Silent services"), "`"+svc+"`") {
				t.Errorf("silent service %q not listed:\n%s", svc, promptSection(t, usr, "Silent services"))
			}
		}
		for _, svc := range run.byRole[roleFresh] {
			if !strings.Contains(promptSection(t, usr, "New services"), "`"+svc+"`") {
				t.Errorf("new service %q not listed:\n%s", svc, promptSection(t, usr, "New services"))
			}
		}
	})
	t.Run("new signatures", func(t *testing.T) {
		newSigs := promptSection(t, usr, "New signatures")
		for _, want := range []string{"panic: runtime error: invalid memory address", "circuit breaker open for redis-sessions (<n> failures)"} {
			if !strings.Contains(newSigs, want) {
				t.Errorf("new signature %q missing:\n%s", want, newSigs)
			}
		}
		if strings.Contains(newSigs, "upstream payments-gateway returned <n>") {
			t.Errorf("the burst template has a baseline trickle and must not read as new:\n%s", newSigs)
		}
	})
	t.Run("hygiene", func(t *testing.T) {
		hyg := promptSection(t, usr, "Hygiene")
		if !strings.Contains(hyg, "attrs over 1024 bytes: 5") {
			t.Errorf("oversize attrs count missing:\n%s", hyg)
		}
		if strings.Contains(hyg, "empty component: 0 ") {
			t.Errorf("empty component rows not counted:\n%s", hyg)
		}
		for _, svc := range run.byRole[roleDominant] {
			if !strings.Contains(hyg, "Dominant: `"+svc+"`") {
				t.Errorf("dominant template for %q missing:\n%s", svc, hyg)
			}
		}
		for _, svc := range run.byRole[roleNoisy] {
			if strings.Contains(hyg, "Dominant: `"+svc+"`") {
				t.Errorf("error burst on %q reported as a dominant warning:\n%s", svc, hyg)
			}
		}
	})
	if !strings.HasPrefix(run.report, "# Daily Application Log Briefing") {
		t.Errorf("report header wrong:\n%.120s", run.report)
	}
}

// runScenarioAnalysis seeds the scenario and runs the analyzer once,
// skipping when no test database is configured.
func runScenarioAnalysis(t *testing.T) scenarioRun {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; run `make test-integration` to exercise DB integration tests")
	}
	if u, err := url.Parse(dsn); err != nil || !strings.Contains(strings.TrimPrefix(u.Path, "/"), "test") {
		t.Skipf("refusing to seed a database whose name does not contain \"test\": %s", dsn)
	}
	ctx := context.Background()

	m, err := migrate.New("file://../../migrations", dsn)
	if err != nil {
		t.Fatalf("migrate new: %v", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrate up: %v", err)
	}
	_, _ = m.Close() //nolint:errcheck // best-effort close after migrate

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, "TRUNCATE applog_events"); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	// Seed on the hour so day boundaries line up with the aggregate window
	// the analyzer reads (postgres.hourFloor), exactly as the CLI does.
	now := time.Now().UTC().Truncate(time.Hour)
	rows, services, err := seedApplogScenario(ctx, pool, now, 300, 10000, 1, nil)
	if err != nil {
		t.Fatalf("seed scenario: %v", err)
	}
	t.Logf("seeded %d rows for %d services", rows, len(services))
	run := scenarioRun{byRole: map[string][]string{}}
	for _, s := range services {
		run.byRole[s.role] = append(run.byRole[s.role], s.name)
	}

	var captured struct {
		Messages []ollama.ChatMessage `json:"messages"`
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"models":[]}`)) })
	mux.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		_ = json.NewEncoder(w).Encode(ollama.ChatResponse{Message: ollama.ChatMessage{Role: "assistant", Content: scenarioReply}, PromptEvalCount: 1, EvalCount: 1})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := analyzer.New(postgres.NewStore(pool), ollama.New(srv.URL, 30*time.Second), analyzer.Config{Model: "fake"}, logger)
	res, err := a.Run(ctx, analyzer.RunParams{Feed: model.AnalysisFeedApplog, Period: 24 * time.Hour, Mode: model.AnalysisModeDaily})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(captured.Messages) != 2 {
		t.Fatalf("fake ollama captured %d messages, want 2", len(captured.Messages))
	}
	run.system, run.user, run.report = captured.Messages[0].Content, captured.Messages[1].Content, res.Report
	t.Logf("prompt: system %d bytes, user %d bytes (~%d tokens)", len(run.system), len(run.user), (len(run.system)+len(run.user))/4)
	if dir := os.Getenv("APPLOG_PROMPT_OUT"); dir != "" {
		for name, body := range map[string]string{"system.md": run.system, "user.md": run.user, "report.md": run.report} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
		}
	}
	return run
}

// checkScenarioRanking asserts the services with two recurring new
// templates come first and every error burst makes the ranked list. A
// burst's exact rank is not pinned: under the agreed rule a plain service
// whose chance three-row template is absent from the baseline ranks above
// a spike of a known template, and with 300 services that happens.
func checkScenarioRanking(t *testing.T, run scenarioRun) {
	t.Helper()
	ranked := regexp.MustCompile("(?m)^### (\\d+)\\. `([^`]+)`").FindAllStringSubmatch(run.user, -1)
	rankedNames := make([]string, 0, len(ranked))
	for _, m := range ranked {
		rankedNames = append(rankedNames, m[2])
	}
	want := analyzer.DefaultAppLogCaps().RankedServices
	if len(rankedNames) != want {
		t.Fatalf("ranked %d services, want %d: %v", len(rankedNames), want, rankedNames)
	}
	assertRankSet(t, rankedNames, 0, run.byRole[roleNewTemplate], "new-template services")
	for _, svc := range run.byRole[roleNoisy] {
		if !slices.Contains(rankedNames, svc) {
			t.Errorf("burst service %q not in the ranked list %v", svc, rankedNames)
		}
	}
}

func assertRankSet(t *testing.T, ranked []string, from int, want []string, what string) {
	t.Helper()
	got := slices.Clone(ranked[from : from+len(want)])
	want = slices.Clone(want)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("ranks %d-%d = %v, want the %s %v", from+1, from+len(want), got, what, want)
	}
}

// promptSection returns the body of the named H2 section of the user
// prompt, up to the next H2.
func promptSection(t *testing.T, usr, header string) string {
	t.Helper()
	i := strings.Index(usr, "\n## "+header)
	if i < 0 {
		t.Fatalf("section %q missing from prompt", header)
	}
	rest := usr[i+1:]
	if j := strings.Index(rest[3:], "\n## "); j >= 0 {
		rest = rest[:j+3]
	}
	return rest
}
