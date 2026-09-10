package server

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
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
	"github.com/caioricciuti/ch-ui/internal/mcpserver"
	"github.com/caioricciuti/ch-ui/internal/models"
	"github.com/caioricciuti/ch-ui/internal/oidc"
	"github.com/caioricciuti/ch-ui/internal/pipelines"
	"github.com/caioricciuti/ch-ui/internal/scheduler"
	"github.com/caioricciuti/ch-ui/internal/server/handlers"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"github.com/go-chi/chi/v5"
)

// Server is the main HTTP server.
type Server struct {
	cfg            *config.Config
	db             *database.DB
	gateway        *tunnel.Gateway
	scheduler      *scheduler.Runner
	pipelineRunner *pipelines.Runner
	modelRunner    *models.Runner
	modelScheduler *models.Scheduler
	govSyncer      *governance.Syncer
	chHarvester    *clusterhealth.Harvester
	githubSyncer   *ghclient.Syncer
	guardrails     *governance.GuardrailService
	alerts         *alerts.Dispatcher
	auditFwd       *audit.Forwarder
	oidcManager    *oidc.Manager
	agents         *embedded.Manager
	router         chi.Router
	http           *http.Server
	frontendFS     fs.FS
}

// New creates a new Server with all routes configured.
func New(cfg *config.Config, db *database.DB, frontendFS fs.FS, agents *embedded.Manager) *Server {
	r := chi.NewRouter()
	gw := tunnel.NewGateway(db)

	sched := scheduler.NewRunner(db, gw, cfg.AppSecretKey)
	pipeRunner := pipelines.NewRunner(db, gw, cfg)
	modelRunner := models.NewRunner(db, gw, cfg.AppSecretKey)
	modelScheduler := models.NewScheduler(db, modelRunner)

	govStore := governance.NewStore(db)
	govSyncer := governance.NewSyncer(govStore, db, gw, cfg.AppSecretKey)
	chHarvester := clusterhealth.NewHarvester(clusterhealth.NewStore(db), db, gw, cfg.AppSecretKey)
	githubSyncer := ghclient.NewSyncer(db, cfg.AppSecretKey)
	alertDispatcher := alerts.NewDispatcher(db, cfg)

	// Initialize OIDC SSO if configured — via env/YAML (which always wins) or
	// via the admin-managed DB config. Discovery hits the network, so bound it
	// with a timeout; on failure we log and continue with OIDC disabled rather
	// than refusing to start. The manager lets the admin SSO settings endpoint
	// re-initialize the provider at runtime without a restart.
	oidcManager := oidc.NewManager()
	if settings, ok := handlers.ResolveOIDCSettings(cfg, db); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := oidcManager.Reload(ctx, settings)
		cancel()
		if err != nil {
			slog.Error("OIDC SSO disabled — provider discovery failed", "issuer", settings.IssuerURL, "source", settings.Source, "error", err)
		} else {
			slog.Info("OIDC SSO enabled", "issuer", settings.IssuerURL, "source", settings.Source)
		}
	}

	// Wire audit forwarding (SIEM) if any sink is configured. This is a Pro
	// feature; on a community (or fully expired) license it stays off.
	var auditFwd *audit.Forwarder
	if cfg.ProAccess() != config.ProNone {
		auditFwd = buildAuditForwarder(cfg)
	} else if cfg.AuditWebhookURL != "" || cfg.AuditLogFile != "" || cfg.AuditForwardStdout {
		slog.Warn("Audit forwarding (SIEM) is configured but requires a Pro license — not enabled")
	}
	if auditFwd != nil {
		db.OnAudit = func(p database.AuditLogParams) {
			auditFwd.Emit(audit.Event{
				Action:       p.Action,
				Username:     deref(p.Username),
				ConnectionID: deref(p.ConnectionID),
				Details:      deref(p.Details),
				IPAddress:    deref(p.IPAddress),
			})
		}
	}

	s := &Server{
		cfg:            cfg,
		db:             db,
		gateway:        gw,
		scheduler:      sched,
		pipelineRunner: pipeRunner,
		modelRunner:    modelRunner,
		modelScheduler: modelScheduler,
		govSyncer:      govSyncer,
		chHarvester:    chHarvester,
		githubSyncer:   githubSyncer,
		guardrails:     governance.NewGuardrailService(govStore, db),
		alerts:         alertDispatcher,
		auditFwd:       auditFwd,
		oidcManager:    oidcManager,
		agents:         agents,
		router:         r,
		frontendFS:     frontendFS,
	}

	s.setupRoutes()

	s.http = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute, // Long for streaming SSE/queries
		IdleTimeout:  120 * time.Second,
	}

	return s
}

