package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/fsnotify/fsnotify"
)

func TestAddWatchTree(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := addWatchTree(w, root, []string{".git"}); err != nil {
		t.Fatal(err)
	}
}

func TestFSNotifyBackendRun(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	settings := &config.Settings{
		WorkspaceRoot: root,
		App: config.AppConfig{
			Indexing: config.IndexingConfig{
				Extensions: []string{".go"},
				SkipDirs:   []string{".git"},
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan string, 4)
	go func() {
		_ = newFSNotifyBackend().run(ctx, settings, events, nil)
	}()

	time.Sleep(150 * time.Millisecond)
	if err := os.WriteFile(file, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case rel := <-events:
		if rel != "main.go" {
			t.Fatalf("rel=%q", rel)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for fsnotify event")
	}
}

func TestFSNotifyBackendContextCancel(t *testing.T) {
	root := t.TempDir()
	settings := &config.Settings{WorkspaceRoot: root}
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		_ = newFSNotifyBackend().run(ctx, settings, events, nil)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("run did not exit after context cancel")
	}
}

func TestFSNotifyBackendDetectsNewDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	settings := &config.Settings{
		WorkspaceRoot: root,
		App: config.AppConfig{
			Indexing: config.IndexingConfig{
				Extensions: []string{".go"},
				SkipDirs:   []string{".git"},
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan string, 4)
	go func() {
		_ = newFSNotifyBackend().run(ctx, settings, events, nil)
	}()

	time.Sleep(150 * time.Millisecond)
	newDir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	newFile := filepath.Join(newDir, "util.go")
	if err := os.WriteFile(newFile, []byte("package pkg\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case rel := <-events:
		if rel != "pkg/util.go" && rel != "pkg" {
			t.Fatalf("rel=%q", rel)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for fsnotify create event")
	}
}

func TestNewWatcherNilCoordinator(t *testing.T) {
	settings := &config.Settings{
		App: config.AppConfig{
			FileWatcher: config.FileWatcherConfig{Enabled: true},
		},
	}
	w, err := New(settings, nil)
	if err != nil || w != nil {
		t.Fatalf("w=%v err=%v", w, err)
	}
}
