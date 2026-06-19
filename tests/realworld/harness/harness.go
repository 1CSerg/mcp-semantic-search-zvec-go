//go:build realworld && zvec

package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultHTTPPort = 19301

// ServerProcess wraps a spawned MCP binary subprocess.
type ServerProcess struct {
	Cmd      *exec.Cmd
	HTTPBase string
	Port     int
}

// BaseEnv returns env vars for realworld server subprocesses.
func BaseEnv(repo string) []string {
	return append(os.Environ(),
		"WORKSPACE_ROOT="+CorpusDir(repo),
		"INDEX_DIR="+IndexDir(repo),
		"CONFIG_PATH="+ConfigPath(repo),
		"ENV_PATH="+EnvPath(repo),
		"AUTO_INDEX_ON_START=false",
		"FILE_WATCHER_ENABLED=false",
		"MCP_DISABLE_PARENT_WATCH=true",
	)
}

// WithConfigEnv returns env with CONFIG_PATH overridden.
func WithConfigEnv(repo, configPath string) []string {
	env := BaseEnv(repo)
	out := make([]string, 0, len(env)+1)
	replaced := false
	key := "CONFIG_PATH="
	for _, e := range env {
		if strings.HasPrefix(e, key) {
			out = append(out, key+configPath)
			replaced = true
		} else {
			out = append(out, e)
		}
	}
	if !replaced {
		out = append(out, key+configPath)
	}
	return out
}

func prependBinToPath(env []string, binDir string) []string {
	out := make([]string, 0, len(env))
	pathKey := "PATH="
	if runtime.GOOS == "windows" {
		pathKey = "Path="
	}
	for _, e := range env {
		ek := e
		if strings.HasPrefix(strings.ToLower(e), strings.ToLower(pathKey)) {
			rest := e[len(pathKey):]
			ek = pathKey + binDir + string(os.PathListSeparator) + rest
		}
		out = append(out, ek)
	}
	return out
}

func replaceEnvKey(env []string, key, value string) []string {
	out := make([]string, 0, len(env))
	replaced := false
	for _, e := range env {
		if strings.HasPrefix(e, key) {
			out = append(out, key+value)
			replaced = true
		} else {
			out = append(out, e)
		}
	}
	if !replaced {
		out = append(out, key+value)
	}
	return out
}

// StartHTTPServer spawns --http and waits for /health.
func StartHTTPServer(t *testing.T, repo string, port int, extraEnv ...string) *ServerProcess {
	t.Helper()
	return StartHTTPServerWithArgs(t, repo, port, nil, extraEnv...)
}

// StartHTTPServerWithArgs spawns --http with extra CLI flags before --http/--http-addr.
func StartHTTPServerWithArgs(t *testing.T, repo string, port int, extraArgs []string, extraEnv ...string) *ServerProcess {
	t.Helper()
	if port == 0 {
		port = defaultHTTPPort
	}
	bin := BinPath(repo)
	binDir := BinDir(repo)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	args := append(append([]string{}, extraArgs...), "--http", "--http-addr", addr)
	cmd := exec.Command(bin, args...)
	cmd.Env = prependBinToPath(BaseEnv(repo), binDir)
	cmd.Env = append(cmd.Env, extraEnv...)
	cmd.Dir = binDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start http server: %v", err)
	}
	base := "http://" + addr
	proc := &ServerProcess{Cmd: cmd, HTTPBase: base, Port: port}
	t.Cleanup(func() { killProcess(t, proc.Cmd) })
	waitHTTPHealth(t, base)
	return proc
}

// StartHTTPServerWithEnv starts HTTP with a full custom env slice.
func StartHTTPServerWithEnv(t *testing.T, repo string, port int, env []string) *ServerProcess {
	return startHTTPServerWithEnv(t, repo, port, env)
}

// StartHTTPServerWithConfig starts HTTP using an alternate config file.
func StartHTTPServerWithConfig(t *testing.T, repo, configPath string, port int) *ServerProcess {
	return startHTTPServerWithEnv(t, repo, port, WithConfigEnv(repo, configPath))
}

