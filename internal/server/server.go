package server

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/audit"
	"github.com/caioricciuti/ch-ui/internal/clusterhealth"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/embedded"
	ghclient "github.com/caioricciuti/ch-ui/internal/github"
	"github.com/caioricciuti/ch-ui/internal/governance"
	"github.com/caioricciuti/ch-ui/internal/ingest"
	"github.com/caioricciuti/ch-ui/internal/ingest/spool"
	"github.com/caioricciuti/ch-ui/internal/mcpserver"
	"github.com/caioricciuti/ch-ui/internal/models"
	"github.com/caioricciuti/ch-ui/internal/oidc"
	"github.com/caioricciuti/ch-ui/internal/operations"
	performancemonitor "github.com/caioricciuti/ch-ui/internal/performance/monitor"
	"github.com/caioricciuti/ch-ui/internal/pipelines"
	"github.com/caioricciuti/ch-ui/internal/reports"
	"github.com/caioricciuti/ch-ui/internal/scheduler"
	"github.com/caioricciuti/ch-ui/internal/server/handlers"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/sqlalerts"
	"github.com/caioricciuti/ch-ui/internal/ssoidentity"
	"github.com/caioricciuti/ch-ui/internal/telemetry/monitor"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"github.com/go-chi/chi/v5"
)

// Server is the main HTTP server.
type Server struct {
	cfg                *config.Config
	db                 *database.DB
	gateway            *tunnel.Gateway
	scheduler          *scheduler.Runner
	pipelineRunner     *pipelines.Runner
	ingestHub          *pipelines.Hub
	ingestReceiver     *ingest.Receiver
	ingestHTTP         *http.Server
	modelRunner        *models.Runner
	modelScheduler     *models.Scheduler
	govSyncer          *governance.Syncer
	chHarvester        *clusterhealth.Harvester
	monitorRunner      *monitor.MonitorRunner
	sqlAlertRunner     *sqlalerts.Runner
	performanceMonitor *performancemonitor.Monitor
	operationsReports  *operations.Runner
	reportRunner       *reports.Runner
	githubSyncer       *ghclient.Syncer
	guardrails         *governance.GuardrailService
	alerts             *alerts.Dispatcher
	auditFwd           *audit.Forwarder
	oidcManager        *oidc.Manager
	ssoProvisioner     *ssoidentity.Provisioner
	ssoSweeper         *ssoidentity.Sweeper
	agents             *embedded.Manager
	router             chi.Router
	http               *http.Server
	frontendFS         fs.FS
}

