package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zekihan/go-commons/backup"
	"github.com/zekihan/go-commons/metrics"
	"github.com/zekihan/go-commons/middleware"
	"github.com/zekihan/go-commons/oidcauth"
	"github.com/zekihan/go-commons/utils"

	// IANA zones must resolve inside the distroless image.
	_ "time/tzdata"

	gclog "github.com/zekihan/go-commons/log"

	"github.com/zekihan/basket/internal/basket"
	"github.com/zekihan/basket/internal/config"
	"github.com/zekihan/basket/internal/mcp"
	"github.com/zekihan/basket/internal/server"
	"github.com/zekihan/basket/internal/store"
	"github.com/zekihan/basket/web"
)

const (
	readHeaderTimeout = 10 * time.Second
	idleTimeout       = 120 * time.Second
	shutdownTimeout   = 10 * time.Second
)

func main() {
	err := run()
	if err != nil {
		slog.Error("Server failed", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(ctx)
	if err != nil {
		return fmt.Errorf("start: %w", err)
	}

	loggerConfig := new(gclog.LoggerConfig)
	loggerConfig.Level = cfg.LogLevel
	gclog.InitLogger(loggerConfig)
	utils.LogVersionInfo(ctx)

	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()

	authSvc, err := oidcauth.New(ctx, &oidcauth.Config[oidcauth.IdentityInfo]{
		Authorize:         oidcauth.IdentityInfoAuthorize,
		HTTPClient:        nil,
		AppName:           "Basket",
		SessionCookie:     "basket_session",
		TransactionCookie: "basket_oidc_tx",
		SuccessPath:       "/",
		PublicURL:         cfg.PublicURL,
		Issuer:            cfg.OIDCIssuer,
		ClientID:          cfg.OIDCClientID,
		ClientSecret:      cfg.OIDCClientSecret,
		OwnerSubject:      cfg.OwnerSubject,
		MCPClientID:       cfg.MCPClientID,
		MCPAudience:       cfg.MCPAudience,
		CookieInsecure:    cfg.CookieInsecure,
	}, st.DB())
	if err != nil {
		return fmt.Errorf("init sign-in: %w", err)
	}

	webFS, err := web.DistFS()
	if err != nil {
		return fmt.Errorf("start: %w", err)
	}

	svc := basket.NewService(st, cfg.TimeZone, time.Now)

	err = svc.Prune(ctx)
	if err != nil {
		return fmt.Errorf("prune history: %w", err)
	}

	go pruneLoop(ctx, svc)

	srv := server.New(server.Options{
		Auth: authSvc, Service: svc, MCP: mcp.NewHandler(svc), Web: webFS,
		Ping: func(r *http.Request) error { return st.DB().PingContext(r.Context()) },
	})
	registry := srv.MetricsRegistry()

	if cfg.BackupDir != "" {
		go func() {
			backupErr := backup.Run(
				ctx, st.DB(), cfg.BackupDir, cfg.BackupInterval, cfg.BackupKeep,
				backup.WithFilePrefix("basket-"),
			)
			if backupErr != nil && !errors.Is(backupErr, context.Canceled) {
				slog.ErrorContext(ctx, "Database backups stopped", slog.Any("error", backupErr))
			}
		}()
	}

	httpServer := new(http.Server)
	httpServer.Addr = cfg.ListenAddr
	httpServer.Handler = registry.Middleware(
		middleware.RecoverHandler(middleware.Logging(srv.Handler())),
	)
	httpServer.ReadHeaderTimeout = readHeaderTimeout
	httpServer.IdleTimeout = idleTimeout

	slog.InfoContext(ctx, "Listening", slog.String("addr", cfg.ListenAddr))

	if cfg.MetricsAddr != "" {
		slog.InfoContext(ctx, "Serving metrics", slog.String("addr", cfg.MetricsAddr))

		go serveMetrics(ctx, cfg.MetricsAddr, registry)
	}

	return serve(ctx, httpServer)
}

// serveMetrics runs the internal /metrics listener until ctx is canceled.
// An empty addr disables the listener. It is never served on the public
// ingress.
func serveMetrics(ctx context.Context, addr string, registry *metrics.Registry) {
	if addr == "" {
		return
	}

	err := registry.ListenAndServe(ctx, addr)
	if err != nil {
		slog.ErrorContext(ctx, "Metrics server failed", slog.Any("error", err))
	}
}

func serve(ctx context.Context, httpServer *http.Server) error {
	errCh := make(chan error, 1)

	go func() {
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return fmt.Errorf("listen: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	err := httpServer.Shutdown(shutdownCtx)
	if err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	return nil
}

func pruneLoop(ctx context.Context, svc *basket.Service) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := svc.Prune(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "History pruning failed", slog.Any("error", err))
			}
		}
	}
}