// StartHTTPServerWithConfigIndex starts HTTP with alternate config and index dir.
func StartHTTPServerWithConfigIndex(t *testing.T, repo, configPath, indexDir string, port int) *ServerProcess {
	env := WithConfigEnv(repo, configPath)
	env = replaceEnvKey(env, "INDEX_DIR=", indexDir)
	return startHTTPServerWithEnv(t, repo, port, env)
}

func startHTTPServerWithEnv(t *testing.T, repo string, port int, env []string) *ServerProcess {
	t.Helper()
	if port == 0 {
		port = defaultHTTPPort + 100
	}
	bin := BinPath(repo)
	binDir := BinDir(repo)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	cmd := exec.Command(bin, "--http", "--http-addr", addr)
	cmd.Env = prependBinToPath(env, binDir)
	cmd.Dir = binDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start http server: %v", err)
	}
	base := "http://" + addr
	proc := &ServerProcess{Cmd: cmd, HTTPBase: base, Port: port}
	t.Cleanup(func() { killProcess(t, proc.Cmd) })
	waitHTTPHealth(t, base)
	return proc
}

// StartMCPServer spawns --stdio and returns an MCP client session.
func StartMCPServer(t *testing.T, repo string) *mcp.ClientSession {
	t.Helper()
	session, cmd := startMCPServerSession(t, repo, nil)
	t.Cleanup(func() { killProcess(t, cmd) })
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// StartMCPServerSessionNoCleanup connects MCP to --stdio without killing the process on cleanup.
func StartMCPServerSessionNoCleanup(t *testing.T, repo string, extraEnv ...string) (*mcp.ClientSession, *exec.Cmd) {
	t.Helper()
	session, cmd := startMCPServerSession(t, repo, extraEnv)
	t.Cleanup(func() { _ = session.Close() })
	return session, cmd
}

func startMCPServerSession(t *testing.T, repo string, extraEnv []string) (*mcp.ClientSession, *exec.Cmd) {
	t.Helper()
	bin := BinPath(repo)
	binDir := BinDir(repo)
	cmd := exec.Command(bin, "--stdio")
	cmd.Env = prependBinToPath(BaseEnv(repo), binDir)
	cmd.Env = append(cmd.Env, extraEnv...)
	cmd.Dir = binDir

	client := mcp.NewClient(&mcp.Implementation{Name: "realworld-test", Version: "0"}, nil)
	transport := &mcp.CommandTransport{Command: cmd}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("mcp connect: %v", err)
	}
	return session, cmd
}

func waitHTTPHealth(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	client := &http.Client{Timeout: 3 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(base + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(400 * time.Millisecond)
	}
	t.Fatalf("server not healthy at %s/health", base)
}

// WaitIndexIdle polls /v1/status until indexing is idle.
func WaitIndexIdle(t *testing.T, httpBase string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		status := GetJSON(t, httpBase+"/v1/status")
		idx, _ := status["indexing"].(map[string]any)
		if idx == nil {
			time.Sleep(800 * time.Millisecond)
			continue
		}
		if idx["state"] == "error" {
			t.Fatalf("indexing error: %v", idx["message"])
		}
		if running, _ := idx["running"].(bool); !running {
			return status
		}
		time.Sleep(800 * time.Millisecond)
	}
	t.Fatal("indexing did not become idle")
	return nil
}

// ForceReindex triggers POST /v1/reindex with force=true.
func ForceReindex(t *testing.T, httpBase string) {
	t.Helper()
	postJSON(t, httpBase+"/v1/reindex", map[string]any{"force": true})
}

