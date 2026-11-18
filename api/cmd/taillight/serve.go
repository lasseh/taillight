package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	"github.com/lasseh/taillight/docs"
	"github.com/lasseh/taillight/internal/analyzer"
	"github.com/lasseh/taillight/internal/auth"
	"github.com/lasseh/taillight/internal/broker"
	"github.com/lasseh/taillight/internal/config"
	"github.com/lasseh/taillight/internal/handler"
	"github.com/lasseh/taillight/internal/ingestbridge"
	"github.com/lasseh/taillight/internal/juniperref"
	ldapauth "github.com/lasseh/taillight/internal/ldap"
	"github.com/lasseh/taillight/internal/metrics"
	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/mute"
	"github.com/lasseh/taillight/internal/netbox"
	"github.com/lasseh/taillight/internal/notification"
	"github.com/lasseh/taillight/internal/notification/backend"
	oidcauth "github.com/lasseh/taillight/internal/oidc"
	"github.com/lasseh/taillight/internal/ollama"
	"github.com/lasseh/taillight/internal/openai"
	"github.com/lasseh/taillight/internal/postgres"
	"github.com/lasseh/taillight/internal/scheduler"
	"github.com/lasseh/taillight/internal/worker"
	"github.com/lasseh/taillight/pkg/logshipper"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP/SSE server",
	RunE:  runServe,
}

