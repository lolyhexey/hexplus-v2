package panel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/lolyhexey/hexplus/internal/panel/db"
	"github.com/lolyhexey/hexplus/internal/xray"
)

// Server holds the panel's live dependencies. Constructed by Serve and
// passed to handlers so tests can build one with fakes.
type Server struct {
	cfg    Config
	db     *sql.DB
	http   *http.Server
	auth   *Auth
}

// Serve boots the panel: loads Config, opens the DB, wires the router,
// and blocks on http.ListenAndServe until ctx is cancelled. Any error
// during startup is returned; Shutdown errors are logged and swallowed.
func Serve(ctx context.Context) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	sqldb, err := db.Open()
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer sqldb.Close()

	auth := NewAuth(sqldb, cfg.SessionSecret)

	s := &Server{
		cfg:  cfg,
		db:   sqldb,
		auth: auth,
	}
	s.http = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.ListenAddr, cfg.Port),
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Background loops for stats + enforcement. We use a derived ctx
	// so the SIGTERM path cancels them alongside the HTTP shutdown.
	bgCtx, cancelBG := context.WithCancel(ctx)
	defer cancelBG()
	go runStatsLoop(bgCtx, s.db)
	go runEnforcerLoop(bgCtx, s.db)

	errc := make(chan error, 1)
	go func() {
		log.Printf("panel: listening on %s (prefix=%s)", s.http.Addr, cfg.URLPrefix)
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.http.Shutdown(shutCtx); err != nil {
			log.Printf("panel: shutdown: %v", err)
		}
		return nil
	case err := <-errc:
		return err
	}
}

// routes builds the top-level http.Handler. The URLPrefix (if set) is
// stripped before dispatch so downstream handlers can register at their
// natural paths ("/login", "/api/...") without hardcoding the prefix.
//
// The /sub/ subtree lives OUTSIDE the prefix because subscription URLs
// are shared with end-user apps that shouldn't have to know the admin
// prefix — those endpoints authenticate by opaque token instead.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Public: subscription + health. Reachable without login.
	mux.HandleFunc("/sub/", s.handleSubscription)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Prefixed admin surface. Everything under s.cfg.URLPrefix.
	admin := http.NewServeMux()
	admin.HandleFunc("/login", s.handleLogin)
	admin.HandleFunc("/logout", s.handleLogout)
	admin.HandleFunc("/api/session", s.auth.RequireSession(s.handleSession))
	s.registerInboundRoutes(admin)
	s.registerClientRoutes(admin)
	s.registerClientOpRoutes(admin)
	s.registerRoutingRoutes(admin)
	s.registerCertRoutes(admin)
	admin.HandleFunc("/", s.handleRoot) // placeholder — frontend embed replaces this in Phase 10

	if s.cfg.URLPrefix != "" && s.cfg.URLPrefix != "/" {
		mux.Handle(s.cfg.URLPrefix+"/", http.StripPrefix(s.cfg.URLPrefix, admin))
		// bare prefix without trailing slash → redirect to prefix/ so
		// relative URLs in the frontend resolve correctly.
		mux.HandleFunc(s.cfg.URLPrefix, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, s.cfg.URLPrefix+"/", http.StatusMovedPermanently)
		})
	} else {
		mux.Handle("/", admin)
	}
	return mux
}

// handleRoot serves the not-yet-embedded frontend. Phase 10 replaces
// this with an embed.FS-backed handler; for now it just tells the user
// the frontend is coming.
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	// Skip the health/login/api paths that already routed above.
	if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/login" || r.URL.Path == "/logout" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html>
<html><head><title>HEXPLUS Panel</title></head>
<body style="font-family:system-ui;max-width:640px;margin:2rem auto;padding:0 1rem;">
<h1>HEXPLUS V2Ray Panel</h1>
<p>Backend is up. Frontend bundle ships in Phase 10.</p>
<p>Try: <code>POST %s/login</code> with <code>{"username":"...","password":"..."}</code></p>
</body></html>`, s.cfg.URLPrefix)
}

// handleSession returns the caller's admin identity (proves the session
// cookie is valid). Placeholder — real impl reads from context in Phase 5.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// handleSubscription lives in subscription.go.

// runStatsLoop polls xray-core's counters every xray.PollInterval and
// persists them to the panel DB. Errors are logged and swallowed; a
// single failed poll doesn't kill the loop.
func runStatsLoop(ctx context.Context, sqldb *sql.DB) {
	t := time.NewTicker(xray.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := xray.PollAndPersist(ctx, sqldb); err != nil {
				log.Printf("panel: stats poll: %v", err)
			}
		}
	}
}
