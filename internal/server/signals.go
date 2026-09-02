package server

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// SignalContext returns a context cancelled on SIGTERM or interrupt.
//
// SIGTERM is what Kubernetes sends before the grace period; without handling
// it the broker was killed mid-request on every rollout.
func SignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