func runServe(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger, shipper := setupLogger(cfg)
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Connection pool for queries.
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	poolCfg.MaxConns = cfg.DBMaxConns
	poolCfg.MinConns = cfg.DBMinConns
	poolCfg.ConnConfig.RuntimeParams["statement_timeout"] = "60000"
	// Instrument every pooled query with per-operation latency/error metrics.
	poolCfg.ConnConfig.Tracer = postgres.QueryTracer{}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Log reads (lists, stats, exports, SSE backfill) get their own pool, so
	// a read workload cannot take the connections that authentication, the
	// ingest bridge and /health need from the main pool.
	readPoolCfg := poolCfg.Copy()
	readPoolCfg.MaxConns = cfg.DBReadMaxConns
	readPoolCfg.MinConns = min(poolCfg.MinConns, cfg.DBReadMaxConns)
	readPool, err := pgxpool.NewWithConfig(ctx, readPoolCfg)
	if err != nil {
		return fmt.Errorf("read pool (db_read_max_conns): %w", err)
	}
	defer readPool.Close()

	store := postgres.NewStore(pool)
	readStore := postgres.NewStore(readPool)
	authStore := postgres.NewAuthStore(pool)

	// Apply configurable retention policies.
	retention := postgres.RetentionConfig{
		SrvlogDays:          cfg.Retention.SrvlogDays,
		NetlogDays:          cfg.Retention.NetlogDays,
		AppLogDays:          cfg.Retention.AppLogDays,
		NotificationLogDays: cfg.Retention.NotificationLogDays,
		RsyslogStatsDays:    cfg.Retention.RsyslogStatsDays,
		MetricsDays:         cfg.Retention.MetricsDays,
	}
	if err := store.ApplyRetentionPolicies(ctx, retention); err != nil {
		logger.Warn("failed to apply retention policies", "err", err)
	}

	// Seed continuous aggregates so summaries work immediately on fresh deploys.
	if err := store.RefreshContinuousAggregates(ctx); err != nil {
		logger.Warn("failed to refresh continuous aggregates", "err", err)
	}

	// Auto-import Juniper syslog reference XLSX files on a cold table.
	if cfg.JuniperRefPath != "" {
		if err := juniperref.AutoImport(ctx, logger, store, cfg.JuniperRefPath); err != nil {
			logger.Warn("juniper ref auto-import failed", "err", err)
		}
	}

	// Dedicated LISTEN connection.
	listenChannels := []string{"srvlog_ingest", "netlog_ingest"}
	listener := postgres.NewListener(cfg.DatabaseURL, pool, cfg.NotificationBufferSize, logger, listenChannels)
	notifications, err := listener.Listen(ctx)
	if err != nil {
		return err
	}

	// SSE brokers.
	srvlogBroker := broker.NewSrvlogBroker(logger)
	netlogBroker := broker.NewNetlogBroker(logger)
	applogBroker := broker.NewAppLogBroker(logger)

	// Notification engine (optional).
	var notifEngine *notification.Engine
	if cfg.Notification.Enabled {
		notifEngine = notification.NewEngine(store, cfg.Notification, logger)
		notifEngine.RegisterBackend(notification.ChannelTypeSlack, backend.NewSlack(logger))
		notifEngine.RegisterBackend(notification.ChannelTypeWebhook, backend.NewWebhook(logger))
		notifEngine.RegisterBackend(notification.ChannelTypeNtfy, backend.NewNtfy(logger))
		// Email backend is always registered so channels can be created without SMTP
		// configured. Send-time will surface a clear error if smtp.host is unset.
		notifEngine.RegisterBackend(notification.ChannelTypeEmail, backend.NewEmail(cfg.SMTP, logger))
		notifEngine.Start(ctx)
	}

	// Mute rules load before the NOTIFY workers start, so no early event is
	// dispatched unmarked. A failed load starts with nothing muted; the tick
	// retries every 30s.
	mutes := mute.NewSet(store, time.Now, logger)
	loadCtx, loadCancel := context.WithTimeout(ctx, 10*time.Second)
	if err := mutes.Reload(loadCtx); err != nil {
		logger.Warn("initial mute rule load failed; starting with nothing muted", "err", err)
	}
	loadCancel()
	go mutes.Run(ctx)

	startBackgroundWorkers(ctx, logger, store, authStore, pool, notifications, srvlogBroker, netlogBroker, notifEngine, mutes, cfg.NotificationWorkers, retention)

	// Analysis (optional).
	var analysis *analysisWiring
	if cfg.Analysis.Enabled {
		analysis, err = setupAnalysis(ctx, cfg, store, readStore, logger, notifEngine)
		if err != nil {
			return fmt.Errorf("analysis config: %w", err)
		}
	}

	// LDAP authentication (optional).
	var ldapAuth ldapauth.Authenticator
	if cfg.LDAP.Enabled {
		if err := cfg.LDAP.Validate(); err != nil {
			return fmt.Errorf("ldap config: %w", err)
		}
		ldapAuth = ldapauth.NewClient(cfg.LDAP.Config, logger)
		logger.Info("LDAP authentication enabled", "url", cfg.LDAP.URL)
	}

	// OIDC single sign-on (optional). Construction is network-free: discovery
	// runs lazily on the first login attempt, so an unreachable IdP at boot
	// does not keep taillight down.
	var oidcAuth *oidcauth.Provider
	if cfg.OIDC.Enabled {
		if err := cfg.OIDC.Validate(); err != nil {
			return err
		}
		oidcAuth = oidcauth.New(cfg.OIDC.Config, logger)
		logger.Info("OIDC authentication enabled", "issuer", cfg.OIDC.IssuerURL)
	}

	// Summary scheduler (optional — requires notification engine).
	var summaryScheduler *scheduler.SummaryScheduler
	if notifEngine != nil {
		summaryScheduler = scheduler.NewSummaryScheduler(store, notifEngine, logger)
		go summaryScheduler.Start(ctx)
	}

	// Netbox enrichment client (optional).
	nbClient := setupNetbox(cfg, logger)

	r := setupRouter(cfg, logger, store, readStore, authStore, ldapAuth, oidcAuth, srvlogBroker, netlogBroker, applogBroker, analysis, notifEngine, summaryScheduler, nbClient, mutes)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		// ReadTimeout bounds slow-drip request bodies (e.g. the unauthenticated
		// login endpoint and applog ingest), which ReadHeaderTimeout and chi's
		// context-only Timeout middleware do not. SSE routes are bodyless GETs,
		// so a whole-request read timeout does not affect streaming (audit S2).
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		logger.Info("starting server", "addr", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "err", err)
			cancel()
		}
	}()

	// Separate metrics server when configured.
	var metricsSrv *http.Server
	if cfg.MetricsAddr != "" {
		metricsSrv = startMetricsServer(cfg.MetricsAddr, logger)
	}

	<-ctx.Done()
	logger.Info("shutting down")

	// Close SSE brokers first so clients disconnect cleanly.
	srvlogBroker.Shutdown()
	netlogBroker.Shutdown()
	applogBroker.Shutdown()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer shutdownCancel()

	// Shutdown notification engine (drain dispatch queue).
	if notifEngine != nil {
		if err := notifEngine.Shutdown(shutdownCtx); err != nil {
			logger.Warn("notification engine shutdown error", "err", err)
		}
	}

	// Shutdown metrics server.
	if metricsSrv != nil {
		if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
			logger.Warn("metrics server shutdown error", "err", err)
		}
	}

	// Shutdown listener to close the LISTEN connection.
	if err := listener.Shutdown(shutdownCtx); err != nil {
		logger.Warn("listener shutdown error", "err", err)
	}

	// Flush remaining logs while the HTTP server is still accepting. With
	// self-log-shipping enabled this POSTs to the local ingest route, whose
	// auth middleware touches the API key — so both the HTTP server and the
	// auth touch worker must still be running at this point.
	if shipper != nil {
		if err := shipper.Shutdown(shutdownCtx); err != nil {
			logger.Warn("logshipper shutdown error", "err", err)
		}
	}

	// Drain the HTTP server (stop accepting, wait for in-flight requests) before
	// stopping the auth touch worker, so no in-flight authenticated request can
	// race the touch worker during shutdown (audit M5).
	srvErr := srv.Shutdown(shutdownCtx)

	// No requests are in flight now — drain the auth touch worker.
	authStore.Stop()

	// Stop the netbox cache janitor.
	if nbClient != nil {
		nbClient.Close()
	}

	return srvErr
}