// New creates a new Server with all routes configured.
func New(cfg *config.Config, db *database.DB, frontendFS fs.FS, agents *embedded.Manager) *Server {
	r := chi.NewRouter()
	gw := tunnel.NewGateway(db)

	// Pro background workers follow the license at runtime: each tick asks
	// its gate, so they pause when no license is active (after the grace
	// window) and resume on activation, without a restart.
	sched := scheduler.NewRunner(db, gw, cfg.AppSecretKey, config.NewProGate("Scheduled query jobs", cfg.ProAccess).Allow)
	pipeRunner := pipelines.NewRunner(db, gw, cfg)
	ingestHub := openIngestHub(cfg)
	pipeRunner.SetHub(ingestHub)
	modelRunner := models.NewRunner(db, gw, cfg.AppSecretKey)
	modelScheduler := models.NewScheduler(db, modelRunner)

	govStore := governance.NewStore(db)
	govSyncer := governance.NewSyncer(govStore, db, gw, cfg.AppSecretKey, config.NewProGate("Governance background sync", cfg.ProAccess).Allow)
	chHarvester := clusterhealth.NewHarvester(clusterhealth.NewStore(db), db, gw, cfg.AppSecretKey, config.NewProGate("Cluster health harvester", cfg.ProAccess).Allow)
	monitorRunner := monitor.NewMonitorRunner(db, gw, cfg.AppSecretKey, config.NewProGate("Telemetry monitors", cfg.ProAccess).Allow)
	githubSyncer := ghclient.NewSyncer(db, cfg.AppSecretKey)
	alertDispatcher := alerts.NewDispatcher(db, cfg)
	guardrails := governance.NewGuardrailService(govStore, db)

	// Initialize OIDC SSO if configured — via env/YAML (which always wins) or
	// via the admin-managed DB config. Discovery hits the network, so bound it
	// with a timeout; on failure we log and continue with OIDC disabled rather
	// than refusing to start. The manager lets the admin SSO settings endpoint
	// re-initialize the provider at runtime without a restart.
	oidcManager := oidc.NewManager()
	ssoProvisioner := &ssoidentity.Provisioner{DB: db, Gateway: gw, Secret: cfg.AppSecretKey}
	if settings, ok := handlers.ResolveOIDCSettings(cfg, db); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := oidcManager.Reload(ctx, settings)
		cancel()
		if err != nil {
			slog.Error("OIDC SSO disabled — provider discovery failed", "issuer", settings.IssuerURL, "source", settings.Source, "error", err)
			if settings.SSOOnly {
				slog.Warn("SSO only is set but SSO could not be loaded; password sign-in stays available until it loads")
			}
		} else {
			slog.Info("OIDC SSO enabled", "issuer", settings.IssuerURL, "source", settings.Source, "sso_only", settings.SSOOnly)
		}
	}
	handlers.RevokePasswordSessions(db, cfg, oidcManager, "system", "")
	if cfg.AllowPasswordLogin {
		slog.Warn("CHUI_ALLOW_PASSWORD_LOGIN is set: password sign-in is allowed even if SSO only is turned on. Unset it once you can sign in again.")
	}

	// Wire audit forwarding (SIEM) if any sink is configured. This is a Pro
	// feature checked per event, so it follows license activation and expiry
	// at runtime. Events raised while no license is active are dropped, not
	// queued.
	auditFwd := buildAuditForwarder(cfg)
	if auditFwd != nil {
		auditGate := config.NewProGate("Audit forwarding (SIEM)", cfg.ProAccess)
		db.OnAudit = func(p database.AuditLogParams) {
			if !auditGate.Allow() {
				return
			}
			// ch_user only when it differs from the actor, as stored.
			chUser := deref(p.ClickhouseUser)
			if chUser == deref(p.Username) {
				chUser = ""
			}
			auditFwd.Emit(audit.Event{
				Action:         p.Action,
				Username:       deref(p.Username),
				ClickhouseUser: chUser,
				ConnectionID:   deref(p.ConnectionID),
				Details:        deref(p.Details),
				IPAddress:      deref(p.IPAddress),
			})
		}
	}

	s := &Server{
		cfg:                cfg,
		db:                 db,
		gateway:            gw,
		scheduler:          sched,
		pipelineRunner:     pipeRunner,
		ingestHub:          ingestHub,
		ingestReceiver:     ingest.NewReceiver(db, ingestHub),
		modelRunner:        modelRunner,
		modelScheduler:     modelScheduler,
		govSyncer:          govSyncer,
		chHarvester:        chHarvester,
		monitorRunner:      monitorRunner,
		performanceMonitor: performancemonitor.New(db, gw, cfg.AppSecretKey, config.NewProGate("Performance monitor", cfg.ProAccess).Allow),
		operationsReports:  operations.NewRunner(db, gw, cfg),
		reportRunner:       reports.NewRunner(db, gw, guardrails, cfg.AppSecretKey, cfg.AppURL, config.NewProGate("Scheduled report delivery", cfg.ProAccess).Allow),
		githubSyncer:       githubSyncer,
		guardrails:         guardrails,
		sqlAlertRunner:     sqlalerts.NewRunner(db, gw, guardrails, cfg.AppSecretKey, config.NewProGate("SQL alerts", cfg.ProAccess).Allow),
		alerts:             alertDispatcher,
		auditFwd:           auditFwd,
		oidcManager:        oidcManager,
		ssoProvisioner:     ssoProvisioner,
		ssoSweeper:         ssoidentity.NewSweeper(db, ssoProvisioner, config.NewProGate("Per-person SSO user sweep", cfg.ProAccess).Allow),
		agents:             agents,
		router:             r,
		frontendFS:         frontendFS,
	}

	s.setupRoutes()

	s.http = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute, // Long for streaming SSE/queries
		IdleTimeout:  120 * time.Second,
	}
	if cfg.IngestAddr != "" {
		s.ingestHTTP = &http.Server{
			Addr:         cfg.IngestAddr,
			Handler:      s.ingestRouter(),
			ReadTimeout:  30 * time.Second,
			WriteTimeout: time.Minute,
			IdleTimeout:  120 * time.Second,
		}
	}

	return s
}