// AssertSearchHit searches and requires a hit matching criteria.
func AssertSearchHit(t *testing.T, httpBase, query, pathSuffix, wantSymbol, wantStrategy string) map[string]any {
	t.Helper()
	body := map[string]any{"query": query, "limit": 20}
	if strings.Contains(query, "path_glob:") {
		parts := strings.SplitN(query, "|", 2)
		body["query"] = strings.TrimSpace(parts[0])
		if len(parts) == 2 {
			body["path_glob"] = strings.TrimSpace(strings.TrimPrefix(parts[1], "path_glob:"))
		}
	}
	resp := postJSON(t, httpBase+"/v1/search", body)
	results, _ := resp["results"].([]any)
	for _, r := range results {
		item, _ := r.(map[string]any)
		if item == nil {
			continue
		}
		path, _ := item["path"].(string)
		if pathSuffix != "" && !strings.Contains(path, pathSuffix) {
			continue
		}
		if wantSymbol != "" {
			sym, _ := item["symbol_name"].(string)
			if sym != wantSymbol {
				continue
			}
		}
		if wantStrategy != "" {
			strat, _ := item["chunk_strategy"].(string)
			if strat != wantStrategy {
				t.Fatalf("hit %q has chunk_strategy=%q want %q", path, strat, wantStrategy)
			}
		}
		return item
	}
	t.Fatalf("no search hit for query=%q pathSuffix=%q symbol=%q strategy=%q in %v", query, pathSuffix, wantSymbol, wantStrategy, resp)
	return nil
}

// StartMockEmbed runs tests/realworld/mock/embed.go on the given port.
func StartMockEmbed(t *testing.T, repo string, port, dims int, fail bool) *exec.Cmd {
	return StartMockEmbedFailCount(t, repo, port, dims, fail, 0)
}

// StartMockEmbedFailCount starts mock embed; failCount>0 returns 503 for first N embedding requests (E4).
func StartMockEmbedFailCount(t *testing.T, repo string, port, dims int, fail bool, failCount int) *exec.Cmd {
	t.Helper()
	mockSrc := filepath.Join(repo, "tests", "realworld", "mock", "embed.go")
	args := []string{"run", mockSrc, "-port", fmt.Sprintf("%d", port), "-dims", fmt.Sprintf("%d", dims)}
	if fail {
		args = append(args, "-fail")
	}
	if failCount > 0 {
		args = append(args, "-fail-count", fmt.Sprintf("%d", failCount))
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = repo
	if err := cmd.Start(); err != nil {
		t.Fatalf("start mock embed: %v", err)
	}
	t.Cleanup(func() { killProcess(t, cmd) })
	waitMockEmbed(t, port)
	return cmd
}

func waitMockEmbed(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/models", port)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("mock embed not ready on port %d", port)
}

// WriteTempConfig copies a template and replaces MOCK_PORT placeholder.
func WriteTempConfig(t *testing.T, repo, profile string, mockPort int) string {
	t.Helper()
	src := ConfigTemplate(repo, profile)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read config template: %v", err)
	}
	text := strings.ReplaceAll(string(data), "MOCK_PORT", fmt.Sprintf("%d", mockPort))
	dir := filepath.Join(RealworldRoot(repo), "tmp-config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir tmp-config: %v", err)
	}
	dst := filepath.Join(dir, profile+".yaml")
	if err := os.WriteFile(dst, []byte(text), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return dst
}

// WriteDetectableMockConfig writes a mock-embed config with a unique active_profile marker.
func WriteDetectableMockConfig(t *testing.T, repo, activeProfile string, mockPort int) string {
	t.Helper()
	src := ConfigTemplate(repo, "daemon-workspace")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read config template: %v", err)
	}
	text := strings.ReplaceAll(string(data), "MOCK_PORT", fmt.Sprintf("%d", mockPort))
	text = strings.Replace(text, "active_profile: daemon_smoke", "active_profile: "+activeProfile, 1)
	text = strings.Replace(text, "daemon_smoke:", activeProfile+":", 1)
	dir := filepath.Join(RealworldRoot(repo), "tmp-config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir tmp-config: %v", err)
	}
	dst := filepath.Join(dir, activeProfile+".yaml")
	if err := os.WriteFile(dst, []byte(text), 0o644); err != nil {
		t.Fatalf("write detectable config: %v", err)
	}
	return dst
}

