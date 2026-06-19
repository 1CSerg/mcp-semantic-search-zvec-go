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
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	daemonMockPort = 19990
	daemonHTTPPort = 19400
)

// DaemonWorkspace describes one workspace entry in daemon.yaml.
type DaemonWorkspace struct {
	ID         string
	Root       string
	IndexDir   string
	ConfigPath string
	Keyword    string
}

// DaemonSetup holds paths and process for a shared daemon test run.
type DaemonSetup struct {
	RootDir    string
	DaemonYAML string
	HTTPBase   string
	Port       int
	Cmd        *exec.Cmd
	Workspaces []DaemonWorkspace
}

// PrepareDaemonWorkspaces creates temp workspaces with mock-embed config (T4/T5/A1).
func PrepareDaemonWorkspaces(t *testing.T, repo string, mockPort int) *DaemonSetup {
	t.Helper()
	root := filepath.Join(RealworldRoot(repo), "tmp-daemon")
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("clean daemon root: %v", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir daemon root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	cfgTemplate := ConfigTemplate(repo, "daemon-workspace")
	cfgData, err := os.ReadFile(cfgTemplate)
	if err != nil {
		t.Fatalf("read daemon-workspace config: %v", err)
	}
	cfgText := strings.ReplaceAll(string(cfgData), "MOCK_PORT", fmt.Sprintf("%d", mockPort))

	specs := []struct {
		id, keyword string
	}{
		{"rw-ws-alpha", "REALWORLD_GO_AUTH_GATE"},
		{"rw-ws-beta", "REALWORLD_PY_HANDLER"},
		{"rw-ws-gamma", "REALWORLD_JS_UTIL"},
	}
	var workspaces []DaemonWorkspace
	for _, spec := range specs {
		wsRoot := filepath.Join(root, spec.id)
		install := filepath.Join(wsRoot, ".mcp-semantic-search-zvec-go")
		idxDir := filepath.Join(install, "data", "index")
		if err := os.MkdirAll(idxDir, 0o755); err != nil {
			t.Fatalf("mkdir index: %v", err)
		}
		// Symlink/copy corpus into workspace root for search markers.
		corpusLink := filepath.Join(wsRoot, "corpus")
		_ = os.RemoveAll(corpusLink)
		if err := copyDir(CorpusDir(repo), corpusLink); err != nil {
			t.Fatalf("copy corpus to %s: %v", spec.id, err)
		}
		cfgPath := filepath.Join(install, "config.yaml")
		if err := os.WriteFile(cfgPath, []byte(cfgText), 0o644); err != nil {
			t.Fatalf("write workspace config: %v", err)
		}
		workspaces = append(workspaces, DaemonWorkspace{
			ID:         spec.id,
			Root:       wsRoot,
			IndexDir:   idxDir,
			ConfigPath: cfgPath,
			Keyword:    spec.keyword,
		})
	}

	daemonYAML := filepath.Join(root, "daemon.yaml")
	var b strings.Builder
	b.WriteString("max_open_workspaces: 2\nworkspaces:\n")
	for _, ws := range workspaces {
		fmt.Fprintf(&b, "  - id: %s\n    root: %s\n    index_dir: %s\n    config_path: %s\n",
			ws.ID, filepath.ToSlash(ws.Root), filepath.ToSlash(ws.IndexDir), filepath.ToSlash(ws.ConfigPath))
	}
	if err := os.WriteFile(daemonYAML, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write daemon.yaml: %v", err)
	}

	return &DaemonSetup{
		RootDir:    root,
		DaemonYAML: daemonYAML,
		Port:       daemonHTTPPort,
		Workspaces: workspaces,
	}
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// StartDaemon spawns --daemon and waits for /health.
func StartDaemon(t *testing.T, repo string, setup *DaemonSetup, apiToken string) *DaemonSetup {
	t.Helper()
	bin := BinPath(repo)
	binDir := BinDir(repo)
	addr := fmt.Sprintf("127.0.0.1:%d", setup.Port)
	args := []string{"--daemon", "--daemon-config", setup.DaemonYAML, "--http-addr", addr}
	cmd := exec.Command(bin, args...)
	env := prependBinToPath(BaseEnv(repo), binDir)
	if apiToken != "" {
		env = append(env, "API_TOKEN="+apiToken)
	}
	cmd.Env = env
	cmd.Dir = binDir
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	setup.Cmd = cmd
	setup.HTTPBase = "http://" + addr
	t.Cleanup(func() { killProcess(t, cmd) })
	waitHTTPHealth(t, setup.HTTPBase)
	return setup
}

// ReindexDaemonWorkspace triggers force reindex for a daemon workspace.
func ReindexDaemonWorkspace(t *testing.T, setup *DaemonSetup, wsID, bearer string) {
	t.Helper()
	body := map[string]any{"force": true, "workspace_id": wsID}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, setup.HTTPBase+"/v1/reindex", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Workspace-ID", wsID)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("daemon reindex %s: %v", wsID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("daemon reindex %s: status=%d body=%s", wsID, resp.StatusCode, data)
	}
}

// DaemonOpenWorkspaces returns workspace id → open flag from GET /v1/workspaces.
func DaemonOpenWorkspaces(t *testing.T, setup *DaemonSetup) map[string]bool {
	t.Helper()
	workspaces := GetJSON(t, setup.HTTPBase+"/v1/workspaces")
	list, _ := workspaces["workspaces"].([]any)
	open := make(map[string]bool, len(list))
	for _, item := range list {
		ws, _ := item.(map[string]any)
		if ws == nil {
			continue
		}
		id, _ := ws["id"].(string)
		o, _ := ws["open"].(bool)
		open[id] = o
	}
	return open
}

// TouchDaemonWorkspace opens a workspace handle via status (no path redaction check).
func TouchDaemonWorkspace(t *testing.T, setup *DaemonSetup, wsID, bearer string) map[string]any {
	t.Helper()
	return WaitDaemonIndexIdle(t, setup, wsID, bearer)
}

// WaitDaemonIndexIdle waits until workspace indexing is idle.
func WaitDaemonIndexIdle(t *testing.T, setup *DaemonSetup, wsID, bearer string) map[string]any {
	t.Helper()
	url := setup.HTTPBase + "/v1/status?workspace_id=" + wsID
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		status, code := GetJSONAuth(t, url, bearer)
		if code >= 400 {
			time.Sleep(800 * time.Millisecond)
			continue
		}
		idx, _ := status["indexing"].(map[string]any)
		if idx != nil && idx["state"] == "error" {
			t.Fatalf("indexing error for %s: %v", wsID, idx["message"])
		}
		if idx != nil {
			if running, _ := idx["running"].(bool); !running {
				return status
			}
		}
		time.Sleep(800 * time.Millisecond)
	}
	t.Fatalf("daemon indexing did not idle for %s", wsID)
	return nil
}