// openIngestHub opens the ingest spool in <data dir>/ingest-spool. When it
// cannot be opened the server still starts; webhook and ingest pipelines
// then refuse to start and the receivers answer 503.
func openIngestHub(cfg *config.Config) *pipelines.Hub {
	if cfg.DatabasePath == "" {
		// No data directory (tests): run without a spool.
		return pipelines.NewHub(nil)
	}
	dir := cfg.IngestDir()
	sp, err := spool.Open(dir, spool.Options{MaxBytes: cfg.IngestSpoolBytes(), MaxRecordBytes: ingestMaxRecordBytes})
	if err != nil {
		slog.Error("Ingest spool could not be opened; webhook and ingest pipelines are disabled", "dir", dir, "error", err)
		return pipelines.NewHub(nil)
	}
	st := sp.TotalStats()
	slog.Info("Ingest spool ready", "dir", dir, "max_bytes", cfg.IngestSpoolBytes(), "pending_bytes", st.Bytes, "pending_segments", st.Segments)
	return pipelines.NewHub(sp)
}

// ingestMaxRecordBytes bounds one spooled request after encoding. A 32 MB
// OTLP protobuf body grows when written as JSONEachRow.
const ingestMaxRecordBytes = 256 << 20

// ingestRouter serves only the receivers, for --ingest-addr: /ingest/*
// (OTLP and events under v1, the Elasticsearch bulk API under es, Splunk
// HEC under services/collector) and the same endpoints at the root, so a
// bare host:port works as OTEL_EXPORTER_OTLP_ENDPOINT and as a HEC URL.
func (s *Server) ingestRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	trustedProxies, err := s.cfg.TrustedProxyPrefixes()
	if err != nil {
		trustedProxies = nil
	}
	r.Use(middleware.RealIP(trustedProxies))
	r.Use(middleware.Metrics)
	r.Use(middleware.Logger)
	r.Use(middleware.BodyLimit(ingest.MaxBodyBytes))
	r.Get("/health", (&handlers.HealthHandler{}).Health)
	r.Route("/ingest", s.ingestReceiver.Routes)
	s.ingestReceiver.Routes(r)
	return r
}

