package zvec

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPromoteAndDiscardStagingCollection(t *testing.T) {
	dir := tempDir(t)
	cfg := Config{IndexDir: dir, WorkspaceRoot: filepath.Join(dir, "ws"), ProfileName: "test", Dimensions: 4}
	active := CollectionPath(cfg)
	stagingCfg := cfg
	stagingCfg.CollectionSuffix = StagingCollectionSuffix
	staging := CollectionPath(stagingCfg)

	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(active, "active.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "staging.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := PromoteStagingCollection(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(active, "staging.txt")); err != nil {
		t.Fatalf("promoted content missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(active, "active.txt")); !os.IsNotExist(err) {
		t.Fatal("old active content should be gone after promote")
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatal("staging path should be gone after promote")
	}

	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := DiscardStagingCollection(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatal("staging path should be gone after discard")
	}
}

func TestPromoteAndDiscardStagingManifest(t *testing.T) {
	dir := tempDir(t)
	active := filepath.Join(dir, "manifest.db")
	staging := StagingManifestPath(dir)
	if err := os.WriteFile(active, []byte("old-manifest"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staging, []byte("new-manifest"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PromoteStagingManifest(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(active)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new-manifest" {
		t.Fatalf("active=%q", data)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatal("staging manifest should be gone")
	}
	if err := os.WriteFile(staging, []byte("tmp"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := DiscardStagingManifest(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatal("discard should remove staging manifest")
	}
}

// stagingOpenProbe records which open path openStaging used.
type stagingOpenProbe struct {
	Store        // embedded interface: unused methods are not exercised here
	writable     bool
	writableErr  error
	readOnlyCall int
}

func (p *stagingOpenProbe) OpenWritable() error {
	p.writable = true
	return p.writableErr
}

func (p *stagingOpenProbe) Open() error {
	p.readOnlyCall++
	return nil
}

func TestOpenStagingUsesWritableOpen(t *testing.T) {
	probe := &stagingOpenProbe{}
	cfg := Config{IndexDir: tempDir(t), WorkspaceRoot: "/proj", ProfileName: "test", Dimensions: 4}

	got, err := openStaging(cfg, func(Config) Store { return probe })
	if err != nil {
		t.Fatalf("openStaging: %v", err)
	}
	if got != probe {
		t.Fatalf("openStaging returned %T, want the created store", got)
	}
	if !probe.writable {
		t.Fatal("openStaging must open the staging collection writable (read-only Open fails on a clean index)")
	}
	if probe.readOnlyCall != 0 {
		t.Fatalf("read-only Open called %d times, want 0", probe.readOnlyCall)
	}
}

func TestOpenStagingPropagatesWritableError(t *testing.T) {
	wantErr := errors.New("staging open failed")
	probe := &stagingOpenProbe{writableErr: wantErr}

	if _, err := openStaging(Config{IndexDir: tempDir(t)}, func(Config) Store { return probe }); !errors.Is(err, wantErr) {
		t.Fatalf("err=%v want %v", err, wantErr)
	}
	if probe.readOnlyCall != 0 {
		t.Fatal("must not fall back to read-only Open when writable open fails")
	}
}

func TestOpenStagingFallsBackToReadOnlyOpen(t *testing.T) {
	wantErr := errors.New("read-only open failed")
	probe := &readOnlyOnlyProbe{openErr: wantErr}

	if _, err := openStaging(Config{IndexDir: tempDir(t)}, func(Config) Store { return probe }); !errors.Is(err, wantErr) {
		t.Fatalf("err=%v want %v", err, wantErr)
	}
	if probe.readOnlyCall != 1 {
		t.Fatalf("read-only Open called %d times, want 1", probe.readOnlyCall)
	}
}

// readOnlyOnlyProbe has no OpenWritable, so openStaging must fall back to Open.
type readOnlyOnlyProbe struct {
	Store        // embedded interface: unused methods are not exercised here
	openErr      error
	readOnlyCall int
}

func (p *readOnlyOnlyProbe) Open() error {
	p.readOnlyCall++
	return p.openErr
}