// setupNetbox creates the netbox enrichment client when enabled.
// Returns nil when disabled or when client init fails.
func setupNetbox(cfg config.Config, logger *slog.Logger) *netbox.Client {
	if !cfg.Netbox.Enabled {
		return nil
	}
	nbCfg := cfg.Netbox.Config
	nbCfg.Logger = logger
	nbClient, err := netbox.NewClient(nbCfg)
	if err != nil {
		logger.Warn("netbox enrichment disabled: client init failed", "err", err)
		return nil
	}
	return nbClient
}

// setupLogger creates the application logger with optional log shipping.
func setupLogger(cfg config.Config) (*slog.Logger, *logshipper.Handler) {
	consoleHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})

	var shipper *logshipper.Handler
	var logHandler slog.Handler = consoleHandler
	if cfg.LogShipper.Enabled {
		addr := cfg.ListenAddr
		if strings.HasPrefix(addr, ":") {
			addr = "localhost" + addr
		}
		var err error
		shipper, err = logshipper.New(logshipper.Config{
			Endpoint:    "http://" + addr + "/api/v1/applog/ingest",
			APIKey:      logshipper.Secret(cfg.LogShipper.APIKey),
			Service:     cfg.LogShipper.Service,
			Component:   cfg.LogShipper.Component,
			Host:        cfg.LogShipper.Host, // Empty: the library resolves it and reports a failed lookup.
			AddSource:   true,
			MinLevel:    cfg.LogShipper.MinLevel,
			BatchSize:   cfg.LogShipper.BatchSize,
			FlushPeriod: cfg.LogShipper.FlushPeriod,
			BufferSize:  cfg.LogShipper.BufferSize,
		})
		if err != nil {
			slog.New(consoleHandler).Error("logshipper init failed", "error", err)
			return slog.New(consoleHandler), nil
		}
		logHandler = logshipper.MultiHandler(consoleHandler, shipper)
	}

	return slog.New(logHandler), shipper
}

// startBackgroundWorkers launches goroutines for notification bridging,
// DB pool metric collection, expired session cleanup, and meta cache pruning.
func startBackgroundWorkers(
	ctx context.Context,
	logger *slog.Logger,
	store *postgres.Store,
	authStore *postgres.AuthStore,
	pool *pgxpool.Pool,
	notifications <-chan postgres.Notification,
	srvlogBroker *broker.SrvlogBroker,
	netlogBroker *broker.NetlogBroker,
	notifEngine *notification.Engine,
	mutes ingestbridge.Muter,
	notifWorkers int,
	retention postgres.RetentionConfig,
) {
	if notifWorkers <= 0 {
		notifWorkers = 4
	}

	// Bridge: fetch each notified row by ID and received_at, mark it with its
	// mute rule, and broadcast it to SSE clients. Only unmuted events reach the
	// notification engine. Multiple workers drain the channel concurrently so
	// that DB fetch latency doesn't cause backpressure under high event volume.
	sinks := ingestbridge.Sinks{
		BroadcastSrvlog: func(e model.SrvlogEvent) {
			srvlogBroker.Broadcast(e)
			if e.MutedBy != nil {
				metrics.MutedEventsTotal.WithLabelValues("srvlog").Inc()
			}
		},
		BroadcastNetlog: func(e model.NetlogEvent) {
			netlogBroker.Broadcast(e)
			if e.MutedBy != nil {
				metrics.MutedEventsTotal.WithLabelValues("netlog").Inc()
			}
		},
	}
	if notifEngine != nil {
		sinks.NotifySrvlog = notifEngine.HandleSrvlogEvent
		sinks.NotifyNetlog = notifEngine.HandleNetlogEvent
	}

	for range notifWorkers {
		go func() {
			for n := range notifications {
				metrics.NotificationsReceivedTotal.WithLabelValues(n.Channel).Inc()
				ingestbridge.Dispatch(ctx, ingestbridge.Notification{Channel: n.Channel, ID: n.ID, ReceivedAt: n.ReceivedAt},
					store, mutes, sinks, logger, 30*time.Second)
			}
		}()
	}
	logger.Info("notification bridge started", "workers", notifWorkers)

	// Periodically collect application metrics: update Prometheus gauges
	// and insert a snapshot into the taillight_metrics hypertable.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Update DB pool gauges from pgxpool stats.
				stat := pool.Stat()
				metrics.DBPoolActiveConns.Set(float64(stat.AcquiredConns()))
				metrics.DBPoolIdleConns.Set(float64(stat.IdleConns()))
				metrics.DBPoolTotalConns.Set(float64(stat.TotalConns()))

				// Snapshot all metrics and persist.
				snap := metrics.Snapshot()
				queryCtx, queryCancel := context.WithTimeout(ctx, 30*time.Second)
				if err := store.InsertMetricsSnapshot(queryCtx, snap); err != nil {
					queryCancel()
					if ctx.Err() != nil {
						return // shutting down
					}
					logger.Warn("insert metrics snapshot", "err", err)
				} else {
					queryCancel()
				}
			}
		}
	}()

	// Periodically clean expired sessions.
	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				queryCtx, queryCancel := context.WithTimeout(ctx, 30*time.Second)
				n, err := authStore.CleanExpiredSessions(queryCtx)
				queryCancel()
				if err != nil {
					if ctx.Err() != nil {
						return // shutting down
					}
					logger.Warn("clean expired sessions", "err", err)
				} else if n > 0 {
					logger.Info("cleaned expired sessions", "count", n)
				}
			}
		}
	}()

	// Periodically drop filter-list values with no event inside their
	// feed's retention.
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				queryCtx, queryCancel := context.WithTimeout(ctx, time.Minute)
				n, err := store.PruneMetaCaches(queryCtx, retention)
				queryCancel()
				if err != nil {
					if ctx.Err() != nil {
						return // shutting down
					}
					logger.Warn("prune meta caches", "err", err)
				} else if n > 0 {
					logger.Info("pruned meta cache values", "count", n)
				}
			}
		}
	}()
}

