package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/taillight/internal/config"
)

// The "analysis" scenario writes a repeatable applog data set straight to
// the database: a few hundred services with a skewed volume distribution
// over a 7-day baseline plus a 24-hour window, with signals planted so an
// applog analysis run over the window has known answers. It bypasses the
// ingest API on purpose: the analyzer reads received_at, which the API
// stamps with the server clock, and a baseline needs backdated rows.

const (
	scenarioAnalysis  = "analysis"
	scenarioCopyChunk = 5000
	scenarioDays      = 8 // 7 baseline days + the 24-hour window.
)

// Planted roles. Each names the report section it should show up in.
const (
	roleNoisy          = "error burst"
	roleNewTemplate    = "new templates"
	roleSilent         = "silent"
	roleFresh          = "new service"
	roleOversize       = "oversize attrs"
	roleEmptyComponent = "empty component"
	roleDominant       = "dominant template"
)

var scenarioNouns = []string{
	"orders", "billing", "auth", "search", "catalog", "cart", "checkout", "shipping", "inventory", "pricing",
	"notify", "email", "sms", "push", "reports", "export", "import", "sync", "ledger", "payments",
	"fraud", "kyc", "users", "sessions", "tokens", "edge", "cache", "media", "images", "video",
	"feeds", "recs", "ranking", "geo", "maps", "routing", "fleet", "telemetry", "metrics", "audit",
	"config", "flags", "scheduler", "webhooks", "partners", "invoices", "refunds", "returns", "reviews", "support",
}

var scenarioSuffixes = []string{"api", "worker", "cron", "consumer", "gateway", "svc"}

// scenarioRoles assigns planted roles by volume rank so the mix covers
// busy and quiet services alike.
var scenarioRoles = map[int]string{
	2: roleNoisy, 6: roleNoisy, 11: roleNoisy, 19: roleNoisy, 39: roleNoisy,
	4: roleNewTemplate, 8: roleNewTemplate, 23: roleNewTemplate, 50: roleNewTemplate,
	14: roleSilent, 25: roleSilent,
	9: roleOversize, 30: roleEmptyComponent, 5: roleDominant,
}

type scenarioService struct {
	name      string
	hosts     []string
	share     float64 // fraction of the per-day volume
	component string
	role      string
}

// scenarioServices builds n services ranked by volume with a Zipf-like
// share, applies the planted roles, and appends the two new services that
// exist only in the window.
func scenarioServices(n int) []scenarioService {
	names := make([]string, 0, len(scenarioNouns)*len(scenarioSuffixes))
	for _, noun := range scenarioNouns {
		for _, suf := range scenarioSuffixes {
			names = append(names, noun+"-"+suf)
		}
	}
	n = min(n, len(names))
	weights := make([]float64, n)
	var total float64
	for i := range n {
		weights[i] = 1 / math.Pow(float64(i+1), 0.9)
		total += weights[i]
	}
	out := make([]scenarioService, 0, n+2)
	for i := range n {
		s := scenarioService{name: names[i], share: weights[i] / total, component: "http", role: scenarioRoles[i]}
		if s.role == roleEmptyComponent {
			s.component = ""
		}
		hostCount := 2
		if i < 20 {
			hostCount = 4
		}
		for h := range hostCount {
			s.hosts = append(s.hosts, fmt.Sprintf("%s-%02d.prod", s.name, h+1))
		}
		out = append(out, s)
	}
	out = append(out,
		scenarioService{name: "search-v2-api", hosts: []string{"search-v2-api-01.prod", "search-v2-api-02.prod"}, share: 0.01, component: "http", role: roleFresh},
		scenarioService{name: "ledger-migrator", hosts: []string{"ledger-migrator-01.prod"}, share: 0.002, component: "batch", role: roleFresh},
	)
	return out
}

// scenarioGen accumulates rows and flushes them with COPY.
type scenarioGen struct {
	rng   *rand.Rand
	pool  *pgxpool.Pool
	rows  [][]any
	total int
}

var scenarioColumns = []string{"received_at", "timestamp", "level", "service", "component", "host", "msg", "source", "attrs"}

