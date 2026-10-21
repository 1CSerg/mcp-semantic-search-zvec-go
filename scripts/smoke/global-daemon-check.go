//go:build ignore

// global-daemon-check is the native end-to-end acceptance driver for the
// "one application per user, one daemon, N collections" deployment.
//
// It exercises the real production binary: a mock embeddings server, one shared
// daemon, and two MCP stdio proxies that perform a genuine initialize/tools-call
// handshake (not just curl). Every check verifies the *source* of a result, so a
// collection can never pass by merely lacking similar words.
//
// Usage:
//
//	go run scripts/smoke/global-daemon-check.go -binary <exe> -work <dir>
//
// The driver creates its own user dir, projects and ports under -work and shuts
// every process down in a deferred cleanup. Exit code 0 means all checks passed.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	binaryPath = flag.String("binary", "", "path to the built mcp-semantic-search-zvec-go binary")
	workDir    = flag.String("work", "", "scratch directory (created if missing)")
	keepWork   = flag.Bool("keep", false, "keep the scratch directory for inspection")
	timeout    = flag.Duration("timeout", 120*time.Second, "overall budget for indexing waits")
	dims       = flag.Int("dims", 64, "mock embedding dimensions")
)

func main() {
	flag.Parse()
	if *binaryPath == "" || *workDir == "" {
		fmt.Fprintln(os.Stderr, "usage: global-daemon-check -binary <exe> -work <dir>")
		os.Exit(2)
	}
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------- check runner

type runner struct {
	mu       sync.Mutex
	checks   int
	failures []string
}

func (r *runner) check(name string, fn func() error) {
	r.mu.Lock()
	r.checks++
	r.mu.Unlock()
	if err := fn(); err != nil {
		fmt.Printf("  FAIL %s: %v\n", name, err)
		r.mu.Lock()
		r.failures = append(r.failures, fmt.Sprintf("%s: %v", name, err))
		r.mu.Unlock()
		return
	}
	fmt.Printf("  PASS %s\n", name)
}

func (r *runner) summary() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.failures) == 0 {
		fmt.Printf("PASSED all %d checks.\n", r.checks)
		return nil
	}
	fmt.Printf("FAILED %d/%d checks:\n", len(r.failures), r.checks)
	for _, f := range r.failures {
		fmt.Printf("  - %s\n", f)
	}
	return fmt.Errorf("%d check(s) failed", len(r.failures))
}

// ------------------------------------------------------------- mock embeddings

// deterministicEmbedder returns a stable embedding per text so texts sharing
// characters score higher than unrelated ones. A constant vector would make every
// document equidistant and collection isolation untestable.
type deterministicEmbedder struct {
	dims int
}

func (e deterministicEmbedder) embed(text string) []float64 {
	vec := make([]float64, e.dims)
	for _, r := range strings.ToLower(text) {
		vec[int(r)%e.dims] += 1
	}
	// Normalise so cosine/dot scores are comparable across text lengths.
	norm := 0.0
	for _, v := range vec {
		norm += v * v
	}
	if norm == 0 {
		vec[0] = 1
		return vec
	}
	norm = math.Sqrt(norm)
	for i := range vec {
		vec[i] /= norm
	}
	return vec
}

func startMockEmbeddings(ctx context.Context, dims int) (string, error) {
	embedder := deterministicEmbedder{dims: dims}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/v1/embeddings", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		data := make([]map[string]any, 0, len(req.Input))
		for i, text := range req.Input {
			data = append(data, map[string]any{
				"object":    "embedding",
				"index":     i,
				"embedding": embedder.embed(text),
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(listener) }()
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	return "http://" + listener.Addr().String(), nil
}

// ------------------------------------------------------------------- processes

type processSet struct {
	mu    sync.Mutex
	pids  []int
	cmds  []*exec.Cmd
	procs map[int]*os.Process
}

func newProcessSet() *processSet {
	return &processSet{procs: map[int]*os.Process{}}
}

func (p *processSet) track(cmd *exec.Cmd) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if cmd.Process != nil {
		p.pids = append(p.pids, cmd.Process.Pid)
		p.procs[cmd.Process.Pid] = cmd.Process
	}
	p.cmds = append(p.cmds, cmd)
	return cmd.Process.Pid
}

func (p *processSet) alive(pid int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	proc, ok := p.procs[pid]
	if !ok {
		return false
	}
	return proc.Signal(os.Signal(nil)) == nil
}

// stopAll terminates every process this driver started. It never kills processes
// it did not start.
func (p *processSet) stopAll() {
	p.mu.Lock()
	procs := make([]*os.Process, 0, len(p.procs))
	for _, proc := range p.procs {
		procs = append(procs, proc)
	}
	p.procs = map[int]*os.Process{}
	p.mu.Unlock()
	for _, proc := range procs {
		_ = proc.Kill()
		_, _ = proc.Wait()
	}
}

// ------------------------------------------------------------------ http calls

type daemonAPI struct {
	baseURL string
	token   string
	client  *http.Client
}

func (d *daemonAPI) do(ctx context.Context, method, path string, body any, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, d.baseURL+path, reader)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+d.token)
	resp, err := d.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return resp.StatusCode, err
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("decode %s: %w (%s)", path, err, truncate(string(raw), 300))
		}
	}
	return resp.StatusCode, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ------------------------------------------------------------------ the driver

