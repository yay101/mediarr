package server

import (
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/yay101/mediarr/ai"
	"github.com/yay101/mediarr/auth"
	"github.com/yay101/mediarr/config"
	"github.com/yay101/mediarr/db"
	"github.com/yay101/mediarr/download"
	"github.com/yay101/mediarr/search"
	"github.com/yay101/mediarr/tasks"
)

type Server struct {
	app        *App
	server     *http.Server
	mux        *http.ServeMux
	oidcClient *oidc.Client
	templates  *template.Template
	wsHub      *WSHub
}

type App struct {
	Config     func() *config.Config
	DB         func() *db.Database
	Tasks      func() *tasks.Manager
	Automation func() interface{}
	Subtitles  func() interface{}
	Search     func() *search.Manager
	SearchHub  func() *search.Hub
	Download   func() *download.Manager
	Storage    func() interface{}
	AI         func() *ai.Service
	Stopped    func() <-chan struct{}
}

func New(app *App) *Server {
	// Initialize debug logging
	initDebugLog()

	mux := http.NewServeMux()
	wsHub := NewWSHub()

	go wsHub.Run()

	s := &Server{
		app:   app,
		mux:   mux,
		wsHub: wsHub,
	}

	// Initialize templates
	tmpl, err := template.ParseGlob("web/templates/*.html")
	if err != nil {
		slog.Warn("failed to parse templates", "error", err)
	}
	s.templates = tmpl

	cfg := app.Config()
	if cfg.Auth.OIDC.Enabled {
		oidcCfg := &oidc.Config{
			RedirectURL: cfg.Auth.OIDC.RedirectURL,
		}
		for _, p := range cfg.Auth.OIDC.Providers {
			oidcCfg.Providers = append(oidcCfg.Providers, oidc.ProviderConfig{
				ID:           p.ID,
				Name:         p.Name,
				Issuer:       p.Issuer,
				ClientID:     p.ClientID,
				ClientSecret: p.ClientSecret,
			})
		}
		client, err := oidc.NewClient(oidcCfg, slog.Default())
		if err == nil {
			s.oidcClient = client
			s.setupOIDC()
		} else {
			slog.Error("failed to initialize OIDC client", "error", err)
		}
	}

	s.setupRoutes()
	return s
}

func (s *Server) setupOIDC() {
	s.oidcClient.SetCallback(s.handleOIDCCallback)
}

