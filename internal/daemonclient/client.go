package daemonclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/daemon"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
)

const (
	// DefaultBaseURL is the user-level daemon listener: loopback only.
	DefaultBaseURL = "http://127.0.0.1:8080"
	// DefaultHealthTimeout bounds how long a proxy waits for a cold daemon.
	DefaultHealthTimeout = 15 * time.Second
	// daemonLockName is the singleton lock file under <userdir>/runtime.
	daemonLockName = "daemon.lock"
)

// ErrNonLoopbackListener reports a daemon listener that is not local. The
// user-level daemon is single-user and local; its token is never sent off-host.
var ErrNonLoopbackListener = errors.New("daemon listener must be a loopback address")

// ErrPortInUse reports that the daemon port is answered by something that is not
// our daemon, or by our daemon with a different token. Launching a second daemon
// would be pointless and hiding the conflict would be worse, so it is an error.
var ErrPortInUse = errors.New("daemon port is already in use by another listener")

// Connection is how a proxy reaches the user-level daemon. The token is a local
// secret: String and formatting never expose it.
type Connection struct {
	BaseURL string
	Token   string
}

// String renders the connection without the token.
func (c Connection) String() string {
	return "daemonclient.Connection{" + c.BaseURL + "}"
}

// GoString keeps %#v from printing the token.
func (c Connection) GoString() string { return c.String() }

// Client finds or starts the single user-level daemon.
type Client struct {
	// Executable is the installed binary used to launch the daemon.
	Executable string
	// BaseURL overrides the listener discovered from daemon.yaml.
	BaseURL string
	// HealthTimeout bounds the wait for an authenticated /health (default 15s).
	HealthTimeout time.Duration
	// Start launches a detached daemon. Injectable so unit tests never spawn
	// real detached processes.
	Start func(ctx context.Context, opts LaunchOptions) error
	// HTTPClient overrides the probe client (tests).
	HTTPClient *http.Client

	mu       sync.Mutex // serializes the launch decision
	clientMu sync.Mutex // guards client
	client   *http.Client
}

// LaunchOptions is what the daemon starter needs: where to run from and which
// token to enforce. The token is passed through the child environment, never on
// the command line.
type LaunchOptions struct {
	Executable string
	UserDir    string
	Token      string
	LogDir     string
	ConfigPath string
}

// userDir returns the resolved user-level install directory.
func userDir() (string, error) {
	return config.UserDataDir()
}

// DaemonBaseURL resolves the daemon listener from daemon.yaml, defaulting to
// loopback. A non-loopback listener is rejected: the user-level daemon is local.
func DaemonBaseURL() (string, error) {
	path, err := daemon.DefaultDaemonConfigPath()
	if err != nil {
		return "", err
	}
	addr := ""
	if data, readErr := os.ReadFile(path); readErr == nil {
		var cfg struct {
			HTTPAddr string `yaml:"http_addr"`
		}
		if err := unmarshalYAML(data, &cfg); err != nil {
			return "", fmt.Errorf("parse daemon config %s: %w", path, err)
		}
		addr = strings.TrimSpace(cfg.HTTPAddr)
	}
	if addr == "" {
		return DefaultBaseURL, nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("daemon http_addr %q: %w", addr, err)
	}
	if !isLoopbackHost(host) {
		return "", fmt.Errorf("%w: %s", ErrNonLoopbackListener, addr)
	}
	return "http://" + addr, nil
}

// isLoopbackHost reports whether host addresses only the local machine. An empty
// host binds every interface and is therefore not loopback.
func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	switch host {
	case "127.0.0.1", "::1", "localhost":
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// baseURL returns the effective listener URL for this client.
func (c *Client) baseURL() (string, error) {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/"), nil
	}
	return DaemonBaseURL()
}