type env struct {
	work       string
	globalDir  string
	bin        string
	port       int
	baseURL    string
	token      string
	api        *daemonAPI
	procs      *processSet
	ctx        context.Context
	projectA   string
	projectB   string
	projectC   string
	cyrillic   string
	collection map[string]string // label -> collection id
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	work := *workDir
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	if !*keepWork {
		defer func() { _ = os.RemoveAll(work) }()
	}

	e := &env{
		work:       work,
		globalDir:  filepath.Join(work, "userdir"),
		bin:        *binaryPath,
		procs:      newProcessSet(),
		ctx:        ctx,
		collection: map[string]string{},
	}
	// The daemon is detached by design, so it is not a tracked child: stop it
	// through its own admin API before killing the processes this driver spawned.
	defer func() {
		if e.token != "" {
			if out, err := e.runBinary("--stop-daemon"); err != nil {
				fmt.Fprintf(os.Stderr, "cleanup: stop-daemon: %v (%s)\n", err, truncate(out, 200))
			}
			// Give the daemon a moment to release its singleton lock and port.
			time.Sleep(700 * time.Millisecond)
		}
	}()
	defer e.procs.stopAll()

	fmt.Printf("Native end-to-end smoke\n  binary: %s\n  work:   %s\n\n", e.bin, work)

	r := &runner{}
	if err := e.setup(ctx); err != nil {
		return err
	}

	r.check("daemon answers authenticated /health", e.checkHealth)
	r.check("project A and B have distinct collections", e.checkDistinctCollections)
	r.check("MCP proxy for A performs a real initialize + tools/list", e.checkMCPHandshake)
	r.check("reindex A and B completes", e.checkReindex)
	r.check("search in A returns only A's files", e.checkSearchIsolationA)
	r.check("search in B returns only B's files", e.checkSearchIsolationB)
	r.check("adding C while A/B run keeps the daemon PID", e.checkAddThirdProject)
	r.check("closing proxy A keeps the daemon and B working", e.checkProxyClose)
	r.check("a second proxy for A shares the collection without a second owner", e.checkSecondProxy)
	r.check("help and the removed --gui flag leave the live daemon and proxies untouched", e.checkHeadlessCLIWithLiveDaemon)
	r.check("same collection id on another root is refused", e.checkDuplicateIDRefused)
	r.check("Cyrillic project paths keep zvec inside the global collection dir", e.checkCyrillic)
	r.check("daemon restart reopens the collections", e.checkDaemonRestart)
	// Last: it changes the global embedding profile and rebuilds one collection.
	r.check("changing the embedding profile requires a rebuild, then works", e.checkProfileChange)

	return r.summary()
}

func (e *env) setup(ctx context.Context) error {
	if err := os.MkdirAll(e.globalDir, 0o700); err != nil {
		return err
	}
	embedURL, err := startMockEmbeddings(ctx, *dims)
	if err != nil {
		return fmt.Errorf("start mock embeddings: %w", err)
	}
	port, err := freePort()
	if err != nil {
		return err
	}
	e.port = port
	e.baseURL = fmt.Sprintf("http://127.0.0.1:%d", port)

	configBody := fmt.Sprintf(`active_profile: smoke
profiles:
  smoke:
    provider: openai_compatible
    model: mock
    base_url: %s/v1
    dimensions: %d
    max_input_tokens: 512
    batch_size: 16
file_watcher:
  enabled: false
indexing:
  extensions: [".go", ".md", ".txt"]
  skip_dirs: [".git"]
  chunking:
    strategy: hybrid
    min_chunk_tokens: 4
`, embedURL, *dims)
	if err := os.WriteFile(filepath.Join(e.globalDir, "config.yaml"), []byte(configBody), 0o600); err != nil {
		return err
	}
	daemonBody := fmt.Sprintf("http_addr: 127.0.0.1:%d\nmax_open_workspaces: 10\nauto_index_on_open: false\n", port)
	if err := os.WriteFile(filepath.Join(e.globalDir, "daemon.yaml"), []byte(daemonBody), 0o600); err != nil {
		return err
	}

	e.projectA = filepath.Join(e.work, "project-a")
	e.projectB = filepath.Join(e.work, "project-b")
	e.projectC = filepath.Join(e.work, "project-c")
	e.cyrillic = filepath.Join(e.work, "Проект с пробелами")
	if err := writeFixture(e.projectA, "alpha", "ALPHA"); err != nil {
		return err
	}
	if err := writeFixture(e.projectB, "beta", "BETA"); err != nil {
		return err
	}

	if out, err := e.runBinary("--project-add", e.projectA); err != nil {
		return fmt.Errorf("project-add A: %w\n%s", err, out)
	}
	if out, err := e.runBinary("--project-add", e.projectB); err != nil {
		return fmt.Errorf("project-add B: %w\n%s", err, out)
	}
	idA, err := projectCollectionID(e.projectA)
	if err != nil {
		return err
	}
	idB, err := projectCollectionID(e.projectB)
	if err != nil {
		return err
	}
	e.collection["A"] = idA
	e.collection["B"] = idB

	if out, err := e.runBinary("--ensure-daemon"); err != nil {
		return fmt.Errorf("ensure-daemon: %w\n%s", err, out)
	}
	tokenBytes, err := os.ReadFile(filepath.Join(e.globalDir, "auth.token"))
	if err != nil {
		return fmt.Errorf("read auth.token: %w", err)
	}
	e.token = strings.TrimSpace(string(tokenBytes))
	e.api = &daemonAPI{baseURL: e.baseURL, token: e.token, client: &http.Client{Timeout: 90 * time.Second}}
	return nil
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	return port, listener.Close()
}

