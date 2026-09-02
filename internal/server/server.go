// Package server builds and runs the broker's HTTP(S) listener (Phase 4.5).
//
// Before this package the broker called gin's router.Run, which uses
// http.ListenAndServe: no timeouts, no TLS, and no way to shut down cleanly
// when Kubernetes sends SIGTERM.
package server

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"time"
)

// Options describes the listener to build.
type Options struct {
	Addr    string
	Handler http.Handler
	// TLS nil = plain HTTP. When set, the certificate is served through the
	// config's GetCertificate/GetConfigForClient callbacks rather than from
	// files, so rotation needs no restart.
	TLS               *tls.Config
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

// New builds the http.Server. It does not listen.
func New(o Options) *http.Server {
	return &http.Server{
		Addr:              o.Addr,
		Handler:           o.Handler,
		TLSConfig:         o.TLS,
		ReadHeaderTimeout: o.ReadHeaderTimeout,
		ReadTimeout:       o.ReadTimeout,
		WriteTimeout:      o.WriteTimeout,
		IdleTimeout:       o.IdleTimeout,
	}
}

// Run binds srv.Addr and serves until ctx is cancelled.
func Run(ctx context.Context, srv *http.Server, shutdownTimeout time.Duration) error {
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return err
	}
	return Serve(ctx, srv, ln, shutdownTimeout)
}

// Serve serves on an existing listener until ctx is cancelled, then drains
// in-flight requests within shutdownTimeout.
//
// Split out from Run so tests can bind port 0 and still learn the address.
func Serve(ctx context.Context, srv *http.Server, ln net.Listener, shutdownTimeout time.Duration) error {
	serveErr := make(chan error, 1)
	go func() {
		if srv.TLSConfig != nil {
			// Empty file names: the certificate comes from the TLS config's
			// callbacks.
			serveErr <- srv.ServeTLS(ln, "", "")
			return
		}
		serveErr <- srv.Serve(ln)
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	// Serve returns ErrServerClosed once Shutdown completes; drain it so the
	// goroutine cannot outlive this call.
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
