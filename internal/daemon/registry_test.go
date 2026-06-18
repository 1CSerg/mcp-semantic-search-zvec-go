package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const smokeWorkspaceConfig = `active_profile: smoke
profiles:
  smoke:
    provider: openai_compatible
    model: mock
    base_url: http://127.0.0.1:9/v1
    dimensions: 128
file_watcher:
  enabled: false
`

func writeWorkspaceConfig(t *testing.T, root string) {
	t.Helper()
	install := filepath.Join(root, ".mcp-semantic-search-zvec-go")
	if err := os.MkdirAll(install, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(install, "config.yaml"), []byte(smokeWorkspaceConfig), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryOpenWorkspace(t *testing.T) {
	dir := t.TempDir()
	rootA := filepath.Join(dir, "a")
	rootB := filepath.Join(dir, "b")
	for _, root := range []string{rootA, rootB} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		writeWorkspaceConfig(t, root)
	}

	cfg := Config{
		MaxOpenWorkspaces: 1,
		Workspaces: []WorkspaceSpec{
			{ID: "ws-a", Root: rootA},
			{ID: "ws-b", Root: rootB},
		},
	}
	if err := normalizeConfig(&cfg); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry(cfg, t.Context())
	defer r.Close()

	if _, err := r.GetService("ws-a"); err != nil {
		t.Fatalf("open ws-a: %v", err)
	}
	list := r.ListWorkspaces(false)
	openCount := 0
	for _, ws := range list {
		if ws.Open {
			openCount++
		}
	}
	if openCount != 1 {
		t.Fatalf("open count=%d list=%+v", openCount, list)
	}

	if _, err := r.GetService("ws-b"); err != nil {
		t.Fatalf("open ws-b: %v", err)
	}
	list = r.ListWorkspaces(false)
	openCount = 0
	var openIDs []string
	for _, ws := range list {
		if ws.Open {
			openCount++
			openIDs = append(openIDs, ws.ID)
		}
	}
	if openCount != 1 {
		t.Fatalf("expected LRU to keep one open, got %d (%v)", openCount, openIDs)
	}
}

func TestRegistryListWorkspacesIncludePaths(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Workspaces: []WorkspaceSpec{{ID: "ws1", Root: root}},
	}
	if err := normalizeConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(cfg, t.Context())
	defer r.Close()

	summary := r.ListWorkspaces(false)
	if len(summary) != 1 {
		t.Fatalf("summary len=%d", len(summary))
	}
	if summary[0].Root != "" || summary[0].IndexDir != "" || summary[0].ConfigPath != "" {
		t.Fatalf("summary should omit paths: %+v", summary[0])
	}

	full := r.ListWorkspaces(true)
	if len(full) != 1 {
		t.Fatalf("full len=%d", len(full))
	}
	if full[0].Root == "" || full[0].IndexDir == "" || full[0].ConfigPath == "" {
		t.Fatalf("full should include paths: %+v", full[0])
	}
}

func TestRegistryGetServiceRequiresID(t *testing.T) {
	r := NewRegistry(Config{Workspaces: []WorkspaceSpec{{ID: "x", Root: t.TempDir()}}}, t.Context())
	defer r.Close()
	if _, err := r.GetService(""); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("err=%v", err)
	}
}

func TestNewRegistryNilContext(t *testing.T) {
	r := NewRegistry(Config{Workspaces: []WorkspaceSpec{{ID: "x", Root: t.TempDir()}}}, nil)
	defer r.Close()
	if r.rootCtx == nil {
		t.Fatal("expected non-nil root context")
	}
}

func TestRegistryCloseAll(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceConfig(t, root)
	cfg := Config{Workspaces: []WorkspaceSpec{{ID: "ws", Root: root}}}
	if err := normalizeConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(cfg, t.Context())
	if _, err := r.GetService("ws"); err != nil {
		t.Fatal(err)
	}
	r.Close()
	list := r.ListWorkspaces(false)
	for _, ws := range list {
		if ws.Open {
			t.Fatalf("workspace still open after Close: %+v", ws)
		}
	}
}