// writeFixture creates a project with a marker unique to it, so a search result
// can be attributed to exactly one collection.
func writeFixture(root, name, marker string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	goFile := fmt.Sprintf(`package %s

// %s_MARKER_ONLY_IN_%s identifies this project's fixtures.
func %sHandler() string {
	return "%s unique payload %s"
}
`, name, marker, strings.ToUpper(name), name, marker, marker)
	mdFile := fmt.Sprintf("# %s project\n\n%s_MARKER_ONLY_IN_%s unique documentation payload.\n", name, marker, strings.ToUpper(name))
	txtFile := fmt.Sprintf("%s_MARKER_ONLY_IN_%s plain text fixture\n", marker, strings.ToUpper(name))
	files := map[string]string{
		name + ".go":          goFile,
		"README.md":           mdFile,
		"notes.txt":           txtFile,
		"sub/" + name + ".md": fmt.Sprintf("# %s sub\n\n%s_MARKER_ONLY_IN_%s nested payload.\n", name, marker, strings.ToUpper(name)),
	}
	for rel, body := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func projectCollectionID(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, ".mcp-semantic-search-zvec-go", "project.yaml"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "collection_id:"); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`), nil
		}
	}
	return "", fmt.Errorf("no collection_id in %s", root)
}

func (e *env) runBinary(args ...string) (string, error) {
	cmd := exec.Command(e.bin, args...)
	cmd.Env = append(os.Environ(), "MCP_GLOBAL_DIR="+e.globalDir)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// cliContractBudget bounds one headless-CLI invocation. The check exists to prove
// that help and the removed --gui flag exit promptly, so a run that does not exit
// must fail the smoke instead of hanging it.
var cliContractBudget = 30 * time.Second

// runBinaryCode runs the binary and reports its exit code. A non-zero exit is a
// result, not a failure, so the caller can assert the CLI contract. The child gets
// its own working directory and a sandboxed workspace/index/config environment, so
// a regression that falls back to a per-project mode cannot pick up the repository
// or the developer's index.
func (e *env) runBinaryCode(args ...string) (int, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cliContractBudget)
	defer cancel()

	dir := filepath.Join(e.work, "cli-contract")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, "", err
	}
	cmd := exec.CommandContext(ctx, e.bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"MCP_GLOBAL_DIR="+e.globalDir,
		"WORKSPACE_ROOT="+filepath.Join(dir, "workspace"),
		"INDEX_DIR="+filepath.Join(dir, "index"),
		"CONFIG_PATH="+filepath.Join(dir, "config", "config.yaml"),
		"ENV_PATH="+filepath.Join(dir, "config", ".env"),
	)
	// WaitDelay guarantees Wait returns even if the child ignores the kill signal
	// because a grandchild inherited its stdout pipe.
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return 0, string(out), fmt.Errorf("%v %v: did not exit within %s", filepath.Base(e.bin), args, cliContractBudget)
	}
	if err == nil {
		return 0, string(out), nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), string(out), nil
	}
	return 0, string(out), err
}

// ------------------------------------------------------------------- the checks

func (e *env) checkHealth() error {
	var payload struct {
		Status  string `json:"status"`
		Service string `json:"service"`
	}
	status, err := e.api.do(context.Background(), http.MethodGet, "/health", nil, &payload)
	if err != nil {
		return err
	}
	if status != http.StatusOK || payload.Status != "ok" {
		return fmt.Errorf("health status=%d payload=%+v", status, payload)
	}
	if !strings.Contains(payload.Service, "semantic-search") {
		return fmt.Errorf("unexpected service name %q", payload.Service)
	}
	return nil
}

func (e *env) checkDistinctCollections() error {
	if e.collection["A"] == "" || e.collection["B"] == "" {
		return errors.New("missing collection ids")
	}
	if e.collection["A"] == e.collection["B"] {
		return fmt.Errorf("A and B share collection %q", e.collection["A"])
	}
	return nil
}

// mcpSession is one live MCP stdio proxy.
type mcpSession struct {
	session *mcp.ClientSession
	cmd     *exec.Cmd
	pid     int
}

// openProxy starts a real MCP stdio proxy for a project and performs initialize.
func (e *env) openProxy(ctx context.Context, projectRoot string) (*mcpSession, error) {
	projectConfig := filepath.Join(projectRoot, ".mcp-semantic-search-zvec-go", "project.yaml")
	cmd := exec.Command(e.bin, "--stdio-proxy", "--project-config", projectConfig)
	cmd.Env = append(os.Environ(), "MCP_GLOBAL_DIR="+e.globalDir)
	cmd.Stderr = os.Stderr

	transport := &mcp.CommandTransport{Command: cmd}
	client := mcp.NewClient(&mcp.Implementation{Name: "smoke-client", Version: "1"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect proxy: %w", err)
	}
	pid := e.procs.track(cmd)
	return &mcpSession{session: session, cmd: cmd, pid: pid}, nil
}

func (s *mcpSession) close() {
	if s.session != nil {
		_ = s.session.Close()
	}
}

func (e *env) checkMCPHandshake() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	proxyA, err := e.openProxy(ctx, e.projectA)
	if err != nil {
		return err
	}
	defer proxyA.close()

	tools, err := proxyA.session.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("tools/list: %w", err)
	}
	want := map[string]bool{
		"semantic_search": false, "index_status": false, "reindex": false, "check_update": false,
	}
	for _, tool := range tools.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			return fmt.Errorf("tool %q missing from tools/list", name)
		}
	}

	// index_status must answer for the bound collection, not for another one.
	status, err := callTool(ctx, proxyA.session, "index_status", map[string]any{})
	if err != nil {
		return fmt.Errorf("index_status: %w", err)
	}
	if !strings.Contains(status, e.collection["A"]) && !strings.Contains(status, e.projectA) {
		return fmt.Errorf("index_status does not mention collection A: %s", truncate(status, 400))
	}
	return nil
}

func callTool(ctx context.Context, session *mcp.ClientSession, name string, args map[string]any) (string, error) {
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			out.WriteString(text.Text)
		}
	}
	if result.IsError {
		return out.String(), fmt.Errorf("tool %s returned an error: %s", name, truncate(out.String(), 400))
	}
	return out.String(), nil
}

func (e *env) checkReindex() error {
	for _, label := range []string{"A", "B"} {
		id := e.collection[label]
		var payload map[string]any
		status, err := e.api.do(context.Background(), http.MethodPost, "/v1/reindex",
			map[string]any{"force": true, "workspace_id": id}, &payload)
		if err != nil {
			return fmt.Errorf("reindex %s: %w", label, err)
		}
		if status != http.StatusOK {
			return fmt.Errorf("reindex %s: HTTP %d %v", label, status, payload)
		}
		if err := e.waitIdle(id); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
	}
	return nil
}

// waitIdle polls /v1/status until indexing is not running.
func (e *env) waitIdle(collectionID string) error {
	deadline := time.Now().Add(*timeout)
	for time.Now().Before(deadline) {
		var payload struct {
			Indexing struct {
				Running bool   `json:"running"`
				State   string `json:"state"`
				Error   string `json:"error"`
			} `json:"indexing"`
			IndexedFiles int `json:"indexed_files"`
		}
		status, err := e.api.do(context.Background(), http.MethodGet,
			"/v1/status?workspace_id="+collectionID, nil, &payload)
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("status HTTP %d", status)
		}
		if payload.Indexing.Error != "" {
			return fmt.Errorf("indexing error: %s", payload.Indexing.Error)
		}
		if !payload.Indexing.Running && payload.IndexedFiles > 0 {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("indexing did not finish within %s", *timeout)
}

// searchResult mirrors one /v1/search item.
type searchResult struct {
	Path    string  `json:"path"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score"`
}

