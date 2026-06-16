package main

import (
	"context"
	"errors"
	"testing"
)

func TestIsLoopbackAddr(t *testing.T) {
	for _, tc := range []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8080", true},
		{"[::1]:8080", true},
		{"localhost:8080", true},
		{":8080", false},
		{"0.0.0.0:8080", false},
	} {
		if got := isLoopbackAddr(tc.addr); got != tc.want {
			t.Errorf("isLoopbackAddr(%q)=%v want %v", tc.addr, got, tc.want)
		}
	}
}

func TestAwaitTransportResultsCleanExit(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	errCh := make(chan error, 1)
	errCh <- nil

	if got := awaitTransportResults(ctx, stop, 1, errCh); got != 0 {
		t.Fatalf("awaitTransportResults() = %d, want 0", got)
	}
	if ctx.Err() == nil {
		t.Fatal("expected context cancelled after clean transport exit")
	}
}

func TestAwaitTransportResultsTransportError(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	errCh := make(chan error, 1)
	errCh <- errors.New("mcp: connection closed")

	if got := awaitTransportResults(ctx, stop, 1, errCh); got != 1 {
		t.Fatalf("awaitTransportResults() = %d, want 1", got)
	}
}

func TestAwaitTransportResultsSignalShutdown(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		<-ctx.Done()
		errCh <- nil
	}()

	stop()

	if got := awaitTransportResults(ctx, stop, 1, errCh); got != 0 {
		t.Fatalf("awaitTransportResults() = %d, want 0", got)
	}
}

func TestAwaitTransportResultsMultipleTransports(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	errCh := make(chan error, 2)
	errCh <- nil
	errCh <- nil

	if got := awaitTransportResults(ctx, stop, 2, errCh); got != 0 {
		t.Fatalf("awaitTransportResults() = %d, want 0", got)
	}
}