func (g *scenarioGen) add(ctx context.Context, at time.Time, level string, s scenarioService, msg, source, attrs string) error {
	var attrsVal any
	if attrs != "" {
		attrsVal = attrs
	}
	host := s.hosts[g.rng.IntN(len(s.hosts))]
	g.rows = append(g.rows, []any{at, at, level, s.name, s.component, host, msg, source, attrsVal})
	if len(g.rows) >= scenarioCopyChunk {
		return g.flush(ctx)
	}
	return nil
}

func (g *scenarioGen) flush(ctx context.Context) error {
	if len(g.rows) == 0 {
		return nil
	}
	n, err := g.pool.CopyFrom(ctx, pgx.Identifier{"applog_events"}, scenarioColumns, pgx.CopyFromRows(g.rows))
	if err != nil {
		return fmt.Errorf("copy applog rows: %w", err)
	}
	g.total += int(n)
	g.rows = g.rows[:0]
	return nil
}

// between returns a uniformly random instant in [start, end).
func (g *scenarioGen) between(start, end time.Time) time.Time {
	return start.Add(time.Duration(g.rng.Int64N(int64(end.Sub(start)))))
}

// scenarioDeps are the dependency names the steady templates mention.
var scenarioDeps = []string{"db", "redis", "s3", "inventory-api", "pricing-api"}

// steadyRow emits one row of ordinary traffic. Numbers vary so the trigger
// folds each message into one pattern; the level mix is roughly 84% INFO,
// 10% DEBUG, 4% WARN, 1.8% ERROR, 0.2% FATAL.
func (g *scenarioGen) steadyRow(ctx context.Context, at time.Time, s scenarioService) error {
	dep := scenarioDeps[g.rng.IntN(len(scenarioDeps))]
	switch r := g.rng.IntN(1000); {
	case r < 2:
		return g.add(ctx, at, levelFatal, s, "panic: out of memory", "main.go:88", `{"rss_mb":4096}`)
	case r < 20:
		if g.rng.IntN(2) == 0 {
			return g.add(ctx, at, levelError, s, fmt.Sprintf("db timeout after %dms", 20+g.rng.IntN(500)), "store/query.go:120",
				`{"err":"context deadline exceeded","query":"select_order","db":"orders"}`)
		}
		return g.add(ctx, at, levelError, s, fmt.Sprintf("upstream %s returned %d", dep, 500+g.rng.IntN(4)), "client/http.go:77",
			fmt.Sprintf(`{"err":"bad gateway","upstream":%q,"status":502}`, dep))
	case r < 60:
		if g.rng.IntN(2) == 0 {
			return g.add(ctx, at, levelWarn, s, fmt.Sprintf("slow query took %dms on %s", 200+g.rng.IntN(2000), dep), "store/query.go:98",
				fmt.Sprintf(`{"dep":%q,"threshold_ms":200}`, dep))
		}
		return g.add(ctx, at, levelWarn, s, fmt.Sprintf("retrying call to %s attempt %d", dep, 1+g.rng.IntN(3)), "client/retry.go:31",
			fmt.Sprintf(`{"dep":%q,"backoff_ms":%d}`, dep, 50<<g.rng.IntN(4)))
	case r < 160:
		return g.add(ctx, at, levelDebug, s, fmt.Sprintf("trace span %d closed after %dms", g.rng.IntN(1_000_000), g.rng.IntN(300)), "", "")
	default:
		switch g.rng.IntN(3) {
		case 0:
			return g.add(ctx, at, levelInfo, s, fmt.Sprintf("request handled in %dms", g.rng.IntN(400)), "handler.go:42", "")
		case 1:
			return g.add(ctx, at, levelInfo, s, fmt.Sprintf("job %d completed in %dms", g.rng.IntN(100_000), g.rng.IntN(9000)), "", "")
		default:
			return g.add(ctx, at, levelInfo, s, fmt.Sprintf("cache refresh took %dms", g.rng.IntN(1500)), "cache.go:15", "")
		}
	}
}

// noisyBaselineRow is the burst template at its normal trickle, so the
// window's burst is a spike against baseline rather than a new signature.
func (g *scenarioGen) noisyRow(ctx context.Context, at time.Time, s scenarioService) error {
	return g.add(ctx, at, levelError, s, "upstream payments-gateway returned 504", "client/http.go:77",
		fmt.Sprintf(`{"err":"context deadline exceeded","upstream":"payments-gateway","status":504,"attempt":%d}`, 1+g.rng.IntN(3)))
}