func (e *env) search(collectionID, query string) ([]searchResult, error) {
	var payload struct {
		Results []searchResult `json:"results"`
	}
	status, err := e.api.do(context.Background(), http.MethodPost, "/v1/search",
		map[string]any{"query": query, "limit": 10, "workspace_id": collectionID}, &payload)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("search HTTP %d", status)
	}
	return payload.Results, nil
}

// checkIsolation verifies every result comes from the expected project. Because
// search result paths are workspace-relative, the check resolves each one against
// the collection's own root and requires the file to exist there and to be absent
// from the other project's root. That is a source check, not a heuristic on words.
func (e *env) checkIsolation(label, ownRoot, otherRoot, ownMarker, otherMarker string) error {
	id := e.collection[label]
	results, err := e.search(id, ownMarker+" unique payload")
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return fmt.Errorf("search in %s returned no results", label)
	}
	own := filepath.Clean(ownRoot)
	other := filepath.Clean(otherRoot)
	for _, res := range results {
		if strings.Contains(res.Snippet, otherMarker) {
			return fmt.Errorf("snippet from %s contains %s: %s", label, otherMarker, truncate(res.Snippet, 200))
		}
		abs := resultAbs(own, res.Path)
		if !isUnder(own, abs) {
			return fmt.Errorf("result %q resolves outside %s", res.Path, ownRoot)
		}
		// The source file must exist in this project and carry this project's marker.
		// Both fixtures share filenames like README.md, so the file content is the only
		// reliable attribution: a hit from the other project would not match.
		content, readErr := os.ReadFile(abs)
		if readErr != nil {
			return fmt.Errorf("result %q does not exist under %s: %v", res.Path, ownRoot, readErr)
		}
		if !strings.Contains(string(content), ownMarker) {
			return fmt.Errorf("result %q in %s does not carry %s (wrong project's file?)", res.Path, ownRoot, ownMarker)
		}
	}

	// The other collection must not answer with this project's files either: every
	// hit it returns for this marker must resolve inside its own root.
	otherResults, err := e.search(e.collection[otherLabel(label)], ownMarker+" unique payload")
	if err != nil {
		return err
	}
	for _, res := range otherResults {
		if strings.Contains(res.Snippet, ownMarker) {
			return fmt.Errorf("the other collection leaked %s content: %s", ownMarker, truncate(res.Snippet, 200))
		}
		abs := resultAbs(other, res.Path)
		if isUnder(own, abs) {
			return fmt.Errorf("the other collection returned %q from %s", res.Path, ownRoot)
		}
		// If the file exists in the other project it must carry that project's marker.
		if content, readErr := os.ReadFile(abs); readErr == nil && strings.Contains(string(content), ownMarker) {
			return fmt.Errorf("the other collection served %s content from %q", ownMarker, res.Path)
		}
	}
	return nil
}