func (s *Server) setupRoutes() {
	r := s.router
	cfg := s.cfg
	db := s.db
	gw := s.gateway

	// ── Global middleware ────────────────────────────────────────────────
	r.Use(middleware.Recoverer)
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
		DB:         db,
		Gateway:    gw,
		Config:     cfg,
		Guardrails: s.guardrails,
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

	// ── API routes ─────────────────────────────────────────────────────
	r.Route("/api", func(api chi.Router) {
		// Auth routes (no session required, login creates the session)
		authHandler := &handlers.AuthHandler{
			DB:          db,
			Gateway:     gw,
			RateLimiter: rateLimiter,
			Config:      cfg,
			OIDC:        s.oidcManager,
		}
		api.Route("/auth", authHandler.Routes)

		// License status (no session required)
		licenseHandler := &handlers.LicenseHandler{DB: db, Config: cfg}
		api.Get("/license", licenseHandler.GetLicense)

		// Public dashboard endpoints (no session required) — rate-limited per IP
		// since they are unauthenticated and execute panel SQL.
		publicDashboards := &handlers.DashboardsHandler{DB: db, Gateway: gw, Config: cfg}
		api.With(middleware.IPRateLimit(120, time.Minute)).Mount("/public/dashboards", publicDashboards.PublicRoutes())

		// All routes below require a valid session
		api.Group(func(protected chi.Router) {
			protected.Use(middleware.Session(db, gw))

			// License activation (requires session)
			protected.Post("/license/activate", licenseHandler.ActivateLicense)
			protected.Post("/license/deactivate", licenseHandler.DeactivateLicense)

			// Query execution (community)
			queryHandler := &handlers.QueryHandler{DB: db, Gateway: gw, Config: cfg, Guardrails: s.guardrails}
			protected.Route("/query", queryHandler.Routes)

			// Connections management. Reads (list/get/test) are available to any
			// authenticated user, but mutating a connection or revealing/rotating
			// its tunnel token — the credential an agent uses to bridge into the
			// customer's ClickHouse network — requires admin.
			connectionsHandler := &handlers.ConnectionsHandler{DB: db, Gateway: gw, Config: cfg, Agents: s.agents}
			protected.Route("/connections", func(cr chi.Router) {
				cr.Get("/", connectionsHandler.List)
				cr.Get("/{id}", connectionsHandler.Get)
				cr.Post("/{id}/test", connectionsHandler.TestConnection)

				cr.Group(func(ar chi.Router) {
					ar.Use(middleware.RequireAdmin(db))
					ar.Post("/", connectionsHandler.Create)
					ar.Put("/{id}", connectionsHandler.Update)
					ar.Delete("/{id}", connectionsHandler.Delete)
					ar.Get("/{id}/token", connectionsHandler.GetToken)
					ar.Post("/{id}/regenerate-token", connectionsHandler.RegenerateToken)
					ar.Put("/{id}/sso-account", connectionsHandler.SetSSOAccount)
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

			// ── Community features ─────────────────────────────────────
			// Dashboards
			dashboardsHandler := &handlers.DashboardsHandler{DB: db, Gateway: gw, Config: cfg}
			protected.Mount("/dashboards", dashboardsHandler.Routes())

			// Telemetry (OpenTelemetry data exploration)
			telemetryHandler := &handlers.TelemetryHandler{DB: db, Gateway: gw, Config: cfg}
			protected.Mount("/telemetry", telemetryHandler.Routes())

			// Pipelines
			pipelinesHandler := &handlers.PipelinesHandler{DB: db, Gateway: gw, Config: cfg, Runner: s.pipelineRunner}
			protected.Mount("/pipelines", pipelinesHandler.Routes())

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
			}
			protected.Route("/admin", func(ar chi.Router) {
				adminHandler.Routes(ar)
			})

			// MCP key management (admin-only: keys carry CH credentials)
			mcpKeysHandler := &handlers.MCPKeysHandler{DB: db, Config: cfg}
			protected.With(middleware.RequireAdmin(db)).Route("/mcp-keys", mcpKeysHandler.Routes)

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

				// Query Insights (system.query_log analytics)
				queryInsightsHandler := &handlers.QueryInsightsHandler{DB: db, Gateway: gw, Config: cfg}
				pro.Mount("/query-insights", queryInsightsHandler.Routes())

				// Cost Center (showback/chargeback over query_log + parts)
				costsHandler := &handlers.CostsHandler{DB: db, Gateway: gw, Config: cfg}
				pro.Mount("/costs", costsHandler.Routes())
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
			fileServer := http.FileServer(http.FS(s.frontendFS))
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

// Start starts the HTTP server.
func (s *Server) Start() error {
	s.db.StartCleanupJobs()
	s.db.StartRetentionJob()
	s.scheduler.Start()
	s.pipelineRunner.Start()
	s.modelScheduler.Start()
	switch {
	case !s.cfg.IsPro():
		slog.Info("Governance background sync disabled (requires Pro license)")
	case !s.db.GovernanceSyncEnabled():
		slog.Info("Governance background sync disabled (opt-in required; enable in Governance → Settings)")
	default:
		s.govSyncer.StartBackground()
	}
	if s.cfg.IsPro() {
		s.chHarvester.StartBackground()
	} else {
		slog.Info("Cluster health harvester disabled (requires Pro license)")
	}
	s.alerts.Start()

	if s.cfg.IsPro() {
		if n, err := s.db.SweepStaleBrainApprovals(10 * time.Minute); err == nil && n > 0 {
			slog.Info("Swept stale brain approvals", "count", n)
		}
	}

	if s.cfg.TLSEnabled() {
		slog.Info("Server listening with native TLS", "addr", s.http.Addr)
		return s.http.ListenAndServeTLS(s.cfg.TLSCertFile, s.cfg.TLSKeyFile)
	}

	s.warnIfInsecure()
	slog.Info("Server listening", "addr", s.http.Addr)
	return s.http.ListenAndServe()
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
	s.modelScheduler.Stop()
	s.govSyncer.Stop()
	s.chHarvester.Stop()
	s.alerts.Stop()
	s.gateway.Stop()
	s.auditFwd.Close()
	return s.http.Shutdown(ctx)
}
