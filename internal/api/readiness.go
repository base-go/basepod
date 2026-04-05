package api

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/base-go/basepod/internal/app"
)

const (
	appReadyTimeout      = 60 * time.Second
	appReadyPollInterval = 250 * time.Millisecond
)

func waitForLocalPort(ctx context.Context, port int) error {
	if port <= 0 {
		return nil
	}

	address := fmt.Sprintf("127.0.0.1:%d", port)
	ticker := time.NewTicker(appReadyPollInterval)
	defer ticker.Stop()

	var lastErr error
	for {
		dialer := net.Dialer{Timeout: appReadyPollInterval}
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err

		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("timed out waiting for %s: %w", address, lastErr)
			}
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Server) waitForAppReadiness(ctx context.Context, a *app.App) error {
	if a == nil || a.Ports.HostPort <= 0 {
		return nil
	}

	readyCtx, cancel := context.WithTimeout(ctx, appReadyTimeout)
	defer cancel()

	return waitForLocalPort(readyCtx, a.Ports.HostPort)
}