// EnsureRunning returns a live connection, launching the daemon when needed.
//
// The flow is: resolve the loopback listener and the local token, probe the
// authenticated /health endpoint, and only if that fails launch a detached
// daemon and wait for health. The wait is bounded and never requires a specific
// collection to be ready: a fresh collection may still be indexing.
func (c *Client) EnsureRunning(ctx context.Context) (Connection, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Connection{}, err
	}
	baseURL, err := c.baseURL()
	if err != nil {
		return Connection{}, err
	}
	if err := validateLoopbackURL(baseURL); err != nil {
		return Connection{}, err
	}
	token, err := EnsureAuthToken()
	if err != nil {
		return Connection{}, err
	}

	if err := c.probeHealth(ctx, baseURL, token); err == nil {
		return Connection{BaseURL: baseURL, Token: token}, nil
	} else if errors.Is(err, ErrPortInUse) {
		return Connection{}, fmt.Errorf("%w: %s: %v", ErrPortInUse, baseURL, err)
	}

	// Serialize the decision inside this process; cross-process single-instance
	// is the daemon's own OS lock, and losing racers simply wait for health.
	c.mu.Lock()
	defer c.mu.Unlock()
	// Re-resolve: a competing launch (or a daemon that rewrote daemon.yaml) may
	// have changed the listener while this goroutine waited for the lock.
	if resolved, err := c.baseURL(); err == nil {
		baseURL = resolved
	}
	if err := c.probeHealth(ctx, baseURL, token); err == nil {
		return Connection{BaseURL: baseURL, Token: token}, nil
	} else if errors.Is(err, ErrPortInUse) {
		return Connection{}, fmt.Errorf("%w: %s: %v", ErrPortInUse, baseURL, err)
	}
	if err := c.launch(ctx, token); err != nil {
		return Connection{}, err
	}
	// The launched daemon is the authority on where it listens.
	if resolved, err := c.baseURL(); err == nil {
		baseURL = resolved
	}
	if err := c.waitHealthy(ctx, baseURL, token); err != nil {
		return Connection{}, err
	}
	return Connection{BaseURL: baseURL, Token: token}, nil
}

// launch starts a detached daemon using the injected starter.
func (c *Client) launch(ctx context.Context, token string) error {
	if c.Start == nil {
		return errors.New("daemonclient: no daemon starter configured")
	}
	dir, err := userDir()
	if err != nil {
		return err
	}
	logs, err := config.GlobalLogsDir()
	if err != nil {
		return err
	}
	configPath, err := config.GlobalConfigPath()
	if err != nil {
		return err
	}
	opts := LaunchOptions{
		Executable: c.Executable,
		UserDir:    dir,
		Token:      token,
		LogDir:     logs,
		ConfigPath: configPath,
	}
	if err := c.Start(ctx, opts); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	return nil
}

// waitHealthy polls the authenticated health endpoint until the deadline.
func (c *Client) waitHealthy(ctx context.Context, baseURL, token string) error {
	timeout := c.HealthTimeout
	if timeout <= 0 {
		timeout = DefaultHealthTimeout
	}
	deadline := time.Now().Add(timeout)
	waitCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	backoff := 50 * time.Millisecond
	var lastErr error
	for {
		if err := c.probeHealth(waitCtx, baseURL, token); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if waitCtx.Err() != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("daemon did not pass the authenticated health check within %s (%s): %w",
				timeout, baseURL, lastErr)
		}
		select {
		case <-waitCtx.Done():
		case <-time.After(backoff):
		}
		if backoff < 500*time.Millisecond {
			backoff *= 2
		}
	}
}