// analysisWiring bundles the per-request handlers produced by setupAnalysis so
// the router can register both /analysis/reports and /analysis/schedules routes
// without growing setupRouter's parameter list further.
type analysisWiring struct {
	reports   *handler.AnalysisHandler
	schedules *handler.AnalysisScheduleHandler
	// readReports serves the report and scope-picker reads from the read
	// pool, the way the log read routes are served.
	readReports *handler.AnalysisHandler
}

// setupAnalysis initializes the LLM analysis subsystem: analyzer + queued
// worker + DB-backed schedule scheduler + HTTP handlers. It fails, before
// anything is started, on a backend configuration that cannot work.
func setupAnalysis(ctx context.Context, cfg config.Config, store, readStore *postgres.Store, logger *slog.Logger, notifEngine *notification.Engine) (*analysisWiring, error) {
	client, err := newAnalysisClient(cfg.Analysis)
	if err != nil {
		return nil, err
	}
	a := analyzer.New(store, client, analyzer.Config{
		API:         cfg.Analysis.API,
		Model:       cfg.Analysis.Model,
		Temperature: cfg.Analysis.Temperature,
		NumCtx:      cfg.Analysis.NumCtx,
		PromptsDir:  cfg.Analysis.PromptsDir,
		AppLog:      analyzer.AppLogCaps(cfg.Analysis.AppLog),
	}, logger)

	// Reconcile orphaned pending/running rows left by a previous crash before
	// the worker starts accepting new work.
	if n, err := store.ReconcileOrphanedReports(ctx); err != nil {
		logger.Error("reconcile orphaned analysis reports failed", "err", err)
	} else if n > 0 {
		logger.Info("reconciled orphaned analysis reports", "count", n)
	}

	completionCB := buildAnalysisCompletionCallback(notifEngine, store, logger)
	w := worker.NewAnalysis(store, a, logger, cfg.Analysis.RunTimeout, completionCB)
	go w.Start(ctx)

	sched := scheduler.NewAnalysisScheduler(store, w, logger)
	go sched.Start(ctx)

	return &analysisWiring{
		reports:     handler.NewAnalysisHandler(store, w),
		schedules:   handler.NewAnalysisScheduleHandler(store, sched),
		readReports: handler.NewAnalysisHandler(readStore, w),
	}, nil
}

// newAnalysisClient builds the chat client for the backend analysis.api
// names.
func newAnalysisClient(cfg config.AnalysisConfig) (analyzer.ChatClient, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.API != config.AnalysisAPIOpenAI {
		return ollama.New(cfg.OllamaURL, cfg.OllamaTimeout), nil
	}
	client, err := openai.New(openai.Config{
		BaseURL: cfg.APIURL,
		APIKey:  cfg.APIKey,
		CAFile:  cfg.CAFile,
		Timeout: cfg.OllamaTimeout,
	})
	if err != nil {
		return nil, err
	}
	return client, nil
}

// channelResolver loads a notification channel by id for completion dispatch.
type channelResolver interface {
	GetNotificationChannel(ctx context.Context, id int64) (notification.Channel, error)
}

// buildAnalysisCompletionCallback wires per-schedule email dispatch: each
// completed report carries the notification channel ids it was enqueued with
// (report.NotifyChannelIDs, snapshotted from the owning schedule). The worker
// resolves those channels live and mails the report through them.
//
// The callback is always wired when a notification engine exists — the
// per-report empty check lives inside the closure, NOT at boot. Gating on a
// global list here would leave the callback nil and silently kill every
// schedule's email, since targets no longer come from config.
//
// Channel ids are validated as existing email channels at schedule
// create/update time (handler). Resolving live (rather than snapshotting the
// channel config) means a channel edited after enqueue uses its current
// recipients; a channel deleted before completion is simply skipped.
func buildAnalysisCompletionCallback(engine *notification.Engine, store channelResolver, logger *slog.Logger) worker.CompletionCallback {
	if engine == nil {
		return nil
	}
	return func(ctx context.Context, r model.AnalysisReport) {
		if len(r.NotifyChannelIDs) == 0 {
			return
		}
		channels := make([]notification.Channel, 0, len(r.NotifyChannelIDs))
		for _, id := range r.NotifyChannelIDs {
			ch, err := store.GetNotificationChannel(ctx, id)
			if err != nil {
				// Channel deleted (or unreadable) since enqueue — skip it
				// rather than failing the whole dispatch.
				logger.Warn("analysis-report notify channel unavailable, skipping",
					"report_id", r.ID, "channel_id", id, "err", err)
				continue
			}
			channels = append(channels, ch)
		}
		engine.SendAnalysisReport(ctx, r, channels)
	}
}