func TestRegistryCloseWaitsForBorrowRelease(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceConfig(t, root)
	cfg := Config{Workspaces: []WorkspaceSpec{{ID: "ws", Root: root}}}
	if err := normalizeConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(cfg, t.Context())

	_, release, err := r.BorrowService("ws")
	if err != nil {
		t.Fatal(err)
	}

	closed := make(chan struct{})
	go func() {
		r.Close()
		close(closed)
	}()

	select {
	case <-closed:
		t.Fatal("Close returned before borrow release")
	case <-time.After(100 * time.Millisecond):
	}

	release()

	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not finish after borrow release")
	}
}

func TestRegistryConcurrentBorrowNoFailedOpen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceConfig(t, root)
	cfg := Config{
		MaxOpenWorkspaces: 1,
		Workspaces:        []WorkspaceSpec{{ID: "ws-a", Root: root}},
	}
	if err := normalizeConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(cfg, t.Context())
	defer r.Close()

	errs := make(chan error, 30)
	for i := 0; i < 30; i++ {
		go func() {
			_, err := r.GetService("ws-a")
			errs <- err
		}()
	}
	for i := 0; i < 30; i++ {
		if err := <-errs; err != nil {
			if strings.Contains(err.Error(), "failed to open") {
				t.Fatalf("unexpected open race: %v", err)
			}
			t.Fatalf("GetService: %v", err)
		}
	}
}

func TestRegistryConcurrentColdOpenBorrowRefs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceConfig(t, root)
	cfg := Config{
		MaxOpenWorkspaces: 1,
		Workspaces:        []WorkspaceSpec{{ID: "ws-a", Root: root}},
	}
	if err := normalizeConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(cfg, t.Context())
	defer r.Close()

	const n = 10
	start := make(chan struct{})
	var wg sync.WaitGroup
	borrowed := make(chan struct{}, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, release, err := r.BorrowService("ws-a")
			if err != nil {
				t.Errorf("BorrowService: %v", err)
				return
			}
			borrowed <- struct{}{}
			time.Sleep(200 * time.Millisecond)
			release()
		}()
	}

	close(start)

	for i := 0; i < n; i++ {
		select {
		case <-borrowed:
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout waiting for borrow %d/%d", i, n)
		}
	}

	r.mu.Lock()
	h := r.open["ws-a"]
	refs := 0
	if h != nil {
		refs = h.refs
	}
	r.mu.Unlock()
	if refs != n {
		t.Fatalf("refs=%d want %d while %d borrows held", refs, n, n)
	}

	wg.Wait()
}

func TestRegistryConcurrentOpenDifferentWorkspaces(t *testing.T) {
	dir := t.TempDir()
	rootA := filepath.Join(dir, "a")
	rootB := filepath.Join(dir, "b")
	for _, root := range []string{rootA, rootB} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		writeWorkspaceConfig(t, root)
	}

	cfg := Config{
		MaxOpenWorkspaces: 1,
		Workspaces: []WorkspaceSpec{
			{ID: "ws-a", Root: rootA},
			{ID: "ws-b", Root: rootB},
		},
	}
	if err := normalizeConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(cfg, t.Context())
	defer r.Close()

	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, id := range []string{"ws-a", "ws-b"} {
		id := id
		go func() {
			<-start
			_, release, err := r.BorrowService(id)
			if err == nil {
				release()
			}
			errs <- err
		}()
	}
	close(start)

	for i := 0; i < 2; i++ {
		err := <-errs
		if err == nil {
			continue
		}
		if strings.Contains(err.Error(), "failed to open") {
			t.Fatalf("unexpected open race: %v", err)
		}
		if !strings.Contains(err.Error(), "max open workspaces") {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestRegistryCloseRootCtxCancelWaitsForBorrow(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceConfig(t, root)
	cfg := Config{Workspaces: []WorkspaceSpec{{ID: "ws", Root: root}}}
	if err := normalizeConfig(&cfg); err != nil {
		t.Fatal(err)
	}

	rootCtx, cancelRoot := context.WithCancel(context.Background())
	r := NewRegistry(cfg, rootCtx)

	_, release, err := r.BorrowService("ws")
	if err != nil {
		t.Fatal(err)
	}

	closed := make(chan struct{})
	go func() {
		cancelRoot()
		r.Close()
		close(closed)
	}()

	select {
	case <-closed:
		t.Fatal("Close returned before borrow release despite root cancel")
	case <-time.After(100 * time.Millisecond):
	}

	release()

	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not finish after borrow release")
	}
}

func TestRegistryCloseWaitsForColdOpen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceConfig(t, root)
	cfg := Config{Workspaces: []WorkspaceSpec{{ID: "ws", Root: root}}}
	if err := normalizeConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(cfg, t.Context())

	opening := make(chan struct{})
	go func() {
		_, release, err := r.BorrowService("ws")
		if err == nil {
			release()
		}
		close(opening)
	}()

	select {
	case <-opening:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for cold open")
	}

	closed := make(chan struct{})
	go func() {
		r.Close()
		close(closed)
	}()

	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close blocked while cold-open should have finished")
	}

	list := r.ListWorkspaces(false)
	for _, ws := range list {
		if ws.Open {
			t.Fatalf("workspace still open after Close during cold-open: %+v", ws)
		}
	}
}