// probeHealth calls GET /health and verifies it is our daemon: a 200 response
// with the expected service identity. A foreign listener on the port therefore
// fails with ErrPortInUse instead of being mistaken for the daemon.
func (c *Client) probeHealth(ctx context.Context, baseURL, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		// Our daemon, but it holds a different token: a second daemon would not
		// fix that, and silently regenerating the token would lock out the
		// running one.
		return fmt.Errorf("%w: daemon rejected the local auth token (HTTP %d)", ErrPortInUse, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: /health returned HTTP %d", ErrPortInUse, resp.StatusCode)
	}
	var payload struct {
		Status  string `json:"status"`
		Service string `json:"service"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("%w: /health is not our daemon: %w", ErrPortInUse, err)
	}
	if payload.Status != "ok" || !strings.Contains(payload.Service, "semantic-search") {
		return fmt.Errorf("%w: /health is not our daemon: %s", ErrPortInUse, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	c.clientMu.Lock()
	defer c.clientMu.Unlock()
	if c.client == nil {
		c.client = &http.Client{
			Timeout: 3 * time.Second,
			// The local daemon never redirects; following one could leak the
			// Bearer token to another host.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return c.client
}

// validateLoopbackURL rejects any base URL that would send the token off-host.
func validateLoopbackURL(baseURL string) error {
	host := baseURL
	if idx := strings.Index(host, "://"); idx >= 0 {
		host = host[idx+3:]
	}
	if idx := strings.IndexAny(host, "/?#"); idx >= 0 {
		host = host[:idx]
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if !isLoopbackHost(host) {
		return fmt.Errorf("%w: %s", ErrNonLoopbackListener, baseURL)
	}
	return nil
}

// Running reports whether an authenticated daemon is already listening, without
// launching one. Management commands use it so they never start a daemon as a
// side effect.
func (c *Client) Running(ctx context.Context) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	baseURL, err := c.baseURL()
	if err != nil {
		return false, err
	}
	if err := validateLoopbackURL(baseURL); err != nil {
		return false, err
	}
	token, err := ReadAuthToken()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if err := c.probeHealth(ctx, baseURL, token); err != nil {
		if errors.Is(err, ErrPortInUse) {
			return false, err
		}
		return false, nil
	}
	return true, nil
}

// CollectionOpen reports whether the daemon currently holds an open handle for a
// collection. It never opens the collection itself.
func (c *Client) CollectionOpen(ctx context.Context, collectionID string) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	running, err := c.Running(ctx)
	if err != nil {
		return false, err
	}
	if !running {
		return false, nil
	}
	baseURL, err := c.baseURL()
	if err != nil {
		return false, err
	}
	token, err := ReadAuthToken()
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/workspaces", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("workspace list: HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Workspaces []struct {
			ID   string `json:"id"`
			Open bool   `json:"open"`
		} `json:"workspaces"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return false, err
	}
	for _, ws := range payload.Workspaces {
		if ws.ID == collectionID {
			return ws.Open, nil
		}
	}
	return false, nil
}

// Stop asks the daemon to shut down gracefully. It is authenticated and reports
// a clear error when no daemon is running.
func (c *Client) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	baseURL, err := c.baseURL()
	if err != nil {
		return err
	}
	if err := validateLoopbackURL(baseURL); err != nil {
		return err
	}
	token, err := ReadAuthToken()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("no daemon is running (auth.token not found)")
		}
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/admin/stop", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("stop daemon: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("stop daemon: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// ErrReloadConflict reports a reload refused because a changed or removed
// collection is still in use. The previous snapshot stays in effect.
var ErrReloadConflict = errors.New("daemon refused the project snapshot: collection in use")

// ReloadProjects asks the daemon to re-read projects.d.
func (c *Client) ReloadProjects(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	baseURL, err := c.baseURL()
	if err != nil {
		return err
	}
	if err := validateLoopbackURL(baseURL); err != nil {
		return err
	}
	token, err := ReadAuthToken()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("no daemon is running (auth.token not found)")
		}
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/admin/reload-projects", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("reload projects: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode == http.StatusConflict {
		var payload struct {
			Error       string   `json:"error"`
			Collections []string `json:"collections"`
		}
		_ = json.Unmarshal(body, &payload)
		if len(payload.Collections) > 0 {
			return fmt.Errorf("%w: %s", ErrReloadConflict, strings.Join(payload.Collections, ", "))
		}
		if payload.Error != "" {
			return fmt.Errorf("%w: %s", ErrReloadConflict, payload.Error)
		}
		return ErrReloadConflict
	}
	if resp.StatusCode >= 400 {
		var payload struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(body, &payload); err == nil && payload.Error != "" {
			return fmt.Errorf("%s", payload.Error)
		}
		return fmt.Errorf("reload projects: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// DaemonLockPath returns <userdir>/runtime/daemon.lock.
func DaemonLockPath() (string, error) {
	dir, err := userDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "runtime", daemonLockName), nil
}

// AcquireDaemonLock takes the singleton daemon lock. A second daemon must not
// start its registry: it reports ErrDaemonAlreadyRunning instead.
func AcquireDaemonLock() (*lock.Lock, error) {
	dir, err := userDir()
	if err != nil {
		return nil, err
	}
	l := lock.NewWithName(filepath.Join(dir, "runtime"), daemonLockName, 60)
	if err := l.TryAcquire(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDaemonAlreadyRunning, err)
	}
	return l, nil
}

// ErrDaemonAlreadyRunning reports that another daemon holds the singleton lock.
var ErrDaemonAlreadyRunning = errors.New("another daemon already holds the singleton lock")