func (s *Server) setupRoutes() {
	r := s.router
	cfg := s.cfg
	db := s.db
	gw := s.gateway

	// ── Global middleware ────────────────────────────────────────────────
	r.Use(middleware.Recoverer)
	// Resolve the client address first so every limit, audit entry and log
	// line below sees the same one. Validated at startup in cmd/server.go.
	trustedProxies, err := cfg.TrustedProxyPrefixes()
	if err != nil {
		slog.Error("Invalid trusted_proxies, trusting no proxy", "error", err)
		trustedProxies = nil
	}
	r.Use(middleware.RealIP(trustedProxies))
	r.Use(middleware.Metrics)
	r.Use(middleware.Logger)
	r.Use(middleware.SecurityHeaders(!cfg.DevMode))
	r.Use(middleware.CORS(middleware.CORSConfig{
		DevMode:        cfg.DevMode,
		AllowedOrigins: cfg.AllowedOrigins,
		AppURL:         cfg.AppURL,
	}))
	// Outer request-body bound (32MB). Uploads (25MB) and webhooks (10MB) set
	// their own stricter limits inside their handlers.
	r.Use(middleware.BodyLimit(32 << 20))

	// ── Health check (no auth) ──────────────────────────────────────────
	healthHandler := &handlers.HealthHandler{}
	r.Get("/health", healthHandler.Health)

	// ── Prometheus metrics (no auth; scrape on a trusted network) ───────
	r.Get("/metrics", middleware.MetricsHandler())

	// ── WebSocket tunnel endpoint (agent authenticates via token) ────────
	r.HandleFunc("/connect", gw.HandleWebSocket)

	// ── Embedded MCP server (streamable HTTP; chm_ bearer keys) ──────────
	r.Handle("/mcp", mcpserver.Handler(mcpserver.Deps{
		DB:             db,
		Gateway:        gw,
		Config:         cfg,
		Guardrails:     s.guardrails,
		ModelRunner:    s.modelRunner,
		PipelineRunner: s.pipelineRunner,
		ReportRunner:   s.reportRunner,
		IngestHub:      s.ingestHub,
	}))

	// ── OAuth 2.1 authorization server for /mcp ─────────────────────────
	oauthHandler := &handlers.OAuthHandler{DB: db, Config: cfg}
	r.Get("/.well-known/oauth-protected-resource", oauthHandler.ProtectedResourceMetadata)
	r.Get("/.well-known/oauth-protected-resource/mcp", oauthHandler.ProtectedResourceMetadata)
	r.Get("/.well-known/oauth-authorization-server", oauthHandler.AuthorizationServerMetadata)
	r.Get("/oauth/authorize", oauthHandler.Authorize)
	r.With(middleware.IPRateLimit(60, time.Minute)).Post("/oauth/token", oauthHandler.Token)
	r.With(middleware.IPRateLimit(20, time.Minute)).Post("/oauth/register", oauthHandler.Register)

	// ── Rate limiter (shared across handlers) ───────────────────────────
	rateLimiter := middleware.NewRateLimiter(db)

	// ── Webhook endpoints (no session — uses token auth) ──
	r.Post("/api/pipelines/webhook/{id}", pipelines.HandleWebhook)
	r.Post("/api/github/webhook/{connectionId}", handlers.GitHubWebhookHandler(db, cfg, s.githubSyncer))

	// ── Ingest receivers (no session — ingest key auth) ──
	if !(cfg.IngestOnlySeparate && cfg.IngestAddr != "") {
		r.Route("/ingest", s.ingestReceiver.Routes)
	}

	// ── API routes ─────────────────────────────────────────────────────
	r.Route("/api", func(api chi.Router) {
		// First-run setup: closes for good when an admin exists, otherwise
		// issues and logs the one-time setup code. The only place it runs.
		setupHandler := &handlers.SetupHandler{DB: db, RateLimiter: rateLimiter, Agents: s.agents}
		setupHandler.Start()

		// Auth routes (no session required, login creates the session)
		authHandler := &handlers.AuthHandler{
			DB:          db,
			Gateway:     gw,
			RateLimiter: rateLimiter,
			Config:      cfg,
			OIDC:        s.oidcManager,
			Setup:       setupHandler,
			Agents:      s.agents,
			Provisioner: s.ssoProvisioner,
		}
		api.Route("/auth", authHandler.Routes)

		// License status (no session required)
		licenseHandler := &handlers.LicenseHandler{DB: db, Config: cfg}
		api.Get("/license", licenseHandler.GetLicense)

		// Public dashboard endpoints (no session required). The per-IP limit is
		// an outer bound for every public request; panel queries that miss the
		// handler's short result cache are also limited per share token and IP
		// inside the handler, so a 25 panel TV dashboard refreshing every 30 s
		// fits with room to spare.
		publicDashboards := &handlers.DashboardsHandler{DB: db, Gateway: gw, Config: cfg, Guardrails: s.guardrails}
		api.With(middleware.IPRateLimit(1200, time.Minute)).Mount("/public/dashboards", publicDashboards.PublicRoutes())

		// All routes below require a valid session
		api.Group(func(protected chi.Router) {
			protected.Use(middleware.Session(db, gw))

			// License activation and removal change what every user can do, so
			// they are admin only. Reading the license stays public (above).
			protected.With(middleware.RequireAdmin(db)).Post("/license/activate", licenseHandler.ActivateLicense)
			protected.With(middleware.RequireAdmin(db)).Post("/license/deactivate", licenseHandler.DeactivateLicense)

			// Query execution (community)
			queryHandler := &handlers.QueryHandler{DB: db, Gateway: gw, Config: cfg, Guardrails: s.guardrails}
			protected.Route("/query", queryHandler.Routes)

			// Connections management. Reads (list/get/test) are available to any
			// authenticated user, but mutating a connection or revealing/rotating
			// its tunnel token — the credential an agent uses to bridge into the
			// customer's ClickHouse network — requires admin.
			connectionsHandler := &handlers.ConnectionsHandler{DB: db, Gateway: gw, Config: cfg, Agents: s.agents, Provisioner: s.ssoProvisioner}
			protected.Route("/connections", func(cr chi.Router) {
				cr.Get("/", connectionsHandler.List)
				cr.Get("/{id}", connectionsHandler.Get)
				cr.Post("/{id}/test", connectionsHandler.TestConnection)
				cr.Route("/{id}/background-credentials", connectionsHandler.BackgroundCredentialRoutes)

				cr.Group(func(ar chi.Router) {
					ar.Use(middleware.RequireAdmin(db))
					ar.Post("/", connectionsHandler.Create)
					ar.Put("/{id}", connectionsHandler.Update)
					ar.Delete("/{id}", connectionsHandler.Delete)
					ar.Get("/{id}/token", connectionsHandler.GetToken)
					ar.Post("/{id}/regenerate-token", connectionsHandler.RegenerateToken)
					ar.Put("/{id}/sso-account", connectionsHandler.SetSSOAccount)
					ar.Get("/{id}/sso-identity", connectionsHandler.GetSSOIdentity)
					ar.Put("/{id}/sso-identity", connectionsHandler.SetSSOIdentity)
					ar.Post("/{id}/sso-identity/setup-sql", connectionsHandler.SSOIdentitySetupSQL)
					ar.Post("/{id}/sso-identity/verify", connectionsHandler.VerifySSOIdentity)
				})
			})

			// OAuth consent (the SPA page calls these with the session cookie)
			protected.Route("/oauth/consent", oauthHandler.ConsentRoutes)

			// Saved queries (community; parameterized run is Pro-gated inside Routes)
			savedQueriesHandler := &handlers.SavedQueriesHandler{DB: db, Gateway: gw, Config: cfg}
			protected.Route("/saved-queries", savedQueriesHandler.Routes)

			// Query history (community)
			queryHistoryHandler := &handlers.QueryHistoryHandler{DB: db, Config: cfg}
			protected.Route("/query-history", queryHistoryHandler.Routes)

			// Data lineage (community): read on request with the session's
			// ClickHouse account, cached 30 s, no background work.
			lineageHandler := &handlers.LineageHandler{DB: db, Gateway: gw, Config: cfg}
			protected.Route("/lineage", lineageHandler.Routes)

			// ── Community features ─────────────────────────────────────
			// Dashboards
			dashboardsHandler := &handlers.DashboardsHandler{DB: db, Gateway: gw, Config: cfg, Guardrails: s.guardrails}
			protected.Mount("/dashboards", dashboardsHandler.Routes())

			// Telemetry (OpenTelemetry data exploration)
			telemetryHandler := &handlers.TelemetryHandler{DB: db, Gateway: gw, Config: cfg}
			protected.Mount("/telemetry", telemetryHandler.Routes())

			// Pipelines
			pipelinesHandler := &handlers.PipelinesHandler{DB: db, Gateway: gw, Config: cfg, Runner: s.pipelineRunner}
			protected.Mount("/pipelines", pipelinesHandler.Routes())
			ingestHandler := &handlers.IngestHandler{Config: cfg}
			protected.Get("/ingest/info", ingestHandler.Info)

			// Models (dbt-like SQL transformations)
			modelsHandler := &handlers.ModelsHandler{DB: db, Gateway: gw, Config: cfg, Runner: s.modelRunner}
			protected.Mount("/models", modelsHandler.Routes())

			// Brain AI assistant
			brainHandler := &handlers.BrainHandler{DB: db, Gateway: gw, Config: cfg, ModelRunner: s.modelRunner, PipelineRunner: s.pipelineRunner}
			protected.Route("/brain", brainHandler.Routes)

			// Admin routes (require admin role)
			adminHandler := &handlers.AdminHandler{
				DB:           db,
				Gateway:      gw,
				Config:       cfg,
				GovSyncer:    s.govSyncer,
				GitHubSyncer: s.githubSyncer,
				OIDCManager:  s.oidcManager,
				Provisioner:  s.ssoProvisioner,
			}
			protected.Route("/admin", func(ar chi.Router) {
				adminHandler.Routes(ar)
			})

			// MCP key management (admin-only: keys carry CH credentials)
			mcpKeysHandler := &handlers.MCPKeysHandler{DB: db, Config: cfg}
			protected.With(middleware.RequireAdmin(db)).Route("/mcp-keys", mcpKeysHandler.Routes)

			// Schema comparison (Pro). Both routes are reads, POST /compare
			// included, so they stay open in the read-only grace window.
			schemaHandler := &handlers.SchemaCompareHandler{DB: db, Gateway: gw, Config: cfg}
			protected.With(middleware.RequireProRead(cfg)).Mount("/schema-compare", schemaHandler.Routes())

			// ClickHouse access control editor (Pro, admin only). Gated per
			// route: reads and the plan stay open in the read-only grace
			// window, apply needs an active license.
			rbacHandler := &handlers.RBACHandler{DB: db, Gateway: gw, Config: cfg, Syncer: s.govSyncer}
			protected.Mount("/rbac", rbacHandler.Routes())

			// ── Pro-only features ──────────────────────────────────────
			protected.Group(func(pro chi.Router) {
				pro.Use(middleware.RequirePro(cfg))

				// Scheduled jobs
				schedulesHandler := &handlers.SchedulesHandler{DB: db, Gateway: gw, Config: cfg}
				pro.Route("/schedules", schedulesHandler.Routes)

				// Governance
				govHandler := &handlers.GovernanceHandler{
					DB: db, Gateway: gw, Config: cfg,
					Store:  s.govSyncer.GetStore(),
					Syncer: s.govSyncer,
				}
				pro.Mount("/governance", govHandler.Routes())

				// Cluster Health (Operations & Database monitoring)
				clusterHealthHandler := &handlers.ClusterHealthHandler{
					DB: db, Gateway: gw, Config: cfg,
					Store: s.chHarvester.GetStore(),
				}
				pro.Mount("/cluster-health", clusterHealthHandler.Routes())

				// Health checks and SQL threshold alerts
				alertingHandler := &handlers.AlertingHandler{DB: db, Gateway: gw, Config: cfg, Guardrails: s.guardrails}
				pro.Mount("/alerting", alertingHandler.Routes())

				// Query Insights (system.query_log analytics)
				queryInsightsHandler := &handlers.QueryInsightsHandler{DB: db, Gateway: gw, Config: cfg}
				pro.Mount("/query-insights", queryInsightsHandler.Routes())

				// Cost Center (showback/chargeback over query_log + parts)
				costsHandler := &handlers.CostsHandler{DB: db, Gateway: gw, Config: cfg}
				pro.Mount("/costs", costsHandler.Routes())

				performanceHandler := &handlers.PerformanceHandler{DB: db, Gateway: gw, Config: cfg}
				pro.Mount("/performance", performanceHandler.Routes())
				fleetHandler := &handlers.FleetHandler{DB: db, Gateway: gw, Config: cfg, RegressionCounts: func(connectionID string) (int, error) {
					state, err := db.GetPerformanceMonitor(connectionID)
					if err != nil {
						return 0, err
					}
					at, err := time.Parse(time.RFC3339Nano, state.ReportAt)
					if err != nil || !state.Enabled || state.Report == nil || state.LastError != "" || time.Since(at) > 2*time.Hour {
						return 0, fmt.Errorf("no recent successful performance scan")
					}
					return len(state.Report.Regressions), nil
				}}
				pro.Mount("/fleet", fleetHandler.Routes())
				reportsHandler := &handlers.OperationsReportsHandler{DB: db, Config: cfg, Runner: s.operationsReports}
				pro.Mount("/operations-reports", reportsHandler.Routes())
				reportDeliveryHandler := &handlers.ReportsHandler{DB: db, Config: cfg, Runner: s.reportRunner}
				pro.Mount("/reports", reportDeliveryHandler.Routes())
				timelineHandler := &handlers.IncidentTimelineHandler{DB: db, Gateway: gw, Config: cfg}
				pro.Route("/incident-timeline", timelineHandler.Routes)
			})
		})
	})

	// ── SPA fallback (serve embedded frontend) ──────────────────────────
	if s.frontendFS != nil {
		// Check whether the frontend was actually built and embedded.
		if _, err := s.frontendFS.Open("index.html"); err != nil {
			slog.Warn("Frontend assets not embedded; build the frontend first or use a release binary")
			r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprintln(w, "Frontend assets not available. Build the frontend first or use a release binary.")
			})
		} else {
			// Text assets are gzipped once and served compressed (compress.go).
			fileServer := CompressStatic(http.FileServer(http.FS(s.frontendFS)))
			r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
				// Try to serve the file directly
				path := r.URL.Path[1:] // strip leading /
				f, err := s.frontendFS.Open(path)
				if err != nil {
					// File not found — serve index.html for SPA routing
					w.Header().Set("Cache-Control", "no-cache")
					r.URL.Path = "/"
				} else {
					f.Close()
					if strings.HasPrefix(path, "assets/") {
						w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					} else {
						w.Header().Set("Cache-Control", "no-cache")
					}
				}
				fileServer.ServeHTTP(w, r)
			})
		}
	}

	slog.Info("Routes configured")
}

