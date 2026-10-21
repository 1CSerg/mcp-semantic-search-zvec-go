package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"strings"
	"testing"
	"time"
)

func TestResolveRunMode(t *testing.T) {
	for _, tc := range []struct {
		name    string
		in      runMode
		want    runMode
		wantErr error
	}{
		{
			name: "explicit stdio is kept",
			in:   runMode{stdio: true},
			want: runMode{stdio: true},
		},
		{
			name: "explicit http is kept",
			in:   runMode{httpFlag: true},
			want: runMode{httpFlag: true},
		},
		{
			name: "stdio with http keeps both transports",
			in:   runMode{stdio: true, httpFlag: true},
			want: runMode{stdio: true, httpFlag: true},
		},
		{
			name: "daemon implies http and disables stdio",
			in:   runMode{daemonFlag: true, stdio: true},
			want: runMode{daemonFlag: true, httpFlag: true},
		},
		{
			name: "stdio-proxy implies stdio",
			in:   runMode{stdioProxy: true},
			want: runMode{stdio: true, stdioProxy: true},
		},
		{
			name: "stdio-proxy keeps its normalisation priority over daemon",
			in:   runMode{stdioProxy: true, daemonFlag: true},
			want: runMode{stdio: true, stdioProxy: true},
		},
		{
			name:    "empty mode is an error",
			in:      runMode{},
			wantErr: errNoMode,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveRunMode(tc.in)
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

func TestIsHelpRequest(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{args: nil, want: false},
		{args: []string{"-h"}, want: true},
		{args: []string{"--help"}, want: true},
		{args: []string{"-help"}, want: true},
		{args: []string{"--stdio"}, want: false},
		{args: []string{"--stdio", "--help"}, want: true},
		{args: []string{"--http-addr", "127.0.0.1:9090"}, want: false},
	} {
		if got := isHelpRequest(tc.args); got != tc.want {
			t.Errorf("isHelpRequest(%v)=%v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestPrintUsageDescribesModesWithoutGUI(t *testing.T) {
	usageFlags = nil
	defer func() { usageFlags = nil }()

	var buf bytes.Buffer
	printUsage(&buf, "mcp-semantic-search-zvec-go")
	out := buf.String()

	for _, want := range []string{
		"usage: mcp-semantic-search-zvec-go",
		"--stdio",
		"--stdio-proxy",
		"--http",
		"--daemon",
		"--project-add",
		"--project-remove",
		"--projects-reload",
		"--stop-stdio-for-workspace",
		"status 0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("usage text does not mention %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "--gui") {
		t.Errorf("usage text must not advertise the removed --gui flag:\n%s", out)
	}

	// The help is the only discovery surface now that the GUI is gone, so it must
	// describe the combinations the CLI really accepts instead of claiming a
	// single-mode rule that --stdio --http contradicts.
	if strings.Contains(out, "choose exactly one") {
		t.Errorf("usage text claims a single mode, but --stdio --http is supported:\n%s", out)
	}
	for _, want := range []string{
		"--stdio and --http",
		"implies --http",
		"--project-config",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("usage text does not explain %q:\n%s", want, out)
		}
	}
}

// TestPrintUsageRendersRegisteredFlags pins the Flags section: printUsage renders
// the FlagSet the CLI registered, so the help text cannot silently lose the flag
// list (or drift from the parsed flags).
func TestPrintUsageRendersRegisteredFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("probe-flag", "", "probe flag description")
	prev := usageFlags
	usageFlags = fs
	defer func() { usageFlags = prev }()

	var buf bytes.Buffer
	printUsage(&buf, "mcp-semantic-search-zvec-go")
	out := buf.String()

	if !strings.Contains(out, "-probe-flag") || !strings.Contains(out, "probe flag description") {
		t.Fatalf("printUsage did not render the registered flags:\n%s", out)
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

func TestAwaitTransportResultsDrainTimeout(t *testing.T) {
	old := transportDrainTimeout
	transportDrainTimeout = 50 * time.Millisecond
	defer func() { transportDrainTimeout = old }()

	ctx, stop := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	stop()
	if got := awaitTransportResults(ctx, stop, 1, errCh); got != 1 {
		t.Fatalf("awaitTransportResults() = %d, want 1", got)
	}
}

func TestAwaitTransportResultsStopsRemainingTransports(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	errCh := make(chan error, 2)
	peerStopped := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(peerStopped)
		errCh <- nil
	}()
	errCh <- nil

	if got := awaitTransportResults(ctx, stop, 2, errCh); got != 0 {
		t.Fatalf("awaitTransportResults() = %d, want 0", got)
	}
	select {
	case <-peerStopped:
	case <-time.After(time.Second):
		t.Fatal("expected stop() to cancel peer transport")
	}
}