func otherLabel(label string) string {
	if label == "A" {
		return "B"
	}
	return "A"
}

// isUnder reports whether path is inside root. Search result paths are relative
// to the collection's workspace root, so both absolute and relative forms are
// accepted: an absolute path must be inside root, a relative one must resolve
// inside it.
func isUnder(root, path string) bool {
	root = strings.ToLower(filepath.Clean(root))
	cleaned := filepath.Clean(path)
	if filepath.IsAbs(cleaned) {
		abs := strings.ToLower(cleaned)
		if abs == root {
			return true
		}
		return strings.HasPrefix(abs, root+string(filepath.Separator))
	}
	// Relative result path: it cannot escape its own workspace, but it must not be
	// an absolute path from somewhere else either (checked above).
	if strings.HasPrefix(cleaned, "..") {
		return false
	}
	return true
}

// resultAbs resolves a search result path against a workspace root so a test can
// assert the exact file it came from.
func resultAbs(root, resultPath string) string {
	if filepath.IsAbs(resultPath) {
		return filepath.Clean(resultPath)
	}
	return filepath.Join(root, resultPath)
}

func (e *env) checkSearchIsolationA() error {
	return e.checkIsolation("A", e.projectA, e.projectB, "ALPHA", "BETA")
}

func (e *env) checkSearchIsolationB() error {
	return e.checkIsolation("B", e.projectB, e.projectA, "BETA", "ALPHA")
}

func (e *env) checkAddThirdProject() error {
	pidBefore, err := e.daemonPID()
	if err != nil {
		return err
	}
	if err := writeFixture(e.projectC, "gamma", "GAMMA"); err != nil {
		return err
	}
	if out, err := e.runBinary("--project-add", e.projectC); err != nil {
		return fmt.Errorf("project-add C: %w\n%s", err, out)
	}
	idC, err := projectCollectionID(e.projectC)
	if err != nil {
		return err
	}
	e.collection["C"] = idC

	if out, err := e.runBinary("--projects-reload"); err != nil {
		return fmt.Errorf("projects-reload: %w\n%s", err, out)
	}
	pidAfter, err := e.daemonPID()
	if err != nil {
		return err
	}
	if pidBefore != pidAfter {
		return fmt.Errorf("daemon PID changed %d -> %d; a reload must not restart it", pidBefore, pidAfter)
	}

	var payload map[string]any
	if status, err := e.api.do(context.Background(), http.MethodPost, "/v1/reindex",
		map[string]any{"force": true, "workspace_id": idC}, &payload); err != nil || status != http.StatusOK {
		return fmt.Errorf("reindex C: HTTP %d err=%v payload=%v", status, err, payload)
	}
	if err := e.waitIdle(idC); err != nil {
		return fmt.Errorf("C: %w", err)
	}
	results, err := e.search(idC, "GAMMA unique payload")
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return errors.New("search in C returned no results")
	}
	for _, res := range results {
		if !isUnder(e.projectC, resultAbs(e.projectC, res.Path)) {
			return fmt.Errorf("C returned %q, which is outside %s", res.Path, e.projectC)
		}
	}
	// A and B kept their handles: they still answer.
	if _, err := e.search(e.collection["A"], "ALPHA unique payload"); err != nil {
		return fmt.Errorf("A stopped working after adding C: %w", err)
	}
	return nil
}

func (e *env) daemonPID() (int, error) {
	var payload struct {
		Status string `json:"status"`
	}
	if _, err := e.api.do(context.Background(), http.MethodGet, "/health", nil, &payload); err != nil {
		return 0, err
	}
	return daemonPIDFromPort(e.port)
}

// daemonPIDFromPort finds the PID listening on the daemon port.
func daemonPIDFromPort(port int) (int, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		fmt.Sprintf("(Get-NetTCPConnection -LocalPort %d -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1).OwningProcess", port))
	out, err := cmd.Output()
	if err != nil {
		// Non-Windows: fall back to lsof/ss.
		return 0, fmt.Errorf("find daemon pid: %w", err)
	}
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &pid); err != nil {
		return 0, fmt.Errorf("parse daemon pid from %q: %w", strings.TrimSpace(string(out)), err)
	}
	return pid, nil
}