// Start starts the HTTP server on the configured port, all interfaces.
func (s *Server) Start() error {
	return s.serve(nil)
}

// Serve starts the HTTP server on a listener the caller already bound.
// `ch-ui desktop` uses it to serve on a loopback address only.
func (s *Server) Serve(ln net.Listener) error {
	s.http.Addr = ln.Addr().String()
	return s.serve(ln)
}

func (s *Server) serve(ln net.Listener) error {
	s.db.StartCleanupJobs()
	s.db.StartRetentionJob()
	s.scheduler.Start()
	s.pipelineRunner.Start()
	s.modelScheduler.Start()
	// The governance syncer and the cluster health harvester start whatever
	// the license says; their ticks skip while no Pro license is active.
	if s.db.GovernanceSyncEnabled() {
		s.govSyncer.StartBackground()
	} else {
		slog.Info("Governance background sync disabled (opt-in required; enable in Governance → Settings)")
	}
	s.chHarvester.StartBackground()
	s.monitorRunner.Start()
	s.sqlAlertRunner.Start()
	s.performanceMonitor.StartBackground()
	s.operationsReports.Start()
	s.reportRunner.Start()
	s.alerts.Start()
	// Locks and drops per-person SSO users that stopped signing in; the
	// ticks skip while no Pro license is active.
	s.ssoSweeper.Start()

	if s.cfg.IsPro() {
		if n, err := s.db.SweepStaleBrainApprovals(10 * time.Minute); err == nil && n > 0 {
			slog.Info("Swept stale brain approvals", "count", n)
		}
	}

	if s.ingestHTTP != nil {
		go s.serveIngest()
	}

	if s.cfg.TLSEnabled() {
		slog.Info("Server listening with native TLS", "addr", s.http.Addr)
		if ln != nil {
			return s.http.ServeTLS(ln, s.cfg.TLSCertFile, s.cfg.TLSKeyFile)
		}
		return s.http.ListenAndServeTLS(s.cfg.TLSCertFile, s.cfg.TLSKeyFile)
	}

	s.warnIfInsecure()
	slog.Info("Server listening", "addr", s.http.Addr)
	if ln != nil {
		return s.http.Serve(ln)
	}
	return s.http.ListenAndServe()
}