func (s *Server) setupRoutes() {
	s.mux.HandleFunc("GET /health", handleHealth)

	s.mux.HandleFunc("GET /api/v1/auth/me", s.handleAuthMe)
	s.mux.HandleFunc("GET /login", s.handleLoginPage)
	s.mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)

	if s.oidcClient != nil {
		s.mux.Handle("GET /api/v1/auth/provider", s.oidcClient.LibClient.ProviderHandler)
		s.mux.Handle("GET /api/v1/auth/provider/{id}", s.oidcClient.LibClient.ProviderHandler)
		s.mux.Handle("GET /api/v1/auth/callback", s.oidcClient.LibClient.RedirectHandler)
		s.mux.Handle("POST /api/v1/auth/callback", s.oidcClient.LibClient.RedirectHandler)
	}

	s.mux.HandleFunc("GET /api/v1/media", s.authMiddleware(s.handleListMedia))
	s.mux.HandleFunc("POST /api/v1/media", s.authMiddleware(s.handleAddMedia))
	s.mux.HandleFunc("GET /api/v1/media/{type}/{id}", s.authMiddleware(s.handleGetMedia))
	s.mux.HandleFunc("DELETE /api/v1/media/{type}/{id}", s.authMiddleware(s.handleDeleteMedia))
	s.mux.HandleFunc("GET /api/v1/media/{type}/{id}/subtitles", s.authMiddleware(s.handleListSubtitles))
	s.mux.HandleFunc("POST /api/v1/media/{type}/{id}/subtitles", s.authMiddleware(s.handleDownloadSubtitles))
	s.mux.HandleFunc("GET /api/v1/calendar", s.authMiddleware(s.handleGetCalendar))

	s.mux.HandleFunc("GET /api/v1/search/metadata", s.authMiddleware(s.handleSearch))
	s.mux.HandleFunc("POST /api/v1/media/{type}/{id}/search", s.authMiddleware(s.handleTriggerSearch))

	s.mux.HandleFunc("POST /api/v1/search", s.authMiddleware(s.handleManualSearch))
	s.mux.HandleFunc("GET /api/v1/search/{session_id}", s.authMiddleware(s.handleGetSearchResults))
	s.mux.HandleFunc("POST /api/v1/search/{session_id}/download", s.authMiddleware(s.handleDownloadSearchResult))
	s.mux.HandleFunc("DELETE /api/v1/search/{session_id}", s.authMiddleware(s.handleClearSearchSession))

	s.mux.HandleFunc("GET /ws/search", s.authMiddleware(s.handleSearchWebSocket))
	s.mux.HandleFunc("GET /ws", s.authMiddleware(s.handleWS))

	s.mux.HandleFunc("GET /api/v1/rss", s.adminMiddleware(s.handleListRSSFeeds))
	s.mux.HandleFunc("POST /api/v1/rss", s.adminMiddleware(s.handleAddRSSFeed))
	s.mux.HandleFunc("DELETE /api/v1/rss/{id}", s.adminMiddleware(s.handleRemoveRSSFeed))

	s.mux.HandleFunc("GET /api/v1/watchlist", s.authMiddleware(s.handleListWatchlist))
	s.mux.HandleFunc("POST /api/v1/watchlist", s.authMiddleware(s.handleAddWatchlist))
	s.mux.HandleFunc("DELETE /api/v1/watchlist/{id}", s.authMiddleware(s.handleRemoveWatchlist))

	s.mux.HandleFunc("GET /api/v1/downloads", s.authMiddleware(s.handleListDownloads))
	s.mux.HandleFunc("POST /api/v1/downloads", s.authMiddleware(s.handleAddDownload))
	s.mux.HandleFunc("DELETE /api/v1/downloads/{id}", s.authMiddleware(s.handleCancelDownload))
	s.mux.HandleFunc("PATCH /api/v1/downloads/{id}/pause", s.authMiddleware(s.handlePauseDownload))
	s.mux.HandleFunc("PATCH /api/v1/downloads/{id}/resume", s.authMiddleware(s.handleResumeDownload))

	s.mux.HandleFunc("GET /api/v1/files/stream/{id}", s.authMiddleware(s.handleStreamFile))
	s.mux.HandleFunc("GET /api/v1/files/", s.adminMiddleware(s.handleServeFile))

	s.mux.HandleFunc("GET /api/v1/settings", s.adminMiddleware(s.handleGetSettings))
	s.mux.HandleFunc("PATCH /api/v1/settings", s.adminMiddleware(s.handlePatchSettings))
	s.mux.HandleFunc("GET /api/v1/settings/{key}/versions", s.adminMiddleware(s.handleGetSettingVersions))
	s.mux.HandleFunc("GET /api/v1/settings/{key}/versions/{version}", s.adminMiddleware(s.handleGetSettingVersion))
	s.mux.HandleFunc("POST /api/v1/settings/{key}/rollback/{version}", s.adminMiddleware(s.handleRollbackSetting))

	s.mux.HandleFunc("GET /api/v1/tasks", s.adminMiddleware(s.handleListTasks))
	s.mux.HandleFunc("DELETE /api/v1/tasks/{id}", s.adminMiddleware(s.handleKillTask))
	s.mux.HandleFunc("POST /api/v1/config/reload", s.adminMiddleware(s.handleReloadConfig))
	s.mux.HandleFunc("POST /api/v1/media/verify", s.adminMiddleware(s.handleVerifyMedia))

	// AI endpoints
	s.mux.HandleFunc("POST /api/v1/ai/search/refine", s.adminMiddleware(s.handleAIRefineSearch))
	s.mux.HandleFunc("POST /api/v1/ai/file/check", s.adminMiddleware(s.handleAIFileCheck))
	s.mux.HandleFunc("POST /api/v1/ai/metadata/enrich", s.adminMiddleware(s.handleAIMetadataEnrich))
	s.mux.HandleFunc("POST /api/v1/ai/search/natural", s.adminMiddleware(s.handleAINaturalSearch))
	s.mux.HandleFunc("POST /api/v1/ai/album/verify", s.adminMiddleware(s.handleAIAlbumVerify))
	s.mux.HandleFunc("POST /api/v1/ai/didyoumean", s.adminMiddleware(s.handleAIDidYouMean))

	// Frontend routes (SPA)
	frontendRoutes := []string{
		"/",
		"/movies",
		"/tv",
		"/downloads",
		"/search",
		"/rss",
		"/settings",
	}
	for _, route := range frontendRoutes {
		s.mux.HandleFunc("GET "+route, s.handleIndex)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if s.templates == nil {
		http.Error(w, "Templates not loaded", http.StatusInternalServerError)
		return
	}
	err := s.templates.ExecuteTemplate(w, "layout", nil)
	if err != nil {
		slog.Error("failed to render template", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) Start() error {
	cfg := s.app.Config()

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      loggingMiddleware(s.mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		<-s.app.Stopped()
		s.shutdown()
	}()

	if cfg.TLS.Enabled && cfg.TLS.Domain != "" {
		m := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(cfg.TLS.Domain),
			Cache:      autocert.DirCache(cfg.TLS.CertCache),
		}

		// Use ports from config or defaults
		tlsAddr := ":443"
		httpAddr := ":80"

		s.server.Addr = tlsAddr
		s.server.TLSConfig = m.TLSConfig()

		// Start HTTP to HTTPS redirect and challenge handler
		go func() {
			slog.Info("starting HTTP challenge/redirect server", "addr", httpAddr)
			if err := http.ListenAndServe(httpAddr, m.HTTPHandler(nil)); err != nil {
				slog.Error("redirect server error", "error", err)
			}
		}()

		slog.Info("starting server with TLS", "domain", cfg.TLS.Domain, "addr", s.server.Addr)
		return s.server.ListenAndServeTLS("", "")
	}

	slog.Info("starting server", "addr", s.server.Addr)
	return s.server.ListenAndServe()
}

func (s *Server) shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	slog.Info("shutting down server")
	if err := s.server.Shutdown(ctx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Debug("request", "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
