package main

import (
	"context"
	"errors"
	"testing"
)

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
