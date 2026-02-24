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
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/governance"
	"github.com/caioricciuti/ch-ui/internal/scheduler"
	"github.com/caioricciuti/ch-ui/internal/server/handlers"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"github.com/go-chi/chi/v5"
)

// Server is the main HTTP server.
type Server struct {
	cfg        *config.Config
	db         *database.DB
	gateway    *tunnel.Gateway
	scheduler  *scheduler.Runner
	govSyncer  *governance.Syncer
	guardrails *governance.GuardrailService
	alerts     *alerts.Dispatcher
	router     chi.Router
	http       *http.Server
	frontendFS fs.FS
}

// New creates a new Server with all routes configured.
func New(cfg *config.Config, db *database.DB, frontendFS fs.FS) *Server {
	r := chi.NewRouter()
	gw := tunnel.NewGateway(db)

	sched := scheduler.NewRunner(db, gw, cfg.AppSecretKey)

	govStore := governance.NewStore(db)
	govSyncer := governance.NewSyncer(govStore, db, gw, cfg.AppSecretKey)
	alertDispatcher := alerts.NewDispatcher(db, cfg)

	s := &Server{
		cfg:        cfg,
		db:         db,
		gateway:    gw,
		scheduler:  sched,
		govSyncer:  govSyncer,
		guardrails: governance.NewGuardrailService(govStore, db),
		alerts:     alertDispatcher,
		router:     r,
		frontendFS: frontendFS,
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

			// Saved queries (community)
			savedQueriesHandler := &handlers.SavedQueriesHandler{DB: db}
			protected.Route("/saved-queries", savedQueriesHandler.Routes)

			// ── Community features ─────────────────────────────────────
			// Dashboards
			dashboardsHandler := &handlers.DashboardsHandler{DB: db, Gateway: gw, Config: cfg}
			protected.Mount("/dashboards", dashboardsHandler.Routes())

			// Brain AI assistant
			brainHandler := &handlers.BrainHandler{DB: db, Gateway: gw, Config: cfg}
			protected.Route("/brain", brainHandler.Routes)

			// Admin routes (require admin role)
			adminHandler := &handlers.AdminHandler{DB: db, Gateway: gw, Config: cfg}
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
	s.govSyncer.StartBackground()
	s.alerts.Start()

	slog.Info("Server listening", "addr", s.http.Addr)
	return s.http.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("Graceful shutdown initiated")
	s.scheduler.Stop()
	s.govSyncer.Stop()
	s.alerts.Stop()
	s.gateway.Stop()
	return s.http.Shutdown(ctx)
}