// StartMCPProxy spawns --stdio-proxy connected to a shared daemon.
func StartMCPProxy(t *testing.T, repo, workspaceID, daemonURL, apiToken string) *mcp.ClientSession {
	t.Helper()
	bin := BinPath(repo)
	binDir := BinDir(repo)
	args := []string{
		"--stdio-proxy",
		"--workspace-id=" + workspaceID,
		"--daemon-url=" + daemonURL,
	}
	cmd := exec.Command(bin, args...)
	env := prependBinToPath(BaseEnv(repo), binDir)
	if apiToken != "" {
		env = append(env, "API_TOKEN="+apiToken)
	}
	cmd.Env = env
	cmd.Dir = binDir
	t.Cleanup(func() { killProcess(t, cmd) })

	client := mcp.NewClient(&mcp.Implementation{Name: "realworld-proxy", Version: "0"}, nil)
	transport := &mcp.CommandTransport{Command: cmd}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("mcp proxy connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// StartDaemonWithMock starts mock embed + daemon with three workspaces.
func StartDaemonWithMock(t *testing.T, repo string, apiToken string) (*DaemonSetup, *exec.Cmd) {
	t.Helper()
	StartMockEmbed(t, repo, daemonMockPort, 128, false)
	setup := PrepareDaemonWorkspaces(t, repo, daemonMockPort)
	setup = StartDaemon(t, repo, setup, apiToken)
	return setup, nil
}