func (e *env) checkProxyClose() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	proxyA, err := e.openProxy(ctx, e.projectA)
	if err != nil {
		return err
	}
	if _, err := callTool(ctx, proxyA.session, "index_status", map[string]any{}); err != nil {
		return fmt.Errorf("A index_status: %w", err)
	}
	proxyB, err := e.openProxy(ctx, e.projectB)
	if err != nil {
		return err
	}
	defer proxyB.close()

	// Closing A's proxy must not touch the daemon or B.
	proxyA.close()
	time.Sleep(300 * time.Millisecond)
	if err := e.checkHealth(); err != nil {
		return fmt.Errorf("daemon died with proxy A: %w", err)
	}
	out, err := callTool(ctx, proxyB.session, "index_status", map[string]any{})
	if err != nil {
		return fmt.Errorf("B index_status after closing A: %w", err)
	}
	if !strings.Contains(out, e.collection["B"]) && !strings.Contains(out, e.projectB) {
		return fmt.Errorf("B index_status is not bound to B: %s", truncate(out, 300))
	}
	return nil
}

func (e *env) checkSecondProxy() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	first, err := e.openProxy(ctx, e.projectA)
	if err != nil {
		return err
	}
	defer first.close()
	second, err := e.openProxy(ctx, e.projectA)
	if err != nil {
		return err
	}
	defer second.close()

	// Two proxies, one collection: both must be served by the daemon, and no
	// zvec LOCK error may appear.
	for name, proxy := range map[string]*mcpSession{"first": first, "second": second} {
		out, err := callTool(ctx, proxy.session, "index_status", map[string]any{})
		if err != nil {
			return fmt.Errorf("%s proxy index_status: %w", name, err)
		}
		lower := strings.ToLower(out)
		if strings.Contains(lower, "lock") && strings.Contains(lower, "error") {
			return fmt.Errorf("%s proxy reports a lock error: %s", name, truncate(out, 400))
		}
		if strings.Contains(lower, "zvec_open_ok\": false") {
			return fmt.Errorf("%s proxy reports zvec not open: %s", name, truncate(out, 400))
		}
	}
	if first.pid == second.pid {
		return errors.New("the two proxies are the same process")
	}
	return nil
}

// checkHeadlessCLIWithLiveDaemon proves the headless entrypoint contract while a
// real daemon and two real MCP proxies are running: no arguments and --help print
// the help and exit 0, the removed --gui flag exits 2, and none of the three
// touches the daemon process, the project registrations or the collections.
func (e *env) checkHeadlessCLIWithLiveDaemon() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	proxyA, err := e.openProxy(ctx, e.projectA)
	if err != nil {
		return err
	}
	defer proxyA.close()
	proxyB, err := e.openProxy(ctx, e.projectB)
	if err != nil {
		return err
	}
	defer proxyB.close()

	pidBefore, err := e.daemonPID()
	if err != nil {
		return err
	}
	registrationsBefore, err := e.registrationsFingerprint()
	if err != nil {
		return err
	}

	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{name: "no args", args: nil, want: 0},
		{name: "--help", args: []string{"--help"}, want: 0},
		{name: "--gui", args: []string{"--gui"}, want: 2},
	} {
		code, out, runErr := e.runBinaryCode(tc.args...)
		if runErr != nil {
			return fmt.Errorf("%s: %w (%s)", tc.name, runErr, truncate(out, 300))
		}
		if code != tc.want {
			return fmt.Errorf("%s: exit=%d, want %d (%s)", tc.name, code, tc.want, truncate(out, 300))
		}
		if tc.want == 0 {
			for _, mode := range []string{"--stdio", "--stdio-proxy", "--http", "--daemon"} {
				if !strings.Contains(out, mode) {
					return fmt.Errorf("%s: help does not mention %s (%s)", tc.name, mode, truncate(out, 300))
				}
			}
		}
		if strings.Contains(out, "--gui") && tc.want == 0 {
			return fmt.Errorf("%s: help still advertises --gui (%s)", tc.name, truncate(out, 300))
		}
	}

	// The daemon survived and was not replaced by a second instance.
	pidAfter, err := e.daemonPID()
	if err != nil {
		return err
	}
	if pidBefore != pidAfter {
		return fmt.Errorf("daemon PID changed %d -> %d", pidBefore, pidAfter)
	}
	registrationsAfter, err := e.registrationsFingerprint()
	if err != nil {
		return err
	}
	if registrationsBefore != registrationsAfter {
		return fmt.Errorf("project registrations changed:\nbefore: %s\nafter:  %s", registrationsBefore, registrationsAfter)
	}

	// Both live proxies still answer through the daemon.
	for name, proxy := range map[string]*mcpSession{"A": proxyA, "B": proxyB} {
		out, err := callTool(ctx, proxy.session, "index_status", map[string]any{})
		if err != nil {
			return fmt.Errorf("proxy %s index_status: %w", name, err)
		}
		want := e.collection[name]
		if want == "" {
			want = map[string]string{"A": e.projectA, "B": e.projectB}[name]
		}
		if !strings.Contains(out, want) {
			return fmt.Errorf("proxy %s index_status is not bound to %s: %s", name, want, truncate(out, 300))
		}
	}
	return nil
}

// registrationsFingerprint renders the projects.d registrations as one sorted
// string, so any add/remove/rewrite is visible as a difference.
func (e *env) registrationsFingerprint() (string, error) {
	dir := filepath.Join(e.globalDir, "projects.d")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		body, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			return "", readErr
		}
		fmt.Fprintf(&b, "%s=%x;", name, sha256.Sum256(body))
	}
	return b.String(), nil
}

