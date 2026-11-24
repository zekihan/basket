// Package main runs a loopback-only OpenID provider for the Playwright tests.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/oauth2-proxy/mockoidc"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	err := run(ctx)

	stop()

	if err != nil {
		slog.Error("Mock identity provider failed", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	provider, err := mockoidc.NewServer(nil)
	if err != nil {
		return fmt.Errorf("create provider: %w", err)
	}

	var listenConfig net.ListenConfig

	listener, err := listenConfig.Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen on loopback: %w", err)
	}
	defer listener.Close()

	err = provider.Start(listener, nil)
	if err != nil {
		return fmt.Errorf("start provider: %w", err)
	}
	defer func() {
		shutdownErr := provider.Shutdown()
		if shutdownErr != nil {
			slog.ErrorContext(
				ctx,
				"Mock identity provider shutdown failed",
				slog.Any("error", shutdownErr),
			)
		}
	}()

	config := struct {
		Issuer       string `json:"issuer"`
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
		Subject      string `json:"subject"`
	}{
		Issuer:       provider.Issuer(),
		ClientID:     provider.ClientID,
		ClientSecret: provider.ClientSecret,
		Subject:      mockoidc.DefaultUser().Subject,
	}

	err = json.NewEncoder(os.Stdout).Encode(config)
	if err != nil {
		return fmt.Errorf("print provider configuration: %w", err)
	}

	<-ctx.Done()

	return nil
}