func TestRegistryCloseDuringColdOpenNoOrphan(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceConfig(t, root)
	cfg := Config{Workspaces: []WorkspaceSpec{{ID: "ws", Root: root}}}
	if err := normalizeConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(cfg, t.Context())

	borrowDone := make(chan struct{})
	go func() {
		defer close(borrowDone)
		_, release, err := r.BorrowService("ws")
		if release != nil {
			release()
		}
		if err != nil && !errors.Is(err, ErrRegistryClosing) && !strings.Contains(err.Error(), "context canceled") {
			t.Errorf("unexpected borrow error after Close: %v", err)
		}
	}()

	r.Close()

	select {
	case <-borrowDone:
	case <-time.After(5 * time.Second):
		t.Fatal("cold open did not finish after Close")
	}

	r.mu.Lock()
	openCount := len(r.open)
	openingCount := len(r.opening)
	r.mu.Unlock()
	if openCount != 0 || openingCount != 0 {
		t.Fatalf("orphan handles after Close: open=%d opening=%d", openCount, openingCount)
	}
}

func TestRegistryCloseSkipsShutdownWithHeldBorrow(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceConfig(t, root)
	cfg := Config{Workspaces: []WorkspaceSpec{{ID: "ws", Root: root}}}
	if err := normalizeConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(cfg, t.Context())
	r.closeDrainTimeout = 100 * time.Millisecond

	_, release, err := r.BorrowService("ws")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		r.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close blocked longer than drain timeout")
	}

	r.mu.Lock()
	h := r.open["ws"]
	refs := 0
	if h != nil {
		refs = h.refs
	}
	r.mu.Unlock()
	if refs != 1 {
		t.Fatalf("refs=%d want 1 after Close with held borrow", refs)
	}
	release()
}

func TestRegistryReleaseDoubleRelease(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceConfig(t, root)
	cfg := Config{Workspaces: []WorkspaceSpec{{ID: "ws", Root: root}}}
	if err := normalizeConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(cfg, t.Context())
	defer r.Close()

	_, release, err := r.BorrowService("ws")
	if err != nil {
		t.Fatal(err)
	}
	release()
	release()

	r.mu.Lock()
	refs := r.open["ws"].refs
	r.mu.Unlock()
	if refs != 0 {
		t.Fatalf("refs=%d want 0 after double release", refs)
	}
}

func TestRegistryCloseHandleLockedHelpers(t *testing.T) {
	r := NewRegistry(Config{}, t.Context())
	defer r.Close()

	r.closeHandleLocked("missing")

	r.mu.Lock()
	r.open["ws"] = &workspaceHandle{refs: 1}
	r.mu.Unlock()
	r.closeHandleLocked("ws")
	r.mu.Lock()
	if _, ok := r.open["ws"]; !ok {
		r.mu.Unlock()
		t.Fatal("handle should remain while refs > 0")
	}
	r.mu.Unlock()

	r.discardHandle(nil)
}