// AssertJSONExcludesSubstring fails when marshaled JSON contains substr.
func AssertJSONExcludesSubstring(t *testing.T, label string, v any, substr string) {
	t.Helper()
	if substr == "" {
		return
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %s: %v", label, err)
	}
	body := string(raw)
	if strings.Contains(body, substr) {
		t.Fatalf("%s JSON contains forbidden substring %q: %s", label, substr, body)
	}
	slash := filepath.ToSlash(substr)
	if slash != substr && strings.Contains(body, slash) {
		t.Fatalf("%s JSON contains forbidden substring %q: %s", label, slash, body)
	}
}

// AssertNoLeftovers checks no orphan locks, listening harness ports, or stray MCP processes.
func AssertNoLeftovers(t *testing.T, repo string) {
	t.Helper()
	indexDir := IndexDir(repo)
	for _, name := range []string{"index.lock", "stdio.lock"} {
		p := filepath.Join(indexDir, name)
		if _, err := os.Stat(p); err == nil {
			t.Errorf("leftover lock file: %s", p)
		}
	}
	// Common realworld test ports should not remain bound after cleanup.
	for _, port := range []int{19301, 19302, 19400, daemonHTTPPort} {
		if IsPortListening("127.0.0.1", port) {
			t.Errorf("port %d still listening after scenario cleanup", port)
		}
	}
	for port := 19320; port <= 19396; port++ {
		if IsPortListening("127.0.0.1", port) {
			t.Errorf("port %d still listening after scenario cleanup", port)
		}
	}
	binName := filepath.Base(BinPath(repo))
	if stray := findStrayProcesses(binName); len(stray) > 0 {
		t.Errorf("stray %s processes after cleanup: %v", binName, stray)
	}
}

func findStrayProcesses(binName string) []int {
	if runtime.GOOS == "windows" {
		return nil // Avoid flaky tasklist parsing; port checks cover most cases.
	}
	out, err := exec.Command("pgrep", "-f", binName).Output()
	if err != nil {
		return nil
	}
	var pids []int
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var pid int
		if _, err := fmt.Sscanf(line, "%d", &pid); err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids
}

func killProcess(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

// GetJSON performs GET and unmarshals a JSON object.
func GetJSON(t *testing.T, url string) map[string]any {
	t.Helper()
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode >= 400 {
		t.Fatalf("GET %s: %s", url, data)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", url, err)
	}
	return out
}

func postJSON(t *testing.T, url string, body map[string]any) map[string]any {
	t.Helper()
	return PostJSON(t, url, body)
}

// PostJSON performs POST and unmarshals a JSON object (fails on HTTP >= 400).
func PostJSON(t *testing.T, url string, body map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal POST %s: %v body=%s", url, err, data)
	}
	if resp.StatusCode >= 400 {
		t.Fatalf("POST %s status=%d body=%s", url, resp.StatusCode, data)
	}
	return out
}

// CallMCPTool invokes an MCP tool and unmarshals JSON text content.
func CallMCPTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("CallTool %s: empty content", name)
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("CallTool %s: content=%T", name, res.Content[0])
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(text.Text), &payload); err != nil {
		t.Fatalf("CallTool %s unmarshal: %v text=%s", name, err, text.Text)
	}
	return payload
}

// WaitIndexIdleViaMCP polls index_status until indexing is idle.
func WaitIndexIdleViaMCP(t *testing.T, session *mcp.ClientSession) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		status := CallMCPTool(t, session, "index_status", map[string]any{})
		idx, _ := status["indexing"].(map[string]any)
		if idx == nil {
			time.Sleep(800 * time.Millisecond)
			continue
		}
		if idx["state"] == "error" {
			t.Fatalf("indexing error: %v", idx["message"])
		}
		if running, _ := idx["running"].(bool); !running {
			return status
		}
		time.Sleep(800 * time.Millisecond)
	}
	t.Fatal("indexing did not become idle via MCP")
	return nil
}