const scenarioPanicStack = "goroutine 1 [running]:\nmain.(*Handler).Serve(...)\n\t/app/handler.go:42\nnet/http.serverHandler.ServeHTTP(...)\n\t/usr/local/go/src/net/http/server.go:3210"

// plantWindow emits the planted signals for one service into the window.
func (g *scenarioGen) plantWindow(ctx context.Context, s scenarioService, start, end time.Time) error {
	switch s.role {
	case roleNoisy:
		burst := end.Add(-3 * time.Hour)
		for range 600 {
			at := burst.Add(time.Duration(g.rng.Int64N(int64(40*time.Minute))) - 20*time.Minute)
			if err := g.noisyRow(ctx, at, s); err != nil {
				return err
			}
		}
	case roleNewTemplate:
		for range 30 {
			if err := g.add(ctx, g.between(end.Add(-6*time.Hour), end), levelError, s,
				"panic: runtime error: invalid memory address or nil pointer dereference", "handler.go:42",
				fmt.Sprintf(`{"stack":%q,"request_id":"%08x"}`, scenarioPanicStack, g.rng.Uint32())); err != nil {
				return err
			}
		}
		for range 40 {
			if err := g.add(ctx, g.between(end.Add(-6*time.Hour), end), levelWarn, s,
				fmt.Sprintf("circuit breaker open for redis-sessions (%d failures)", 5+g.rng.IntN(20)), "client/breaker.go:64",
				fmt.Sprintf(`{"dep":"redis-sessions","open_for_ms":%d}`, 1000*(1+g.rng.IntN(30)))); err != nil {
				return err
			}
		}
	case roleOversize:
		frames := make([]string, 0, 40)
		for i := range 40 {
			frames = append(frames, fmt.Sprintf("pkg/worker.(*Pool).run.func%d(...)\n\t/app/worker/pool.go:%d", i, 100+i))
		}
		attrs, err := json.Marshal(map[string]any{"stack": strings.Join(frames, "\n"), "worker": 7})
		if err != nil {
			return fmt.Errorf("marshal oversize attrs: %w", err)
		}
		for range 5 {
			if err := g.add(ctx, g.between(start, end), levelError, s,
				fmt.Sprintf("unhandled exception in worker %d", 1+g.rng.IntN(8)), "worker/pool.go:140", string(attrs)); err != nil {
				return err
			}
		}
	case roleDominant:
		for range 200 {
			if err := g.add(ctx, g.between(start, end), levelWarn, s,
				fmt.Sprintf("retrying call to cache attempt %d", 1+g.rng.IntN(5)), "client/retry.go:31",
				fmt.Sprintf(`{"dep":"cache","backoff_ms":%d}`, 50<<g.rng.IntN(4))); err != nil {
				return err
			}
		}
	}
	return nil
}