// readTimeout is the request deadline on the log read REST routes (lists,
// stats, metadata, device views). The SSE streams sit outside it and the
// exports have their own. A variable so tests can shorten it.
var readTimeout = 30 * time.Second

// setupRouter builds the chi router with all middleware and route
// registrations. readStore serves the log read routes; store serves
// everything else, including auth (through authStore) and /health.
func setupRouter(
	cfg config.Config,
	logger *slog.Logger,
	store *postgres.Store,
	readStore *postgres.Store,
	authStore *postgres.AuthStore,
	ldapAuth ldapauth.Authenticator,
	oidcAuth *oidcauth.Provider,
	srvlogBroker *broker.SrvlogBroker,
	netlogBroker *broker.NetlogBroker,
	applogBroker *broker.AppLogBroker,
	analysis *analysisWiring,
	notifEngine *notification.Engine,
	summaryScheduler *scheduler.SummaryScheduler,
	nbClient *netbox.Client,
	mutes *mute.Set,
) chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(clientIPMiddleware(cfg))
	if cfg.RealIPHeader == "" {
		logger.Warn("real_ip_header not set — client IP resolves to the TCP peer; set real_ip_header (e.g. X-Real-IP) when behind a reverse proxy")
	} else if len(cfg.TrustedProxies) == 0 {
		logger.Warn("trusted_proxies not set — real_ip_header is trusted from any TCP peer; set trusted_proxies if the API is reachable without the proxy")
	}
	r.Use(handler.RequestLogger)
	if cfg.LogShipper.Enabled {
		r.Use(handler.SkipPath(handler.SkipPath(middleware.Logger, "/health"), "/api/v1/applog/ingest"))
	} else {
		r.Use(handler.SkipPath(middleware.Logger, "/health"))
	}
	// HTTPMetrics must be OUTER to Recoverer: a handler panic is caught by
	// Recoverer (which writes 500 into the wrapped writer), and HTTPMetrics —
	// recording after next.ServeHTTP — then observes status 500. Registered the
	// other way round, the panic unwinds past HTTPMetrics before it can record,
	// so panic-500s never reach Prometheus (audit S3).
	r.Use(metrics.HTTPMetrics)
	r.Use(middleware.Recoverer)

	// CORS — configurable allowed origins.
	corsOrigins := cfg.CORSAllowedOrigins
	if len(corsOrigins) == 0 {
		corsOrigins = []string{
			"http://localhost:5173", "http://localhost:3000",
			"http://[::1]:5173", "http://[::1]:3000",
		}
		logger.Warn("CORS defaulting to localhost dev origins — set cors_allowed_origins for production")
	}

	// Security headers (CSP connect-src includes CORS origins).
	r.Use(handler.SecurityHeaders(corsOrigins))

	// CORS credentials + wildcard origin is rejected by browsers (spec violation),
	// so only allow credentials when origins are explicitly listed.
	hasWildcard := slices.Contains(corsOrigins, "*")
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   corsOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "Last-Event-ID"},
		AllowCredentials: len(corsOrigins) > 0 && !hasWildcard,
		MaxAge:           300,
	}))

	if cfg.DemoMode {
		logger.Warn("demo mode enabled — all write endpoints return 403")
		r.Use(auth.DenyWrites("/api/v1/applog/ingest"))
	}

	if !cfg.AuthEnabled {
		logger.Warn("authentication is disabled — all endpoints are public")
	} else if !cfg.AuthReadEndpoints {
		logger.Warn("read endpoints are unauthenticated — set auth_read_endpoints=true for production")
	}

	// Srvlog handlers.
	srvlogHandler := handler.NewSrvlogHandler(readStore, mutes)
	srvlogMetaHandler := handler.NewSrvlogMetaHandler(readStore)
	statsHandler := handler.NewStatsHandler(readStore)
	juniperHandler := handler.NewJuniperHandler(readStore)
	rsyslogStatsHandler := handler.NewRsyslogStatsHandler(readStore)
	taillightMetricsHandler := handler.NewTaillightMetricsHandler(readStore)
	srvlogSSEHandler := handler.NewSrvlogSSEHandler(srvlogBroker, readStore, mutes, logger)
	srvlogDeviceHandler := handler.NewSrvlogDeviceHandler(readStore, mutes)

	// Netlog handlers.
	netlogHandler := handler.NewNetlogHandler(readStore, mutes)
	netlogSSEHandler := handler.NewNetlogSSEHandler(netlogBroker, readStore, mutes, logger)
	netlogMetaHandler := handler.NewNetlogMetaHandler(readStore)
	netlogDeviceHandler := handler.NewNetlogDeviceHandler(readStore, mutes)

	// Netbox enrichment handler (only enabled when configured).
	var netboxHandler *handler.NetboxHandler
	if nbClient != nil {
		netboxHandler = handler.NewNetboxHandler(nbClient, store, logger)
	}

	// AppLog handlers.
	applogIngestHandler := handler.NewAppLogIngestHandler(store, applogBroker, logger, notifEngine)
	applogHandler := handler.NewAppLogHandler(readStore)
	applogSSEHandler := handler.NewAppLogSSEHandler(applogBroker, readStore, logger)
	applogMetaHandler := handler.NewAppLogMetaHandler(readStore)
	applogDeviceHandler := handler.NewAppLogDeviceHandler(readStore)
	authHandler := handler.NewAuthHandler(authStore, ldapAuth, oidcAuth, cfg.CookieSecure)
	notifHandler := handler.NewNotificationHandler(store, notifEngine)
	summaryHandler := handler.NewSummaryHandler(store, summaryScheduler)
	muteRuleHandler := handler.NewMuteRuleHandler(store, mutes, time.Now)
	exportHandler := handler.NewExportHandler(readStore, readStore, readStore)
	// Without real_ip_header an anonymous client's IP may be the reverse
	// proxy's, shared by every user behind it, so anonymous reads are then
	// bounded only by the read pool.
	anonReadLimit := handler.ReadLimitPerPrincipal
	if cfg.RealIPHeader == "" {
		anonReadLimit = int(cfg.DBReadMaxConns)
	}
	readLimit := handler.NewReadLimiter(anonReadLimit)
	juniperRefHandler := handler.NewJuniperRefHandler(store)
	configHandler := handler.NewConfigHandler(cfg.Analysis.Enabled, cfg.OIDC.Enabled)

	r.Route("/api/v1", func(r chi.Router) {
		// Runtime config — unauthenticated so the frontend can fetch feature
		// flags before the login UI renders.
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(30 * time.Second))
			r.Get("/config/features", configHandler.Features)
		})

		if cfg.AuthEnabled {
			// Auth endpoints — unauthenticated (login must work without auth).
			r.Group(func(r chi.Router) {
				r.Use(middleware.Timeout(30 * time.Second))
				r.Post("/auth/login", authHandler.Login)
				r.Post("/auth/logout", authHandler.Logout)
				if oidcAuth != nil {
					r.Get("/auth/oidc/login", authHandler.OIDCLogin)
					r.Get("/auth/oidc/callback", authHandler.OIDCCallback)
				}
			})

			// Authenticated auth endpoints (session or API key).
			r.Group(func(r chi.Router) {
				r.Use(middleware.Timeout(30 * time.Second))
				r.Use(auth.SessionOrAPIKey(authStore, authStore))
				r.Get("/auth/me", authHandler.Me)
				r.Get("/auth/keys", authHandler.ListKeys)

				// Keys are minted from a session only, so no key can create
				// another key that outlives it.
				r.With(auth.RequireSession).Post("/auth/keys", authHandler.CreateKey)

				// Account writes — a session, or an API key with the admin grant.
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireSessionOrAdminKey)
					r.Patch("/auth/me/email", authHandler.UpdateEmail)
					r.Patch("/auth/me/preferences", authHandler.UpdatePreferences)
					r.Delete("/auth/keys/{id}", authHandler.RevokeKey)
					r.Post("/auth/sessions/revoke-all", authHandler.LogoutAll)
				})

				// User management — admin scope + handler-level checks (defense-in-depth).
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireScope("admin"))
					r.Get("/auth/users", authHandler.ListUsers)
					r.Post("/auth/users", authHandler.CreateUser)
					r.Patch("/auth/users/{id}/active", authHandler.SetUserActive)
					r.Patch("/auth/users/{id}/password", authHandler.UpdateUserPassword)
					r.Post("/auth/users/{id}/revoke-sessions", authHandler.RevokeUserSessions)
				})
			})
		} else {
			// Auth disabled: /auth/me returns anonymous user, login/logout are no-ops.
			r.Group(func(r chi.Router) {
				r.Use(middleware.Timeout(30 * time.Second))
				r.Use(auth.AllowAnonymous)
				r.Get("/auth/me", authHandler.Me)
				r.Post("/auth/login", authHandler.Me)
				r.Post("/auth/logout", func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck // Static map encode cannot fail; write error is not recoverable.
				})
			})
		}

		// Read-scoped routes (all GET endpoints).
		r.Group(func(r chi.Router) {
			if cfg.AuthEnabled && cfg.AuthReadEndpoints {
				r.Use(auth.SessionOrAPIKey(authStore, authStore))
			} else {
				r.Use(auth.AllowAnonymous)
			}
			r.Use(auth.RequireScope("read"))

			// Srvlog routes — all under /srvlog/ prefix.
			r.Route("/srvlog", func(r chi.Router) {
				// SSE stream — long-lived, no timeout.
				r.Get("/stream", srvlogSSEHandler.Stream)

				// Export — longer timeout for streaming CSV.
				r.Group(func(r chi.Router) {
					r.Use(middleware.Timeout(5 * time.Minute))
					r.Use(readLimit.Middleware)
					r.Get("/export", exportHandler.ExportSrvlogs)
				})

				// REST endpoints — with request timeout.
				r.Group(func(r chi.Router) {
					r.Use(middleware.Timeout(readTimeout))
					r.Use(readLimit.Middleware)

					r.Get("/", srvlogHandler.List)
					r.Get("/{id}", srvlogHandler.Get)

					r.Get("/meta/hosts", srvlogMetaHandler.Hosts)
					r.Get("/meta/programs", srvlogMetaHandler.Programs)
					r.Get("/meta/facilities", srvlogMetaHandler.Facilities)
					r.Get("/meta/tags", srvlogMetaHandler.Tags)

					r.Get("/stats/volume", statsHandler.Volume)
					r.Get("/stats/severity-volume", statsHandler.SeverityVolume)
					r.Get("/stats/summary", statsHandler.SrvlogSummary)
					r.Get("/stats/hosts", statsHandler.Hosts)

					r.Get("/device/{hostname}", srvlogDeviceHandler.Get)
				})
			})

			// Netlog routes — all under /netlog/ prefix.
			r.Route("/netlog", func(r chi.Router) {
				// SSE stream — long-lived, no timeout.
				r.Get("/stream", netlogSSEHandler.Stream)

				// Export — longer timeout for streaming CSV.
				r.Group(func(r chi.Router) {
					r.Use(middleware.Timeout(5 * time.Minute))
					r.Use(readLimit.Middleware)
					r.Get("/export", exportHandler.ExportNetlogs)
				})

				// REST endpoints — with request timeout.
				r.Group(func(r chi.Router) {
					r.Use(middleware.Timeout(readTimeout))
					r.Use(readLimit.Middleware)

					r.Get("/", netlogHandler.List)
					r.Get("/{id}", netlogHandler.Get)

					if netboxHandler != nil {
						r.Get("/{id}/netbox", netboxHandler.EnrichNetlog)
					}

					r.Get("/meta/hosts", netlogMetaHandler.Hosts)
					r.Get("/meta/programs", netlogMetaHandler.Programs)
					r.Get("/meta/facilities", netlogMetaHandler.Facilities)
					r.Get("/meta/tags", netlogMetaHandler.Tags)

					r.Get("/stats/volume", statsHandler.NetlogVolume)
					r.Get("/stats/severity-volume", statsHandler.NetlogSeverityVolume)
					r.Get("/stats/summary", statsHandler.NetlogSummary)

					r.Get("/device/{hostname}", netlogDeviceHandler.Get)
				})
			})

			// Shared endpoints — with request timeout.
			r.Group(func(r chi.Router) {
				r.Use(middleware.Timeout(readTimeout))
				r.Use(readLimit.Middleware)

				r.Route("/juniper", func(r chi.Router) {
					r.Get("/lookup", juniperHandler.Lookup)
				})

				r.Route("/rsyslog", func(r chi.Router) {
					r.Get("/stats/summary", rsyslogStatsHandler.Summary)
					r.Get("/stats/volume", rsyslogStatsHandler.Volume)
				})

				r.Route("/metrics", func(r chi.Router) {
					r.Get("/summary", taillightMetricsHandler.Summary)
					r.Get("/volume", taillightMetricsHandler.Volume)
				})
			})

			// Analysis read endpoints.
			if analysis != nil {
				r.Group(func(r chi.Router) {
					r.Use(middleware.Timeout(30 * time.Second))
					r.Use(readLimit.Middleware)
					r.Get("/analysis/reports", analysis.readReports.List)
					r.Get("/analysis/reports/{slug}", analysis.readReports.Get)
					r.Get("/analysis/reports/{slug}/print", analysis.readReports.Print)
					r.Get("/analysis/hosts", analysis.readReports.Hosts)
					r.Get("/analysis/services", analysis.readReports.Services)
					r.Get("/analysis/schedules", analysis.schedules.List)
					r.Get("/analysis/schedules/{id}", analysis.schedules.Get)
				})
			}

			// App log read endpoints.
			r.Route("/applog", func(r chi.Router) {
				// SSE stream — long-lived, no timeout.
				r.Get("/stream", applogSSEHandler.Stream)

				// Export — longer timeout for streaming CSV.
				r.Group(func(r chi.Router) {
					r.Use(middleware.Timeout(5 * time.Minute))
					r.Use(readLimit.Middleware)
					r.Get("/export", exportHandler.ExportAppLogs)
				})

				// REST endpoints — with request timeout.
				r.Group(func(r chi.Router) {
					r.Use(middleware.Timeout(readTimeout))
					r.Use(readLimit.Middleware)

					r.Get("/", applogHandler.List)
					r.Get("/{id}", applogHandler.Get)

					r.Get("/meta/services", applogMetaHandler.Services)
					r.Get("/meta/components", applogMetaHandler.Components)
					r.Get("/meta/hosts", applogMetaHandler.Hosts)

					r.Get("/stats/volume", statsHandler.AppLogVolume)
					r.Get("/stats/severity-volume", statsHandler.AppLogSeverityVolume)
					r.Get("/stats/summary", statsHandler.AppLogSummary)

					r.Get("/device/{hostname}", applogDeviceHandler.Get)
				})
			})

			// Notification read endpoints.
			r.Group(func(r chi.Router) {
				r.Use(middleware.Timeout(30 * time.Second))
				r.Get("/notifications/channels", notifHandler.ListChannels)
				r.Get("/notifications/channels/{id}", notifHandler.GetChannel)
				r.Get("/notifications/rules", notifHandler.ListRules)
				r.Get("/notifications/rules/{id}", notifHandler.GetRule)
				r.Get("/notifications/log", notifHandler.ListLog)
				r.Get("/notifications/summaries", summaryHandler.ListSchedules)
				r.Get("/notifications/summaries/{id}", summaryHandler.GetSchedule)
			})

			// Mute rule read endpoint.
			r.Group(func(r chi.Router) {
				r.Use(middleware.Timeout(30 * time.Second))
				r.Get("/mute-rules", muteRuleHandler.List)
			})
		})

		// Ingest-scoped route.
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(30 * time.Second))
			if cfg.AuthEnabled {
				r.Use(auth.SessionOrAPIKey(authStore, authStore))
			} else {
				r.Use(auth.AllowAnonymous)
			}
			r.Use(auth.RequireScope("ingest"))
			r.Post("/applog/ingest", applogIngestHandler.Ingest)
		})

		// Admin-scoped routes (write operations).
		r.Group(func(r chi.Router) {
			if cfg.AuthEnabled {
				r.Use(auth.SessionOrAPIKey(authStore, authStore))
			} else {
				r.Use(auth.AllowAnonymous)
			}
			r.Use(auth.RequireScope("admin"))

			// Analysis write endpoints (create/delete reports, schedule CRUD,
			// run-now). The async worker absorbs long-running work so each HTTP
			// request can return promptly under the standard 30s timeout.
			if analysis != nil {
				r.Group(func(r chi.Router) {
					r.Use(middleware.Timeout(30 * time.Second))
					r.Post("/analysis/reports", analysis.reports.Create)
					r.Delete("/analysis/reports/{slug}", analysis.reports.Delete)
					r.Post("/analysis/schedules", analysis.schedules.Create)
					r.Put("/analysis/schedules/{id}", analysis.schedules.Update)
					r.Delete("/analysis/schedules/{id}", analysis.schedules.Delete)
					r.Post("/analysis/schedules/{id}/run", analysis.schedules.Run)
				})
			}

			// Notification write endpoints.
			r.Group(func(r chi.Router) {
				r.Use(middleware.Timeout(30 * time.Second))
				r.Post("/notifications/channels", notifHandler.CreateChannel)
				r.Put("/notifications/channels/{id}", notifHandler.UpdateChannel)
				r.Delete("/notifications/channels/{id}", notifHandler.DeleteChannel)
				r.Post("/notifications/channels/{id}/test", notifHandler.TestChannel)
				r.Post("/notifications/rules", notifHandler.CreateRule)
				r.Put("/notifications/rules/{id}", notifHandler.UpdateRule)
				r.Delete("/notifications/rules/{id}", notifHandler.DeleteRule)
				r.Post("/notifications/summaries", summaryHandler.CreateSchedule)
				r.Put("/notifications/summaries/{id}", summaryHandler.UpdateSchedule)
				r.Delete("/notifications/summaries/{id}", summaryHandler.DeleteSchedule)
				r.Post("/notifications/summaries/{id}/trigger", summaryHandler.TriggerSchedule)
			})

			// Mute rule write endpoints: create, expire-in, unmute.
			r.Group(func(r chi.Router) {
				r.Use(middleware.Timeout(30 * time.Second))
				r.Post("/mute-rules", muteRuleHandler.Create)
				r.Patch("/mute-rules/{id}", muteRuleHandler.ExpireIn)
				r.Delete("/mute-rules/{id}", muteRuleHandler.Unmute)
			})

			// Juniper reference XLSX upload — longer timeout for multipart parse+upsert.
			r.Group(func(r chi.Router) {
				r.Use(middleware.Timeout(2 * time.Minute))
				r.Post("/juniper/ref/upload", juniperRefHandler.Upload)
			})
		})
	})

	// API docs.
	r.Get("/api/v1/openapi.yml", docs.SpecHandler())
	r.Get("/api/docs", docs.ScalarHandler())

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := store.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy"}) //nolint:errcheck // Static map encode cannot fail; write error is not recoverable.
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"}) //nolint:errcheck // Static map encode cannot fail; write error is not recoverable.
	})

	return r
}

// startMetricsServer starts a dedicated HTTP server for Prometheus metrics.
func startMetricsServer(addr string, logger *slog.Logger) *http.Server {
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())
	srv := &http.Server{
		Addr:              addr,
		Handler:           metricsMux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	go func() {
		logger.Info("starting metrics server", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("metrics server error", "err", err)
		}
	}()
	return srv
}
