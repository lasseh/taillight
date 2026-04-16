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
	ldapauth "github.com/lasseh/taillight/internal/ldap"
	"github.com/lasseh/taillight/internal/metrics"
	"github.com/lasseh/taillight/internal/notification"
	"github.com/lasseh/taillight/internal/notification/backend"
	"github.com/lasseh/taillight/internal/ollama"
	"github.com/lasseh/taillight/internal/postgres"
	"github.com/lasseh/taillight/internal/scheduler"
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

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	store := postgres.NewStore(pool)
	authStore := postgres.NewAuthStore(pool)

	// Apply configurable retention policies.
	if err := store.ApplyRetentionPolicies(ctx, postgres.RetentionConfig{
		SrvlogDays:          cfg.Retention.SrvlogDays,
		NetlogDays:          cfg.Retention.NetlogDays,
		AppLogDays:          cfg.Retention.AppLogDays,
		NotificationLogDays: cfg.Retention.NotificationLogDays,
		RsyslogStatsDays:    cfg.Retention.RsyslogStatsDays,
		MetricsDays:         cfg.Retention.MetricsDays,
	}); err != nil {
		logger.Warn("failed to apply retention policies", "err", err)
	}

	// Seed continuous aggregates so summaries work immediately on fresh deploys.
	if err := store.RefreshContinuousAggregates(ctx); err != nil {
		logger.Warn("failed to refresh continuous aggregates", "err", err)
	}

	// Dedicated LISTEN connection.
	var listenChannels []string
	if cfg.Features.Srvlog {
		listenChannels = append(listenChannels, "srvlog_ingest")
	}
	if cfg.Features.Netlog {
		listenChannels = append(listenChannels, "netlog_ingest")
	}
	listener := postgres.NewListener(cfg.DatabaseURL, pool, cfg.NotificationBufferSize, logger, listenChannels)
	notifications, err := listener.Listen(ctx)
	if err != nil {
		return err
	}

	// SSE brokers.
	srvlogBroker := broker.NewSrvlogBroker(logger)
	var netlogBroker *broker.NetlogBroker
	if cfg.Features.Netlog {
		netlogBroker = broker.NewNetlogBroker(logger)
	}
	applogBroker := broker.NewAppLogBroker(logger)

	// Notification engine (optional).
	var notifEngine *notification.Engine
	if cfg.Notification.Enabled {
		notifEngine = notification.NewEngine(store, notification.Config{
			Enabled:             cfg.Notification.Enabled,
			RuleRefreshInterval: cfg.Notification.RuleRefreshInterval,
			DispatchWorkers:     cfg.Notification.DispatchWorkers,
			DispatchBuffer:      cfg.Notification.DispatchBuffer,
			DefaultBurstWindow:  cfg.Notification.DefaultBurstWindow,
			DefaultCooldown:     cfg.Notification.DefaultCooldown,
			DefaultMaxCooldown:  cfg.Notification.DefaultMaxCooldown,
			SendTimeout:         cfg.Notification.SendTimeout,
		}, logger)
		notifEngine.RegisterBackend(notification.ChannelTypeSlack, backend.NewSlack(logger))
		notifEngine.RegisterBackend(notification.ChannelTypeWebhook, backend.NewWebhook(logger))
		notifEngine.RegisterBackend(notification.ChannelTypeNtfy, backend.NewNtfy(logger))
		if cfg.SMTP.Host != "" {
			notifEngine.RegisterBackend(notification.ChannelTypeEmail, backend.NewEmail(backend.EmailGlobalConfig{
				Host:     cfg.SMTP.Host,
				Port:     cfg.SMTP.Port,
				Username: cfg.SMTP.Username,
				Password: cfg.SMTP.Password,
				From:     cfg.SMTP.From,
				TLS:      cfg.SMTP.TLS,
				AuthType: cfg.SMTP.AuthType,
			}, logger))
		}
		notifEngine.Start(ctx)
	}

	startBackgroundWorkers(ctx, logger, store, authStore, pool, notifications, srvlogBroker, netlogBroker, notifEngine, cfg.NotificationWorkers)

	// Analysis (optional).
	var analysisHandler *handler.AnalysisHandler
	if cfg.Analysis.Enabled {
		analysisHandler = setupAnalysis(ctx, cfg, store, logger)
	}

	// LDAP authentication (optional).
	var ldapAuth ldapauth.Authenticator
	if cfg.LDAP.Enabled {
		ldapAuth = ldapauth.NewClient(ldapauth.Config{
			URL:            cfg.LDAP.URL,
			StartTLS:       cfg.LDAP.StartTLS,
			TLSSkipVerify:  cfg.LDAP.TLSSkipVerify,
			BindDN:         cfg.LDAP.BindDN,
			BindPassword:   cfg.LDAP.BindPassword,
			UserSearchBase: cfg.LDAP.UserSearchBase,
			UserFilter:     cfg.LDAP.UserFilter,
			AdminGroup:     cfg.LDAP.AdminGroup,
		}, logger)
		logger.Info("LDAP authentication enabled", "url", cfg.LDAP.URL)
	}

	// Summary scheduler (optional — requires notification engine).
	var summaryScheduler *scheduler.SummaryScheduler
	if notifEngine != nil {
		summaryScheduler = scheduler.NewSummaryScheduler(store, notifEngine, logger)
		go summaryScheduler.Start(ctx)
	}

	r := setupRouter(cfg, logger, store, authStore, ldapAuth, srvlogBroker, netlogBroker, applogBroker, analysisHandler, notifEngine, summaryScheduler)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
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
	if netlogBroker != nil {
		netlogBroker.Shutdown()
	}
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

	// Drain auth touch worker before closing the pool.
	authStore.Stop()

	// Shutdown listener to close the LISTEN connection.
	if err := listener.Shutdown(shutdownCtx); err != nil {
		logger.Warn("listener shutdown error", "err", err)
	}

	// Flush remaining logs while the HTTP server is still accepting.
	if shipper != nil {
		if err := shipper.Shutdown(shutdownCtx); err != nil {
			logger.Warn("logshipper shutdown error", "err", err)
		}
	}

	return srv.Shutdown(shutdownCtx)
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
		host := cfg.LogShipper.Host
		if host == "" {
			host, _ = os.Hostname()
		}
		var err error
		shipper, err = logshipper.New(logshipper.Config{
			Endpoint:    "http://" + addr + "/api/v1/applog/ingest",
			APIKey:      logshipper.Secret(cfg.LogShipper.APIKey),
			Service:     cfg.LogShipper.Service,
			Component:   cfg.LogShipper.Component,
			Host:        host,
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
// DB pool metric collection, and expired session cleanup.
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
	notifWorkers int,
) {
	if notifWorkers <= 0 {
		notifWorkers = 4
	}

	// Bridge: fetch each notified row by ID and broadcast to SSE clients.
	// Multiple workers drain the channel concurrently so that DB fetch
	// latency doesn't cause backpressure under high event volume.
	for range notifWorkers {
		go func() {
			for n := range notifications {
				metrics.NotificationsReceivedTotal.WithLabelValues(n.Channel).Inc()
				switch n.Channel {
				case "srvlog_ingest":
					queryCtx, queryCancel := context.WithTimeout(ctx, 30*time.Second)
					event, err := store.GetSrvlog(queryCtx, n.ID)
					queryCancel()
					if err != nil {
						logger.Warn("fetch srvlog event for broadcast", "id", n.ID, "err", err)
						continue
					}
					srvlogBroker.Broadcast(event)
					if notifEngine != nil {
						notifEngine.HandleSrvlogEvent(event)
					}
				case "netlog_ingest":
					if netlogBroker != nil {
						queryCtx, queryCancel := context.WithTimeout(ctx, 30*time.Second)
						event, err := store.GetNetlog(queryCtx, n.ID)
						queryCancel()
						if err != nil {
							logger.Warn("fetch netlog event for broadcast", "id", n.ID, "err", err)
							continue
						}
						netlogBroker.Broadcast(event)
						if notifEngine != nil {
							notifEngine.HandleNetlogEvent(event)
						}
					}
				}
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
}

// setupAnalysis initializes the LLM analysis subsystem and starts the scheduler.
func setupAnalysis(ctx context.Context, cfg config.Config, store *postgres.Store, logger *slog.Logger) *handler.AnalysisHandler {
	ollamaClient := ollama.New(cfg.Analysis.OllamaURL)
	a := analyzer.New(store, ollamaClient, analyzer.Config{
		Model:       cfg.Analysis.Model,
		Temperature: cfg.Analysis.Temperature,
		NumCtx:      cfg.Analysis.NumCtx,
		Feed:        cfg.Analysis.Feed,
	}, logger)

	sched := scheduler.New(a, scheduler.Config{
		Enabled:    cfg.Analysis.Enabled,
		ScheduleAt: cfg.Analysis.ScheduleAt,
	}, logger)
	go sched.Start(ctx)

	return handler.NewAnalysisHandler(store, a)
}

// setupRouter builds the chi router with all middleware and route registrations.
func setupRouter(
	cfg config.Config,
	logger *slog.Logger,
	store *postgres.Store,
	authStore *postgres.AuthStore,
	ldapAuth ldapauth.Authenticator,
	srvlogBroker *broker.SrvlogBroker,
	netlogBroker *broker.NetlogBroker,
	applogBroker *broker.AppLogBroker,
	analysisHandler *handler.AnalysisHandler,
	notifEngine *notification.Engine,
	summaryScheduler *scheduler.SummaryScheduler,
) chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(handler.RequestLogger)
	if cfg.LogShipper.Enabled {
		r.Use(handler.SkipPath(handler.SkipPath(middleware.Logger, "/health"), "/api/v1/applog/ingest"))
	} else {
		r.Use(handler.SkipPath(middleware.Logger, "/health"))
	}
	r.Use(middleware.Recoverer)
	r.Use(metrics.HTTPMetrics)

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
		AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
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
	srvlogHandler := handler.NewSrvlogHandler(store)
	srvlogMetaHandler := handler.NewSrvlogMetaHandler(store)
	statsHandler := handler.NewStatsHandler(store, cfg.Features.Netlog)
	juniperHandler := handler.NewJuniperHandler(store)
	rsyslogStatsHandler := handler.NewRsyslogStatsHandler(store)
	taillightMetricsHandler := handler.NewTaillightMetricsHandler(store)
	srvlogSSEHandler := handler.NewSrvlogSSEHandler(srvlogBroker, store, logger)
	srvlogDeviceHandler := handler.NewSrvlogDeviceHandler(store)

	// Netlog handlers (feature-gated).
	var netlogHandler *handler.NetlogHandler
	var netlogSSEHandler *handler.NetlogSSEHandler
	var netlogMetaHandler *handler.NetlogMetaHandler
	var netlogDeviceHandler *handler.NetlogDeviceHandler
	if cfg.Features.Netlog && netlogBroker != nil {
		netlogHandler = handler.NewNetlogHandler(store)
		netlogSSEHandler = handler.NewNetlogSSEHandler(netlogBroker, store, logger)
		netlogMetaHandler = handler.NewNetlogMetaHandler(store)
		netlogDeviceHandler = handler.NewNetlogDeviceHandler(store)
	}

	// AppLog handlers.
	applogIngestHandler := handler.NewAppLogIngestHandler(store, applogBroker, logger, notifEngine)
	applogHandler := handler.NewAppLogHandler(store)
	applogSSEHandler := handler.NewAppLogSSEHandler(applogBroker, store, logger)
	applogMetaHandler := handler.NewAppLogMetaHandler(store)
	applogDeviceHandler := handler.NewAppLogDeviceHandler(store)
	authHandler := handler.NewAuthHandler(authStore, ldapAuth, cfg.CookieSecure)
	notifHandler := handler.NewNotificationHandler(store, notifEngine)
	summaryHandler := handler.NewSummaryHandler(store, summaryScheduler)
	exportHandler := handler.NewExportHandler(store, store, store)
	juniperRefHandler := handler.NewJuniperRefHandler(store)

	r.Route("/api/v1", func(r chi.Router) {
		if cfg.AuthEnabled {
			// Auth endpoints — unauthenticated (login must work without auth).
			r.Group(func(r chi.Router) {
				r.Use(middleware.Timeout(30 * time.Second))
				r.Post("/auth/login", authHandler.Login)
				r.Post("/auth/logout", authHandler.Logout)
			})

			// Authenticated auth endpoints (session or API key).
			r.Group(func(r chi.Router) {
				r.Use(middleware.Timeout(30 * time.Second))
				r.Use(auth.SessionOrAPIKey(authStore, authStore))
				r.Get("/auth/me", authHandler.Me)
				r.Patch("/auth/me/email", authHandler.UpdateEmail)
				r.Patch("/auth/me/preferences", authHandler.UpdatePreferences)
				r.Get("/auth/keys", authHandler.ListKeys)
				r.Post("/auth/keys", authHandler.CreateKey)
				r.Delete("/auth/keys/{id}", authHandler.RevokeKey)
				r.Post("/auth/sessions/revoke-all", authHandler.LogoutAll)

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
					r.Get("/export", exportHandler.ExportSrvlogs)
				})

				// REST endpoints — with request timeout.
				r.Group(func(r chi.Router) {
					r.Use(middleware.Timeout(30 * time.Second))

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

			// Netlog routes — feature-gated, all under /netlog/ prefix.
			if netlogHandler != nil {
				r.Route("/netlog", func(r chi.Router) {
					// SSE stream — long-lived, no timeout.
					r.Get("/stream", netlogSSEHandler.Stream)

					// Export — longer timeout for streaming CSV.
					r.Group(func(r chi.Router) {
						r.Use(middleware.Timeout(5 * time.Minute))
						r.Get("/export", exportHandler.ExportNetlogs)
					})

					// REST endpoints — with request timeout.
					r.Group(func(r chi.Router) {
						r.Use(middleware.Timeout(30 * time.Second))

						r.Get("/", netlogHandler.List)
						r.Get("/{id}", netlogHandler.Get)

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
			}

			// Shared endpoints — with request timeout.
			r.Group(func(r chi.Router) {
				r.Use(middleware.Timeout(30 * time.Second))

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
			if analysisHandler != nil {
				r.Group(func(r chi.Router) {
					r.Use(middleware.Timeout(30 * time.Second))
					r.Get("/analysis/reports", analysisHandler.List)
					r.Get("/analysis/reports/latest", analysisHandler.Latest)
					r.Get("/analysis/reports/{id}", analysisHandler.Get)
				})
			}

			// App log read endpoints.
			r.Route("/applog", func(r chi.Router) {
				// SSE stream — long-lived, no timeout.
				r.Get("/stream", applogSSEHandler.Stream)

				// Export — longer timeout for streaming CSV.
				r.Group(func(r chi.Router) {
					r.Use(middleware.Timeout(5 * time.Minute))
					r.Get("/export", exportHandler.ExportAppLogs)
				})

				// REST endpoints — with request timeout.
				r.Group(func(r chi.Router) {
					r.Use(middleware.Timeout(30 * time.Second))

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

			// Analysis trigger.
			if analysisHandler != nil {
				r.Group(func(r chi.Router) {
					r.Use(middleware.Timeout(15 * time.Minute))
					r.Post("/analysis/reports/trigger", analysisHandler.Trigger)
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
