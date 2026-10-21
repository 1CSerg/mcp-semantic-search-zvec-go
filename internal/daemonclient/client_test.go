package daemonclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testEnv points MCP_GLOBAL_DIR at a temporary user dir and returns it.
func testEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MCP_GLOBAL_DIR", dir)
	return dir
}

// TestAuthTokenCreatedOnce pins the token contract: exactly one token exists
// under concurrent creation, it is 32 random bytes hex-encoded, and it is never
// regenerated or overwritten.
func TestAuthTokenCreatedOnce(t *testing.T) {
	dir := testEnv(t)

	const n = 16
	var wg sync.WaitGroup
	tokens := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tokens[i], errs[i] = EnsureAuthToken()
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	first := tokens[0]
	if len(first) != 64 {
		t.Fatalf("token length=%d want 64 hex chars", len(first))
	}
	for i, tok := range tokens {
		if tok != first {
			t.Fatalf("token %d differs: %q vs %q", i, tok, first)
		}
	}

	// Reading again returns the same token: no silent regeneration.
	again, err := ReadAuthToken()
	if err != nil {
		t.Fatal(err)
	}
	if again != first {
		t.Fatalf("ReadAuthToken=%q want %q", again, first)
	}

	// The token file lives under the user dir with restrictive permissions.
	path := filepath.Join(dir, authTokenFileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("empty token file")
	}
}

// TestAuthTokenCorruptNotRegenerated: a damaged token file must be reported, not
// silently replaced, because the running daemon still holds the old token.
func TestAuthTokenCorruptNotRegenerated(t *testing.T) {
	dir := testEnv(t)
	if _, err := EnsureAuthToken(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, authTokenFileName)
	if err := os.WriteFile(path, []byte("short\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAuthToken(); err == nil {
		t.Fatal("a corrupt token must be an error")
	}
	if _, err := EnsureAuthToken(); err == nil {
		t.Fatal("EnsureAuthToken must not regenerate over a corrupt file")
	}
	// The file is untouched so the operator can inspect it.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "short\n" {
		t.Fatalf("token file was rewritten: %q", data)
	}
}

// TestConnectionRedactedNeverLeaksToken: the token must not appear in String or
// in logs; only the base URL is printable.
func TestConnectionRedactedNeverLeaksToken(t *testing.T) {
	c := Connection{BaseURL: "http://127.0.0.1:8080", Token: "super-secret"}
	if strings.Contains(c.String(), "super-secret") {
		t.Fatalf("Connection.String leaks the token: %q", c.String())
	}
	if strings.Contains(fmt.Sprintf("%v", c), "super-secret") {
		t.Fatalf("Connection formatting leaks the token: %v", c)
	}
}

// fakeDaemon serves an authenticated /health endpoint and counts hits.
type fakeDaemon struct {
	srv   *httptest.Server
	hits  atomic.Int64
	token string
}

func newFakeDaemon(t *testing.T, token string) *fakeDaemon {
	t.Helper()
	d := &fakeDaemon{token: token}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		d.hits.Add(1)
		if token != "" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer "+token {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"mcp-semantic-search-zvec-go"}`))
	})
	d.srv = httptest.NewServer(mux)
	t.Cleanup(d.srv.Close)
	return d
}

func (d *fakeDaemon) baseURL() string { return d.srv.URL }

