// Package server serves the Basket API and embedded application.
package server

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/zekihan/go-commons/metrics"

	"github.com/zekihan/basket/internal/auth"
	"github.com/zekihan/basket/internal/basket"
)

type Auth interface {
	LoginHandler() http.Handler
	CallbackHandler() http.Handler
	LogoutHandler() http.Handler
	RequireSession(next http.Handler) http.Handler
	MCPEnabled() bool
	RequireBearer(next http.Handler) http.Handler
	ProtectedResourceHandler() http.Handler
}

type Options struct {
	Auth    Auth
	Service *basket.Service
	MCP     http.Handler
	Web     fs.FS
	Ping    func(*http.Request) error
}
type Server struct {
	deps     Options
	registry *metrics.Registry
}

func New(
	options Options,
) *Server {
	return &Server{deps: options, registry: metrics.New()}
}
func (srv *Server) MetricsRegistry() *metrics.Registry { return srv.registry }

const contentSecurityPolicy = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; " +
	"frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

func (srv *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /auth/login", srv.deps.Auth.LoginHandler())
	mux.Handle("GET /auth/callback", srv.deps.Auth.CallbackHandler())
	mux.Handle("POST /auth/logout", srv.deps.Auth.LogoutHandler())
	mux.Handle(
		"GET /auth/me",
		srv.deps.Auth.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			me, ok := auth.Identity(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized, "unauthenticated", "Sign in is required.")

				return
			}

			writeJSON(w, http.StatusOK, me)
		})),
	)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if srv.deps.Ping != nil && srv.deps.Ping(r) != nil {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)

			return
		}

		_, err := w.Write([]byte("ok"))
		if err != nil {
			return
		}
	})
	mux.HandleFunc("GET /", srv.serveWeb)

	mcp := http.NotFoundHandler()
	if srv.deps.Auth.MCPEnabled() && srv.deps.MCP != nil {
		mcp = srv.deps.Auth.RequireBearer(srv.deps.MCP)
		mux.Handle(
			"GET /.well-known/oauth-protected-resource",
			srv.deps.Auth.ProtectedResourceHandler(),
		)
	}

	api := srv.deps.Auth.RequireSession(http.HandlerFunc(srv.api))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		w.Header().Set("Referrer-Policy", "same-origin")

		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			switch r.URL.Path {
			case "/api/snapshot", "/api/ops", "/api/similar", "/api/export":
				r.Pattern = r.URL.Path
			default:
				r.Pattern = "/api/*"
			}

			api.ServeHTTP(w, r)

			return
		}

		if r.URL.Path == "/mcp" {
			r.Pattern = "/mcp"
			mcp.ServeHTTP(w, r)

			return
		}

		mux.ServeHTTP(w, r)
	})
}
