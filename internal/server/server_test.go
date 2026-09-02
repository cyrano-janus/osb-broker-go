package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testOptions(h http.Handler) Options {
	return Options{
		Addr:              ":0",
		Handler:           h,
		ReadHeaderTimeout: 1 * time.Second,
		ReadTimeout:       2 * time.Second,
		WriteTimeout:      3 * time.Second,
		IdleTimeout:       4 * time.Second,
	}
}

func TestNew_AppliesTimeouts(t *testing.T) {
	// The broker ran with no timeouts at all before Phase 4.5; an unbounded
	// ReadHeaderTimeout is a Slowloris invitation.
	srv := New(testOptions(http.NotFoundHandler()))

	assert.Equal(t, ":0", srv.Addr)
	assert.Equal(t, 1*time.Second, srv.ReadHeaderTimeout)
	assert.Equal(t, 2*time.Second, srv.ReadTimeout)
	assert.Equal(t, 3*time.Second, srv.WriteTimeout)
	assert.Equal(t, 4*time.Second, srv.IdleTimeout)
	assert.NotNil(t, srv.Handler)
	assert.Nil(t, srv.TLSConfig, "plain HTTP unless a TLS config is supplied")
}

func TestServe_ReturnsNilOnContextCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := New(testOptions(http.NotFoundHandler()))
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- Serve(ctx, srv, ln, time.Second) }()

	cancel()
	select {
	case err := <-done:
		// http.ErrServerClosed is the expected outcome of a shutdown, not a
		// failure to report to the operator.
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after context cancel")
	}
}

func TestServe_InFlightRequestCompletesDuringShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()

	started := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		// Long enough that the shutdown below certainly begins first.
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "done")
	})

	srv := New(testOptions(handler))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, srv, ln, 5*time.Second) }()

	respCh := make(chan *http.Response, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/slow")
		if err == nil {
			respCh <- resp
		}
	}()

	<-started
	cancel() // shutdown starts while the request is still being served

	select {
	case resp := <-respCh:
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode, "in-flight request must be drained, not cut off")
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight request was dropped")
	}
	assert.NoError(t, <-done)
}

func TestRun_ReportsBindError(t *testing.T) {
	// Occupy a port, then ask the broker to bind the same one.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	opts := testOptions(http.NotFoundHandler())
	opts.Addr = ln.Addr().String()
	srv := New(opts)

	err = Run(context.Background(), srv, time.Second)
	require.Error(t, err, "a port clash must surface, not be swallowed")
}