// serveIngest runs the separate ingest listener. A failure to bind is logged;
// the main server keeps running.
func (s *Server) serveIngest() {
	var err error
	if s.cfg.TLSEnabled() {
		slog.Info("Ingest listener with native TLS", "addr", s.ingestHTTP.Addr)
		err = s.ingestHTTP.ListenAndServeTLS(s.cfg.TLSCertFile, s.cfg.TLSKeyFile)
	} else {
		slog.Info("Ingest listener", "addr", s.ingestHTTP.Addr)
		err = s.ingestHTTP.ListenAndServe()
	}
	if err != nil && err != http.ErrServerClosed {
		slog.Error("Ingest listener stopped", "addr", s.ingestHTTP.Addr, "error", err)
	}
}

// warnIfInsecure logs a prominent warning when the server is about to serve
// plaintext HTTP without an obvious TLS terminator in front of it. Session
// cookies and ClickHouse credentials must not cross a network in cleartext.
func (s *Server) warnIfInsecure() {
	appURL := strings.ToLower(strings.TrimSpace(s.cfg.AppURL))
	// Behind an HTTPS reverse proxy or a loopback-only deployment is fine.
	if strings.HasPrefix(appURL, "https://") {
		return
	}
	if strings.Contains(appURL, "localhost") || strings.Contains(appURL, "127.0.0.1") {
		return
	}
	slog.Warn("Serving plaintext HTTP — session cookies and ClickHouse credentials are NOT encrypted in transit. " +
		"Terminate TLS at a reverse proxy, or set tls_cert_file/tls_key_file (TLS_CERT_FILE/TLS_KEY_FILE) to enable native TLS. " +
		"Do not expose CH-UI directly on an untrusted network without TLS.")
}