func (e *env) checkDuplicateIDRefused() error {
	copyRoot := filepath.Join(e.work, "project-a-copy")
	if err := copyTree(e.projectA, copyRoot); err != nil {
		return err
	}
	out, err := e.runBinary("--project-add", copyRoot)
	if err == nil {
		return fmt.Errorf("adding a copy with A's collection id succeeded:\n%s", out)
	}
	if !strings.Contains(out, "already registered") && !strings.Contains(out, "project-new-collection") {
		return fmt.Errorf("the refusal does not explain the conflict:\n%s", out)
	}
	// The original collection is untouched.
	if _, err := e.search(e.collection["A"], "ALPHA unique payload"); err != nil {
		return fmt.Errorf("original collection broke after the refused add: %w", err)
	}
	// The explicit fork works and leaves A alone.
	if out, err := e.runBinary("--project-new-collection", copyRoot); err != nil {
		return fmt.Errorf("project-new-collection: %w\n%s", err, out)
	}
	forkedID, err := projectCollectionID(copyRoot)
	if err != nil {
		return err
	}
	if forkedID == e.collection["A"] {
		return errors.New("the copy kept A's collection id")
	}
	e.collection["copy"] = forkedID
	return nil
}

func copyTree(from, to string) error {
	return filepath.Walk(from, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

// checkProfileChange switches the global profile dimensions and proves the daemon
// demands a rebuild before searching with the new vectors, without disturbing
// another collection. A second mock embeddings server with the new dimensions
// stands in for the changed model.
func (e *env) checkProfileChange() error {
	configPath := filepath.Join(e.globalDir, "config.yaml")
	original, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	// The model itself changes: a different endpoint that returns the new width.
	newDims := *dims * 2
	embedURL, err := startMockEmbeddings(e.ctx, newDims)
	if err != nil {
		return fmt.Errorf("start second mock embeddings: %w", err)
	}
	changed := string(original)
	changed = strings.Replace(changed, fmt.Sprintf("dimensions: %d", *dims), fmt.Sprintf("dimensions: %d", newDims), 1)
	changed = replaceBaseURL(changed, embedURL)
	if changed == string(original) {
		return errors.New("could not rewrite the profile")
	}
	if err := os.WriteFile(configPath, []byte(changed), 0o600); err != nil {
		return err
	}
	defer func() { _ = os.WriteFile(configPath, original, 0o600) }()

	// Global profile changes need a daemon restart (documented behaviour).
	if err := e.restartDaemon(); err != nil {
		return err
	}

	// The collection must not quietly search old vectors: either the status reports
	// an identity mismatch, or the search fails and asks for a rebuild.
	var status struct {
		IdentityMismatch    bool   `json:"identity_mismatch"`
		IdentityMismatchWhy string `json:"identity_mismatch_reason"`
		Message             string `json:"message"`
	}
	code, err := e.api.do(context.Background(), http.MethodGet,
		"/v1/status?workspace_id="+e.collection["A"], nil, &status)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("status HTTP %d", code)
	}
	if !status.IdentityMismatch {
		// A stale-vector search would be the bug this check exists for.
		if results, searchErr := e.search(e.collection["A"], "ALPHA unique payload"); searchErr == nil && len(results) > 0 {
			return fmt.Errorf("search succeeded after a dimensions change without a rebuild (%d results)", len(results))
		}
	}

	// After a forced rebuild the collection works again with the new model.
	var payload map[string]any
	if code, err := e.api.do(context.Background(), http.MethodPost, "/v1/reindex",
		map[string]any{"force": true, "workspace_id": e.collection["A"]}, &payload); err != nil || code != http.StatusOK {
		return fmt.Errorf("force reindex A: HTTP %d err=%v payload=%v", code, err, payload)
	}
	if err := e.waitIdle(e.collection["A"]); err != nil {
		return fmt.Errorf("A after rebuild: %w", err)
	}
	results, err := e.search(e.collection["A"], "ALPHA unique payload")
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return errors.New("A returned no results after the forced rebuild")
	}
	for _, res := range results {
		if !isUnder(e.projectA, resultAbs(e.projectA, res.Path)) {
			return fmt.Errorf("A returned %q after rebuild", res.Path)
		}
	}

	// B is a separate collection with old-dimension vectors: a global profile change
	// invalidates it too, and it must ask for its own rebuild rather than serve
	// stale results. Its data and registration are untouched.
	var statusB struct {
		IdentityMismatch bool `json:"identity_mismatch"`
	}
	if code, err := e.api.do(context.Background(), http.MethodGet,
		"/v1/status?workspace_id="+e.collection["B"], nil, &statusB); err != nil || code != http.StatusOK {
		return fmt.Errorf("B status: HTTP %d err=%v", code, err)
	}
	if !statusB.IdentityMismatch {
		if resultsB, searchErr := e.search(e.collection["B"], "BETA unique payload"); searchErr == nil && len(resultsB) > 0 {
			return errors.New("B served results from vectors of the old model after the profile change")
		}
	}
	// And it recovers with its own forced rebuild.
	var payloadB map[string]any
	if code, err := e.api.do(context.Background(), http.MethodPost, "/v1/reindex",
		map[string]any{"force": true, "workspace_id": e.collection["B"]}, &payloadB); err != nil || code != http.StatusOK {
		return fmt.Errorf("force reindex B: HTTP %d err=%v payload=%v", code, err, payloadB)
	}
	if err := e.waitIdle(e.collection["B"]); err != nil {
		return fmt.Errorf("B after rebuild: %w", err)
	}
	resultsB, err := e.search(e.collection["B"], "BETA unique payload")
	if err != nil {
		return fmt.Errorf("B after rebuild: %w", err)
	}
	if len(resultsB) == 0 {
		return errors.New("B returned no results after its own rebuild")
	}
	for _, res := range resultsB {
		abs := resultAbs(e.projectB, res.Path)
		if !isUnder(e.projectB, abs) {
			return fmt.Errorf("B returned %q after rebuild", res.Path)
		}
		if _, statErr := os.Stat(abs); statErr != nil {
			return fmt.Errorf("B returned %q which does not exist: %v", res.Path, statErr)
		}
	}
	return nil
}

