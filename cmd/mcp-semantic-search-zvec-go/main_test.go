package main

import (
	"context"
	"errors"
	"testing"
)

func TestResolveRunMode(t *testing.T) {
	for _, tc := range []struct {
		name    string
		in      runMode
		goos    string
		nflag   int
		want    runMode
		wantErr error
	}{
		{
			name: "windows no flags defaults to gui",
			goos: "windows", nflag: 0,
			want: runMode{guiFlag: true},
		},
		{
			name: "linux no flags defaults to stdio",
			goos: "linux", nflag: 0,
			want: runMode{stdio: true},
		},
		{
			name: "explicit gui on windows",
			in:   runMode{guiFlag: true}, goos: "windows", nflag: 1,
			want: runMode{guiFlag: true},
		},
		{
			name: "explicit stdio on windows overrides default",
			in:   runMode{stdio: true}, goos: "windows", nflag: 1,
			want: runMode{stdio: true},
		},
		{
			name: "gui conflicts with stdio",
			in:   runMode{guiFlag: true, stdio: true}, goos: "windows", nflag: 2,
			wantErr: errModeConflict,
		},
		{
			name: "daemon implies http and disables stdio",
			in:   runMode{daemonFlag: true, stdio: true}, goos: "linux", nflag: 1,
			want: runMode{daemonFlag: true, httpFlag: true},
		},
		{
			name: "stdio-proxy implies stdio",
			in:   runMode{stdioProxy: true}, goos: "linux", nflag: 1,
			want: runMode{stdio: true, stdioProxy: true},
		},
		{
			name: "config only without mode errors",
			in:   runMode{}, goos: "linux", nflag: 1,
			wantErr: errNoMode,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveRunMode(tc.in, tc.goos, tc.nflag)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err=%v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("resolveRunMode()=%+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestDefaultNoArgsModes(t *testing.T) {
	for _, tc := range []struct {
		goos      string
		wantStdio bool
		wantGUI   bool
	}{
		{goos: "windows", wantStdio: false, wantGUI: true},
		{goos: "linux", wantStdio: true, wantGUI: false},
		{goos: "darwin", wantStdio: true, wantGUI: false},
	} {
		gotStdio, gotGUI := defaultNoArgsModes(tc.goos)
		if gotStdio != tc.wantStdio || gotGUI != tc.wantGUI {
			t.Fatalf("defaultNoArgsModes(%q)=(%v,%v), want (%v,%v)", tc.goos, gotStdio, gotGUI, tc.wantStdio, tc.wantGUI)
		}
	}
}

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