// buildAuditForwarder constructs the SIEM audit forwarder from config, or
// returns nil when no sink is configured.
func buildAuditForwarder(cfg *config.Config) *audit.Forwarder {
	var sinks []audit.Sink
	if cfg.AuditForwardStdout {
		sinks = append(sinks, audit.StdoutSink{})
	}
	if strings.TrimSpace(cfg.AuditLogFile) != "" {
		sinks = append(sinks, audit.NewFileSink(strings.TrimSpace(cfg.AuditLogFile)))
	}
	if strings.TrimSpace(cfg.AuditWebhookURL) != "" {
		sinks = append(sinks, audit.NewWebhookSink(strings.TrimSpace(cfg.AuditWebhookURL)))
	}
	return audit.NewForwarder(sinks...)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("Graceful shutdown initiated")
	s.scheduler.Stop()
	s.pipelineRunner.Stop()
	// After the pipelines: their drainers hold claimed segments until they
	// stop. Appends in flight finish first; later ones get 503.
	if err := s.ingestHub.Close(); err != nil {
		slog.Warn("Ingest spool close reported errors", "error", err)
	}
	s.modelScheduler.Stop()
	s.govSyncer.Stop()
	s.chHarvester.Stop()
	s.monitorRunner.Stop()
	s.sqlAlertRunner.Stop()
	s.performanceMonitor.Stop()
	s.operationsReports.Stop()
	s.reportRunner.Stop()
	s.alerts.Stop()
	s.ssoSweeper.Stop()
	s.gateway.Stop()
	s.auditFwd.Close()
	if s.ingestHTTP != nil {
		if err := s.ingestHTTP.Shutdown(ctx); err != nil {
			slog.Warn("Ingest listener shutdown", "error", err)
		}
	}
	return s.http.Shutdown(ctx)
}