// replaceBaseURL rewrites the profile base_url to a new mock endpoint.
func replaceBaseURL(configYAML, newBaseURL string) string {
	lines := strings.Split(configYAML, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(trimmed, "base_url:"); ok {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			_ = rest
			lines[i] = indent + "base_url: " + newBaseURL + "/v1"
		}
	}
	return strings.Join(lines, "\n")
}

// checkCyrillic proves a Cyrillic path works and that zvec stays inside the
// global collection directory rather than being relocated to a cache.
func (e *env) checkCyrillic() error {
	if err := writeFixture(e.cyrillic, "delta", "DELTA"); err != nil {
		return err
	}
	if out, err := e.runBinary("--project-add", e.cyrillic); err != nil {
		return fmt.Errorf("project-add cyrillic: %w\n%s", err, out)
	}
	id, err := projectCollectionID(e.cyrillic)
	if err != nil {
		return err
	}
	e.collection["cyrillic"] = id
	if out, err := e.runBinary("--projects-reload"); err != nil {
		return fmt.Errorf("reload: %w\n%s", err, out)
	}
	var payload map[string]any
	if code, err := e.api.do(context.Background(), http.MethodPost, "/v1/reindex",
		map[string]any{"force": true, "workspace_id": id}, &payload); err != nil || code != http.StatusOK {
		return fmt.Errorf("reindex cyrillic: HTTP %d err=%v payload=%v", code, err, payload)
	}
	if err := e.waitIdle(id); err != nil {
		return fmt.Errorf("cyrillic: %w", err)
	}
	results, err := e.search(id, "DELTA unique payload")
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return errors.New("cyrillic project returned no results")
	}
	for _, res := range results {
		if !isUnder(e.cyrillic, resultAbs(e.cyrillic, res.Path)) {
			return fmt.Errorf("cyrillic project returned %q", res.Path)
		}
	}
	// zvec must live under the global collection dir, not in a temp cache.
	collectionDir := filepath.Join(e.globalDir, "data", "collections", id)
	if info, err := os.Stat(collectionDir); err != nil || !info.IsDir() {
		return fmt.Errorf("collection dir %s missing: %v", collectionDir, err)
	}
	if info, err := os.Stat(filepath.Join(collectionDir, "zvec")); err != nil || !info.IsDir() {
		return fmt.Errorf("zvec dir missing under %s: %v", collectionDir, err)
	}
	return nil
}

// checkDaemonRestart kills the daemon and proves EnsureRunning brings it back and
// the collections still open, with no orphan watchers left behind.
func (e *env) checkDaemonRestart() error {
	pid, err := e.daemonPID()
	if err != nil {
		return err
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Kill(); err != nil {
		return fmt.Errorf("kill daemon %d: %w", pid, err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if err := e.checkHealth(); err != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if out, err := e.runBinary("--ensure-daemon"); err != nil {
		return fmt.Errorf("ensure-daemon after crash: %w\n%s", err, out)
	}
	if err := e.checkHealth(); err != nil {
		return fmt.Errorf("daemon did not come back: %w", err)
	}
	// Collections reopen and are still bound to their own project.
	for _, label := range []string{"A", "B"} {
		results, err := e.search(e.collection[label], map[string]string{"A": "ALPHA", "B": "BETA"}[label]+" unique payload")
		if err != nil {
			return fmt.Errorf("%s after restart: %w", label, err)
		}
		if len(results) == 0 {
			return fmt.Errorf("%s returned no results after restart", label)
		}
		root := map[string]string{"A": e.projectA, "B": e.projectB}[label]
		for _, res := range results {
			if !isUnder(root, resultAbs(root, res.Path)) {
				return fmt.Errorf("%s returned %q after restart", label, res.Path)
			}
		}
	}
	return nil
}

// restartDaemon stops the daemon through the admin API and starts it again.
func (e *env) restartDaemon() error {
	if out, err := e.runBinary("--stop-daemon"); err != nil {
		return fmt.Errorf("stop-daemon: %w\n%s", err, out)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if err := e.checkHealth(); err != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if out, err := e.runBinary("--ensure-daemon"); err != nil {
		return fmt.Errorf("ensure-daemon: %w\n%s", err, out)
	}
	return e.checkHealth()
}

// sortedKeys is used by diagnostics only.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