func runApplogScenario(ctx context.Context) error {
	if applogScenario != scenarioAnalysis {
		return fmt.Errorf("unknown scenario %q (want %q)", applogScenario, scenarioAnalysis)
	}
	if applogScenarioPerDay < 100 {
		return fmt.Errorf("per-day must be at least 100")
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	// Day boundaries sit on the hour so they line up with the analyzer's
	// hour-floored aggregate window; see postgres.hourFloor.
	now := time.Now().UTC().Truncate(time.Hour)
	fmt.Printf("scenario %s: ~%d events/day over %d days, seed %d, window ends %s\n",
		scenarioAnalysis, applogScenarioPerDay, scenarioDays, applogScenarioSeed, now.Format(time.RFC3339))
	start := time.Now()
	total, services, err := seedApplogScenario(ctx, pool, now, applogScenarioServices, applogScenarioPerDay, applogScenarioSeed,
		func(day, rows int) { fmt.Printf("  day -%d: %d rows so far\n", day, rows) })
	if err != nil {
		return err
	}
	fmt.Printf("done: %d applog rows for %d services in %s\n", total, len(services), time.Since(start).Round(time.Millisecond))
	printScenarioSummary(services, now)
	return nil
}

// seedApplogScenario writes the scenario rows for a window ending at now
// and returns the row count and the service list with its planted roles.
// progress, when non-nil, is called after each day is flushed. The
// integration test in this package seeds through it too, so the CLI and the
// test cannot drift apart.
func seedApplogScenario(ctx context.Context, pool *pgxpool.Pool, now time.Time, nServices, perDay int, seed int64, progress func(day, rows int)) (int, []scenarioService, error) {
	services := scenarioServices(nServices)
	g := &scenarioGen{
		rng:  rand.New(rand.NewPCG(uint64(seed), uint64(seed)+1)), //nolint:gosec // repeatable fixture data, not security-sensitive
		pool: pool,
	}
	for day := range scenarioDays {
		dayEnd := now.Add(-time.Duration(day) * 24 * time.Hour)
		dayStart := dayEnd.Add(-24 * time.Hour)
		if day == 0 {
			dayEnd = dayEnd.Add(-time.Minute) // keep the newest row inside a minute-truncated window
		}
		for _, s := range services {
			inWindow := day == 0
			if (s.role == roleSilent && inWindow) || (s.role == roleFresh && !inWindow) {
				continue
			}
			count := max(int(float64(perDay)*s.share), 5)
			for range count {
				if err := g.steadyRow(ctx, g.between(dayStart, dayEnd), s); err != nil {
					return g.total, nil, err
				}
			}
			// The burst and oversize templates keep a baseline trickle so the
			// window shows a spike and a hygiene fact, not new signatures.
			if s.role == roleNoisy {
				for range 5 {
					if err := g.noisyRow(ctx, g.between(dayStart, dayEnd), s); err != nil {
						return g.total, nil, err
					}
				}
			}
			if s.role == roleOversize && !inWindow {
				if err := g.add(ctx, g.between(dayStart, dayEnd), levelError, s,
					fmt.Sprintf("unhandled exception in worker %d", 1+g.rng.IntN(8)), "worker/pool.go:140", `{"worker":3}`); err != nil {
					return g.total, nil, err
				}
			}
			if inWindow {
				if err := g.plantWindow(ctx, s, dayStart, dayEnd); err != nil {
					return g.total, nil, err
				}
			}
		}
		if err := g.flush(ctx); err != nil {
			return g.total, nil, err
		}
		if progress != nil {
			progress(day, g.total)
		}
	}
	// Backdated rows land in hourly buckets the continuous aggregate may
	// already have materialized; the real-time view only reads raw rows
	// past its watermark. Refresh so the analyzer's stats see every day.
	if _, err := pool.Exec(ctx, "CALL refresh_continuous_aggregate('applog_summary_hourly', NULL, NULL)"); err != nil {
		return g.total, nil, fmt.Errorf("refresh applog_summary_hourly: %w", err)
	}
	return g.total, services, nil
}

// printScenarioSummary lists the planted signals so a report over the
// window can be checked against them.
func printScenarioSummary(services []scenarioService, now time.Time) {
	byRole := map[string][]string{}
	for _, s := range services {
		if s.role != "" {
			byRole[s.role] = append(byRole[s.role], s.name)
		}
	}
	roles := make([]string, 0, len(byRole))
	for r := range byRole {
		roles = append(roles, r)
	}
	sort.Strings(roles)
	fmt.Printf("\nplanted signals (window = %s → %s, baseline = the 7 days before):\n",
		now.Add(-24*time.Hour).Format("2006-01-02 15:04"), now.Format("2006-01-02 15:04 UTC"))
	detail := map[string]string{
		roleNoisy:          "600 ERROR `upstream payments-gateway returned <n>` around " + now.Add(-3*time.Hour).Format("15:04 UTC") + ", baseline 5/day",
		roleNewTemplate:    "30 ERROR `panic: runtime error: invalid memory address…` + 40 WARN `circuit breaker open for redis-sessions (<n> failures)`, neither in baseline",
		roleSilent:         "baseline traffic, no rows in the window",
		roleFresh:          "rows in the window only",
		roleOversize:       "5 ERROR `unhandled exception in worker <n>` rows with attrs over 1024 bytes; the template has a baseline trickle",
		roleEmptyComponent: "every row has an empty component",
		roleDominant:       "200 WARN `retrying call to cache attempt <n>`, most of its warn+ volume",
	}
	for _, r := range roles {
		fmt.Printf("  %-18s %s\n%22s%s\n", r+":", strings.Join(byRole[r], ", "), "", detail[r])
	}
}