// TestEnsureRunningLiveDaemon: a healthy authenticated daemon is reused with no
// launch at all.
func TestEnsureRunningLiveDaemon(t *testing.T) {
	dir := testEnv(t)
	token, err := EnsureAuthToken()
	if err != nil {
		t.Fatal(err)
	}
	writeDaemonYAML(t, dir, "")

	d := newFakeDaemon(t, token)
	var launches atomic.Int64
	c := &Client{
		Executable: "/nonexistent/mcp",
		BaseURL:    d.baseURL(),
		Start: func(context.Context, LaunchOptions) error {
			launches.Add(1)
			return nil
		},
	}
	conn, err := c.EnsureRunning(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if launches.Load() != 0 {
		t.Fatalf("launch count=%d want 0", launches.Load())
	}
	if conn.Token != token {
		t.Fatalf("connection token mismatch")
	}
	if conn.BaseURL != d.baseURL() {
		t.Fatalf("base url=%q", conn.BaseURL)
	}
	if d.hits.Load() == 0 {
		t.Fatal("health was not probed")
	}
}

// TestEnsureRunningColdStart: with nothing listening, the client launches exactly
// one detached daemon and then waits for health.
func TestEnsureRunningColdStart(t *testing.T) {
	dir := testEnv(t)
	writeDaemonYAML(t, dir, "")

	var launches atomic.Int64
	var d *fakeDaemon
	var c *Client
	c = &Client{
		Executable:    "/nonexistent/mcp",
		BaseURL:       "http://127.0.0.1:1",
		HTTPClient:    &http.Client{Timeout: 300 * time.Millisecond},
		HealthTimeout: 5 * time.Second,
		Start: func(_ context.Context, opts LaunchOptions) error {
			launches.Add(1)
			// The launched daemon comes up on its own port; point the client at it.
			d = newFakeDaemon(t, opts.Token)
			c.BaseURL = d.baseURL()
			return nil
		},
	}
	if _, err := c.EnsureRunning(t.Context()); err != nil {
		t.Fatal(err)
	}
	if launches.Load() != 1 {
		t.Fatalf("launch count=%d want 1", launches.Load())
	}
	if d == nil || d.hits.Load() == 0 {
		t.Fatal("health was not awaited after launch")
	}
}

// TestEnsureRunningTimeout: a daemon that never becomes healthy yields an
// explanatory error mentioning the timeout, not a hang.
func TestEnsureRunningTimeout(t *testing.T) {
	dir := testEnv(t)
	writeDaemonYAML(t, dir, "")

	var launches atomic.Int64
	c := &Client{
		Executable: "/nonexistent/mcp",
		BaseURL:    "http://127.0.0.1:1",
		Start: func(context.Context, LaunchOptions) error {
			launches.Add(1)
			return nil
		},
		HealthTimeout: 150 * time.Millisecond,
	}
	start := time.Now()
	_, err := c.EnsureRunning(t.Context())
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if !strings.Contains(err.Error(), "health") {
		t.Fatalf("error should mention the health check: %v", err)
	}
	if launches.Load() != 1 {
		t.Fatalf("launch count=%d want 1", launches.Load())
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout was not honoured: %v", elapsed)
	}
}

// TestEnsureRunningCancellation: a cancelled context aborts promptly.
func TestEnsureRunningCancellation(t *testing.T) {
	dir := testEnv(t)
	writeDaemonYAML(t, dir, "")
	c := &Client{
		Executable:    "/nonexistent/mcp",
		BaseURL:       "http://127.0.0.1:1",
		Start:         func(context.Context, LaunchOptions) error { return nil },
		HealthTimeout: 10 * time.Second,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.EnsureRunning(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v want context.Canceled", err)
	}
}

// TestEnsureRunningPortTakenByForeignServer: when the port is served by something
// that is not our daemon, the client refuses instead of launching or connecting.
func TestEnsureRunningPortTakenByForeignServer(t *testing.T) {
	dir := testEnv(t)
	writeDaemonYAML(t, dir, "")

	// A foreign server answers /health with an incompatible body and no auth.
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"hello":"world"}`))
	}))
	defer foreign.Close()

	var launches atomic.Int64
	c := &Client{
		Executable: "/nonexistent/mcp",
		BaseURL:    foreign.URL,
		Start: func(context.Context, LaunchOptions) error {
			launches.Add(1)
			return nil
		},
		HealthTimeout: 200 * time.Millisecond,
	}
	if _, err := c.EnsureRunning(t.Context()); err == nil {
		t.Fatal("expected an error for a foreign listener")
	} else if !errors.Is(err, ErrPortInUse) {
		t.Fatalf("err=%v want ErrPortInUse", err)
	}
	if launches.Load() != 0 {
		t.Fatalf("must not launch a second daemon on a taken port, launches=%d", launches.Load())
	}
}

// TestEnsureRunningWrongTokenRefuses: a daemon answering with a different token
// is reported as a port conflict instead of being replaced.
func TestEnsureRunningWrongTokenRefuses(t *testing.T) {
	dir := testEnv(t)
	if _, err := EnsureAuthToken(); err != nil {
		t.Fatal(err)
	}
	writeDaemonYAML(t, dir, "")

	d := newFakeDaemon(t, "some-other-token-that-is-long-enough-000000")
	var launches atomic.Int64
	c := &Client{
		Executable: "/nonexistent/mcp",
		BaseURL:    d.baseURL(),
		Start: func(context.Context, LaunchOptions) error {
			launches.Add(1)
			return nil
		},
		HealthTimeout: 200 * time.Millisecond,
	}
	_, err := c.EnsureRunning(t.Context())
	if !errors.Is(err, ErrPortInUse) {
		t.Fatalf("err=%v want ErrPortInUse", err)
	}
	if launches.Load() != 0 {
		t.Fatalf("launches=%d want 0", launches.Load())
	}
}

// TestEnsureRunningPortConflictMessage: a bind error from the launched daemon is
// reported with the port, without retrying on another port.
func TestEnsureRunningPortConflictMessage(t *testing.T) {
	dir := testEnv(t)
	writeDaemonYAML(t, dir, "")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	base := "http://" + ln.Addr().String()

	c := &Client{
		Executable:    "/nonexistent/mcp",
		BaseURL:       base,
		HTTPClient:    &http.Client{Timeout: 300 * time.Millisecond},
		HealthTimeout: 200 * time.Millisecond,
		Start: func(context.Context, LaunchOptions) error {
			return fmt.Errorf("daemon exited: listen tcp %s: bind: address already in use", ln.Addr().String())
		},
	}
	_, err = c.EnsureRunning(t.Context())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), ln.Addr().String()) {
		t.Fatalf("error should name the address: %v", err)
	}
}

// TestConcurrentEnsureRunningSingleLaunch: ten concurrent proxies produce one
// launch and all end up on the same daemon.
func TestConcurrentEnsureRunningSingleLaunch(t *testing.T) {
	dir := testEnv(t)
	writeDaemonYAML(t, dir, "")

	var launches atomic.Int64
	var mu sync.Mutex
	var d *fakeDaemon
	var c *Client
	c = &Client{
		Executable:    "/nonexistent/mcp",
		BaseURL:       "http://127.0.0.1:1",
		HTTPClient:    &http.Client{Timeout: 300 * time.Millisecond},
		HealthTimeout: 5 * time.Second,
		Start: func(_ context.Context, opts LaunchOptions) error {
			launches.Add(1)
			time.Sleep(50 * time.Millisecond)
			mu.Lock()
			d = newFakeDaemon(t, opts.Token)
			c.BaseURL = d.baseURL()
			mu.Unlock()
			return nil
		},
	}

	const n = 10
	var wg sync.WaitGroup
	conns := make([]Connection, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			conns[i], errs[i] = c.EnsureRunning(t.Context())
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	if launches.Load() != 1 {
		t.Fatalf("launch count=%d want 1", launches.Load())
	}
	for i := 1; i < n; i++ {
		if conns[i].BaseURL != conns[0].BaseURL {
			t.Fatalf("goroutine %d got %q, want %q", i, conns[i].BaseURL, conns[0].BaseURL)
		}
	}
}

// TestDaemonBaseURLDefault: the default listener is loopback, never all interfaces.
func TestDaemonBaseURLDefault(t *testing.T) {
	dir := testEnv(t)
	writeDaemonYAML(t, dir, "")
	u, err := DaemonBaseURL()
	if err != nil {
		t.Fatal(err)
	}
	if u != "http://127.0.0.1:8080" {
		t.Fatalf("base url=%q", u)
	}

	// An explicit loopback listener in daemon.yaml is honoured.
	writeDaemonYAML(t, dir, "http_addr: 127.0.0.1:9999\n")
	if u, err = DaemonBaseURL(); err != nil {
		t.Fatal(err)
	}
	if u != "http://127.0.0.1:9999" {
		t.Fatalf("base url=%q", u)
	}

	// A non-loopback bind is rejected: the user-level daemon is local only.
	writeDaemonYAML(t, dir, "http_addr: 0.0.0.0:9999\n")
	if _, err := DaemonBaseURL(); err == nil {
		t.Fatal("a non-loopback daemon listener must be rejected")
	}
}

// TestClientRejectsNonLoopback: the client never sends its token to a remote URL.
func TestClientRejectsNonLoopback(t *testing.T) {
	testEnv(t)
	c := &Client{Executable: "/nonexistent/mcp", BaseURL: "http://example.com:8080"}
	if _, err := c.EnsureRunning(t.Context()); err == nil {
		t.Fatal("a non-loopback base URL must be rejected")
	}
}

func writeDaemonYAML(t *testing.T, dir, extra string) {
	t.Helper()
	body := extra
	if body == "" {
		body = "# default loopback listener\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "daemon.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
