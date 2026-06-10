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
	"github.com/caioricciuti/ch-ui/internal/clusterhealth"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	ghclient "github.com/caioricciuti/ch-ui/internal/github"
	"github.com/caioricciuti/ch-ui/internal/governance"
	"github.com/caioricciuti/ch-ui/internal/models"
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
	router         chi.Router
	http           *http.Server
	frontendFS     fs.FS
}

// New creates a new Server with all routes configured.
func New(cfg *config.Config, db *database.DB, frontendFS fs.FS) *Server {
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
	r.Use(middleware.Logger)
	r.Use(middleware.SecurityHeaders(!cfg.DevMode))
	r.Use(middleware.CORS(middleware.CORSConfig{
		DevMode:        cfg.DevMode,
		AllowedOrigins: cfg.AllowedOrigins,
		AppURL:         cfg.AppURL,
	}))

	// ── Health check (no auth) ──────────────────────────────────────────
	healthHandler := &handlers.HealthHandler{}
	r.Get("/health", healthHandler.Health)

	// ── WebSocket tunnel endpoint (agent authenticates via token) ────────
	r.HandleFunc("/connect", gw.HandleWebSocket)

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
		}
		api.Route("/auth", authHandler.Routes)

		// License status (no session required)
		licenseHandler := &handlers.LicenseHandler{DB: db, Config: cfg}
		api.Get("/license", licenseHandler.GetLicense)

		// Public dashboard endpoints (no session required)
		publicDashboards := &handlers.DashboardsHandler{DB: db, Gateway: gw, Config: cfg}
		api.Mount("/public/dashboards", publicDashboards.PublicRoutes())

		// All routes below require a valid session
		api.Group(func(protected chi.Router) {
			protected.Use(middleware.Session(db, gw))

			// License activation (requires session)
			protected.Post("/license/activate", licenseHandler.ActivateLicense)
			protected.Post("/license/deactivate", licenseHandler.DeactivateLicense)

			// Query execution (community)
			queryHandler := &handlers.QueryHandler{DB: db, Gateway: gw, Config: cfg, Guardrails: s.guardrails}
			protected.Route("/query", queryHandler.Routes)

			// Connections management (community)
			connectionsHandler := &handlers.ConnectionsHandler{DB: db, Gateway: gw, Config: cfg}
			protected.Route("/connections", func(cr chi.Router) {
				cr.Get("/", connectionsHandler.List)
				cr.Post("/", connectionsHandler.Create)
				cr.Get("/{id}", connectionsHandler.Get)
				cr.Delete("/{id}", connectionsHandler.Delete)
				cr.Post("/{id}/test", connectionsHandler.TestConnection)
				cr.Get("/{id}/token", connectionsHandler.GetToken)
				cr.Post("/{id}/regenerate-token", connectionsHandler.RegenerateToken)
			})

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
			}
			protected.Route("/admin", func(ar chi.Router) {
				adminHandler.Routes(ar)
			})

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

	slog.Info("Server listening", "addr", s.http.Addr)
	return s.http.ListenAndServe()
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
	return s.http.Shutdown(ctx)
}
