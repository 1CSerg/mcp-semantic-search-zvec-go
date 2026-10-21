package daemon

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/testutil"
)

var uuidV4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

const testGlobalConfig = `active_profile: test
profiles:
  test:
    provider: openai_compatible
    model: mock
    base_url: http://127.0.0.1:9/v1
    dimensions: 8
file_watcher:
  enabled: false
`

// newTestManager returns a ProjectManager rooted at a temporary user dir with
// MCP_GLOBAL_DIR pointed at it, so collection/config path helpers resolve there.
func newTestManager(t *testing.T) *ProjectManager {
	t.Helper()
	userDir := t.TempDir()
	// Registry writes leave files in projects.d; on Windows the parent removal can
	// briefly fail with ERROR_DIR_NOT_EMPTY (pending-delete bookkeeping).
	t.Cleanup(func() { testutil.RemoveDirWithRetry(t, userDir) })
	t.Setenv("MCP_GLOBAL_DIR", userDir)
	if err := os.WriteFile(filepath.Join(userDir, "config.yaml"), []byte(testGlobalConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewProjectManager(userDir)
}

func makeProjectRoot(t *testing.T, base, name string) string {
	t.Helper()
	root := filepath.Join(base, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func (m *ProjectManager) listIDsForTest(t *testing.T) map[string]string {
	t.Helper()
	regs, err := m.loadRegistrations()
	if err != nil {
		t.Fatalf("load registrations: %v", err)
	}
	out := make(map[string]string, len(regs))
	for _, reg := range regs {
		out[reg.ID] = reg.Root
	}
	return out
}

func TestAddProjectGeneratesUUIDAndIsIdempotent(t *testing.T) {
	m := newTestManager(t)
	root := makeProjectRoot(t, t.TempDir(), "my-app")

	pc, err := m.AddProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if !uuidV4Pattern.MatchString(pc.CollectionID) {
		t.Fatalf("collection_id %q is not a UUID v4", pc.CollectionID)
	}
	if pc.SchemaVersion != config.ProjectSchemaVersion {
		t.Fatalf("schema_version=%d", pc.SchemaVersion)
	}

	// project.yaml is written under the project and stays portable.
	cfgPath := config.ProjectConfigPath(root)
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), root) {
		t.Fatalf("project.yaml must not contain the absolute root:\n%s", data)
	}

	// Adding twice is idempotent: same id, one registration.
	again, err := m.AddProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if again.CollectionID != pc.CollectionID {
		t.Fatalf("id changed on re-add: %q -> %q", pc.CollectionID, again.CollectionID)
	}
	if ids := m.listIDsForTest(t); len(ids) != 1 {
		t.Fatalf("registrations=%v", ids)
	}
}

func TestAddProjectSameBasenameDifferentIDs(t *testing.T) {
	m := newTestManager(t)
	base := t.TempDir()
	rootA := makeProjectRoot(t, base, filepath.Join("a", "app"))
	rootB := makeProjectRoot(t, base, filepath.Join("b", "app"))

	pcA, err := m.AddProject(rootA)
	if err != nil {
		t.Fatal(err)
	}
	pcB, err := m.AddProject(rootB)
	if err != nil {
		t.Fatal(err)
	}
	if pcA.CollectionID == pcB.CollectionID {
		t.Fatalf("two roots with the same basename must get different ids: %q", pcA.CollectionID)
	}
	if ids := m.listIDsForTest(t); len(ids) != 2 {
		t.Fatalf("registrations=%v", ids)
	}
}

// TestProjectRegistrationCollision covers the two dangerous collisions: the same
// id pointing at another root, and ids that differ only by case.
func TestProjectRegistrationCollision(t *testing.T) {
	m := newTestManager(t)
	base := t.TempDir()
	rootA := makeProjectRoot(t, base, "app-a")
	rootB := makeProjectRoot(t, base, "app-b")

	pcA, err := m.AddProject(rootA)
	if err != nil {
		t.Fatal(err)
	}
	beforeA, err := os.ReadFile(config.ProjectConfigPath(rootA))
	if err != nil {
		t.Fatal(err)
	}
	beforeRegA, err := os.ReadFile(m.registrationPath(pcA.CollectionID))
	if err != nil {
		t.Fatal(err)
	}

	// Same id, different root: refuse without touching either file.
	writeProjectConfigBody(t, rootB, pcA.CollectionID)
	beforeB, err := os.ReadFile(config.ProjectConfigPath(rootB))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddProject(rootB); err == nil {
		t.Fatal("expected a conflict for the same collection_id on another root")
	}
	assertUnchanged(t, config.ProjectConfigPath(rootA), beforeA)
	assertUnchanged(t, m.registrationPath(pcA.CollectionID), beforeRegA)
	assertUnchanged(t, config.ProjectConfigPath(rootB), beforeB)
	if ids := m.listIDsForTest(t); len(ids) != 1 {
		t.Fatalf("registrations changed: %v", ids)
	}

	// Case-only id collision is the same conflict.
	rootC := makeProjectRoot(t, base, "app-c")
	writeProjectConfigBody(t, rootC, strings.ToUpper(pcA.CollectionID))
	if _, err := m.AddProject(rootC); err == nil {
		t.Fatal("expected a case-insensitive collection_id conflict")
	}
	if ids := m.listIDsForTest(t); len(ids) != 1 {
		t.Fatalf("registrations changed: %v", ids)
	}
}

// TestAddProjectSameRootDifferentID refuses a silent re-identification of an
// already registered root.
func TestAddProjectSameRootDifferentID(t *testing.T) {
	m := newTestManager(t)
	root := makeProjectRoot(t, t.TempDir(), "app")

	pc, err := m.AddProject(root)
	if err != nil {
		t.Fatal(err)
	}
	regBefore, err := os.ReadFile(m.registrationPath(pc.CollectionID))
	if err != nil {
		t.Fatal(err)
	}

	// Someone hand-edits project.yaml to a different id on a registered root.
	writeProjectConfigBody(t, root, "11111111-2222-4333-8444-555555555555")
	if _, err := m.AddProject(root); err == nil {
		t.Fatal("expected refusal for the same root with a different collection_id")
	}
	assertUnchanged(t, m.registrationPath(pc.CollectionID), regBefore)
}

// TestProjectNewCollection forks a copied project: the copy gets a new id and
// the original registration and data are untouched.
func TestProjectNewCollection(t *testing.T) {
	m := newTestManager(t)
	base := t.TempDir()
	original := makeProjectRoot(t, base, "original")

	pc, err := m.AddProject(original)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a filesystem copy of the project, including project.yaml.
	copied := makeProjectRoot(t, base, "copied")
	copyFile(t, config.ProjectConfigPath(original), config.ProjectConfigPath(copied))
	// Give the copy an override to prove settings survive the re-identification.
	writeProjectConfigBody(t, copied, "collection_id: "+pc.CollectionID+"\nindexing:\n  skip_dirs: [vendor]\n")

	regBefore, err := os.ReadFile(m.registrationPath(pc.CollectionID))
	if err != nil {
		t.Fatal(err)
	}
	collectionDir, err := config.CollectionDir(pc.CollectionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(collectionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(collectionDir, "marker")
	if err := os.WriteFile(marker, []byte("original data"), 0o600); err != nil {
		t.Fatal(err)
	}

	forked, err := m.NewProjectCollection(copied)
	if err != nil {
		t.Fatal(err)
	}
	if forked.CollectionID == pc.CollectionID {
		t.Fatal("the copy must get a new collection_id")
	}
	if !uuidV4Pattern.MatchString(forked.CollectionID) {
		t.Fatalf("forked id %q is not a UUID v4", forked.CollectionID)
	}
	if forked.Indexing == nil || len(forked.Indexing.SkipDirs) != 1 {
		t.Fatalf("copy settings lost: %+v", forked.Indexing)
	}

	// Original registration, config and data untouched.
	assertUnchanged(t, m.registrationPath(pc.CollectionID), regBefore)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("original collection data must survive: %v", err)
	}
	originalPC, err := config.LoadProjectConfig(config.ProjectConfigPath(original))
	if err != nil {
		t.Fatal(err)
	}
	if originalPC.CollectionID != pc.CollectionID {
		t.Fatalf("original project.yaml changed: %q", originalPC.CollectionID)
	}
	ids := m.listIDsForTest(t)
	if len(ids) != 2 {
		t.Fatalf("registrations=%v", ids)
	}
	if ids[forked.CollectionID] == "" {
		t.Fatalf("fork registration missing: %v", ids)
	}
}

// TestProjectCanonicalPaths checks that cosmetic path differences and symlink
// aliases resolve to the same project instead of creating duplicates.
func TestProjectCanonicalPaths(t *testing.T) {
	m := newTestManager(t)
	base := t.TempDir()
	root := makeProjectRoot(t, base, "Проект с пробелами")

	pc, err := m.AddProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	variants := []string{
		root,
		root + string(filepath.Separator),
		filepath.Join(root, ".", ""),
		filepath.Join(root, "sub", ".."),
	}
	for _, variant := range variants {
		again, err := m.AddProject(variant)
		if err != nil {
			t.Fatalf("AddProject(%q): %v", variant, err)
		}
		if again.CollectionID != pc.CollectionID {
			t.Fatalf("AddProject(%q) got id %q want %q", variant, again.CollectionID, pc.CollectionID)
		}
	}
	if ids := m.listIDsForTest(t); len(ids) != 1 {
		t.Fatalf("path variants created duplicates: %v", ids)
	}

	if runtime.GOOS == "windows" {
		upper := strings.ToUpper(root)
		again, err := m.AddProject(upper)
		if err != nil {
			t.Fatalf("AddProject(%q): %v", upper, err)
		}
		if again.CollectionID != pc.CollectionID {
			t.Fatalf("case variant got id %q want %q", again.CollectionID, pc.CollectionID)
		}
		if ids := m.listIDsForTest(t); len(ids) != 1 {
			t.Fatalf("case variant created a duplicate: %v", ids)
		}
	}

	// A symlink alias of the same root must not become a second project.
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(root, alias); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Fatal(err)
	}
	again, err := m.AddProject(alias)
	if err != nil {
		t.Fatalf("AddProject(symlink alias): %v", err)
	}
	if again.CollectionID != pc.CollectionID {
		t.Fatalf("symlink alias got id %q want %q", again.CollectionID, pc.CollectionID)
	}
	if ids := m.listIDsForTest(t); len(ids) != 1 {
		t.Fatalf("symlink alias created a duplicate: %v", ids)
	}
}

// TestProjectMalformedRegistration covers interrupted writes and corrupt YAML:
// the registry must stay readable and a broken file must never be treated as a
// successful (empty) registration.
func TestProjectMalformedRegistration(t *testing.T) {
	m := newTestManager(t)
	root := makeProjectRoot(t, t.TempDir(), "app")

	if err := os.MkdirAll(m.projectsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(m.projectsDir(), "broken.yaml")
	if err := os.WriteFile(bad, []byte("id: [unclosed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddProject(root); err == nil {
		t.Fatal("a malformed registration must fail the read-modify-write, not be ignored")
	}
	if _, err := m.loadRegistrations(); err == nil {
		t.Fatal("loadRegistrations must report the malformed file")
	}

	// An empty file is not a valid registration either.
	if err := os.WriteFile(bad, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.loadRegistrations(); err == nil {
		t.Fatal("an empty registration file must be an error")
	}

	// A malformed project.yaml must not be silently replaced by a new identity.
	if err := os.Remove(bad); err != nil {
		t.Fatal(err)
	}
	cfgPath := config.ProjectConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("collection_id: [unclosed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddProject(root); err == nil {
		t.Fatal("a malformed project.yaml must not be regenerated silently")
	}
	assertUnchanged(t, cfgPath, []byte("collection_id: [unclosed\n"))
	if ids := m.listIDsForTest(t); len(ids) != 0 {
		t.Fatalf("no registration should exist: %v", ids)
	}
}

// TestProjectRemoveKeepsData checks that removing a project drops only the
// registration and the managed project.yaml, never the collection data.
func TestProjectRemoveKeepsData(t *testing.T) {
	m := newTestManager(t)
	base := t.TempDir()
	rootA := makeProjectRoot(t, base, "app-a")
	rootB := makeProjectRoot(t, base, "app-b")

	pcA, err := m.AddProject(rootA)
	if err != nil {
		t.Fatal(err)
	}
	pcB, err := m.AddProject(rootB)
	if err != nil {
		t.Fatal(err)
	}

	collectionDir, err := config.CollectionDir(pcA.CollectionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(collectionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(collectionDir, "marker")
	if err := os.WriteFile(marker, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := m.RemoveProject(rootA); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.registrationPath(pcA.CollectionID)); !os.IsNotExist(err) {
		t.Fatalf("registration for A must be removed, stat err=%v", err)
	}
	if _, err := os.Stat(config.ProjectConfigPath(rootA)); !os.IsNotExist(err) {
		t.Fatalf("managed project.yaml for A must be removed, stat err=%v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("collection data must survive removal: %v", err)
	}

	// B is untouched.
	ids := m.listIDsForTest(t)
	if len(ids) != 1 || ids[pcB.CollectionID] == "" {
		t.Fatalf("registrations=%v", ids)
	}
	if _, err := os.Stat(config.ProjectConfigPath(rootB)); err != nil {
		t.Fatalf("B project.yaml must survive: %v", err)
	}

	// Removing an unregistered root is a no-op, not an error.
	if err := m.RemoveProject(filepath.Join(base, "never-added")); err != nil {
		t.Fatalf("remove of an unregistered root: %v", err)
	}
}

// TestProjectConcurrentAdd ensures the management lock serializes read-modify-
// write cycles: concurrent adds of distinct roots all land.
func TestProjectConcurrentAdd(t *testing.T) {
	m := newTestManager(t)
	base := t.TempDir()
	roots := make([]string, 6)
	for i := range roots {
		roots[i] = makeProjectRoot(t, base, "app-"+string(rune('a'+i)))
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(roots))
	for _, root := range roots {
		wg.Add(1)
		go func(root string) {
			defer wg.Done()
			if _, err := m.AddProject(root); err != nil {
				errs <- err
			}
		}(root)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent add: %v", err)
	}
	if ids := m.listIDsForTest(t); len(ids) != len(roots) {
		t.Fatalf("registrations=%d want %d: %v", len(ids), len(roots), ids)
	}
}

// TestProjectRegistrationFormat restricts the new registration file to
// id/root/description: legacy custom paths are not part of the new format.
func TestProjectRegistrationFormat(t *testing.T) {
	m := newTestManager(t)
	root := makeProjectRoot(t, t.TempDir(), "app")
	pc, err := m.AddProject(root)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(m.registrationPath(pc.CollectionID))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{"index_dir", "config_path"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("registration must not contain %s:\n%s", forbidden, text)
		}
	}
	for _, want := range []string{"id:", "root:", "description:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("registration missing %s:\n%s", want, text)
		}
	}

	// A legacy registration with custom paths is rejected in the new mode.
	legacy := filepath.Join(m.projectsDir(), "legacy-id.yaml")
	legacyBody := "id: legacy-id\nroot: " + root + "\nindex_dir: /tmp/elsewhere\n"
	if err := os.WriteFile(legacy, []byte(legacyBody), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.loadRegistrations(); err == nil {
		t.Fatal("legacy registration with custom paths must be rejected in the new mode")
	}
}

// TestProjectRelocate covers the explicit move path: the id is kept, the root
// changes only when confirmed and the collection is idle.
func TestProjectRelocate(t *testing.T) {
	m := newTestManager(t)
	base := t.TempDir()
	oldRoot := makeProjectRoot(t, base, "old-location")
	pc, err := m.AddProject(oldRoot)
	if err != nil {
		t.Fatal(err)
	}

	newRoot := makeProjectRoot(t, base, "new-location")
	copyFile(t, config.ProjectConfigPath(oldRoot), config.ProjectConfigPath(newRoot))
	// A real move removes the old directory.
	if err := os.RemoveAll(oldRoot); err != nil {
		t.Fatal(err)
	}

	// Unconfirmed relocation is refused.
	if _, err := m.RelocateProject(newRoot, RelocateOptions{}); err == nil {
		t.Fatal("relocation must require explicit confirmation")
	}
	// A busy collection is refused.
	busy := func(string) error { return errCollectionBusy }
	if _, err := m.RelocateProject(newRoot, RelocateOptions{Confirmed: true, CanRelocate: busy}); err == nil {
		t.Fatal("relocation must be refused while the collection is busy")
	}

	relocated, err := m.RelocateProject(newRoot, RelocateOptions{Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	if relocated.CollectionID != pc.CollectionID {
		t.Fatalf("relocation must keep the id: %q -> %q", pc.CollectionID, relocated.CollectionID)
	}
	regs, err := m.loadRegistrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(regs) != 1 {
		t.Fatalf("registrations=%v", regs)
	}
	if got, want := regs[0].Root, newRoot; got != want {
		t.Fatalf("registration root=%q want %q", got, want)
	}
}

// TestProjectRelocateRefusesStaleOldRoot refuses to move an identity while the
// old root still holds a project config: that would give one id two roots.
func TestProjectRelocateRefusesStaleOldRoot(t *testing.T) {
	m := newTestManager(t)
	base := t.TempDir()
	oldRoot := makeProjectRoot(t, base, "old")
	pc, err := m.AddProject(oldRoot)
	if err != nil {
		t.Fatal(err)
	}
	newRoot := makeProjectRoot(t, base, "new")
	copyFile(t, config.ProjectConfigPath(oldRoot), config.ProjectConfigPath(newRoot))

	// Old root still exists with the same project.yaml -> ambiguous, refuse.
	if _, err := m.RelocateProject(newRoot, RelocateOptions{Confirmed: true}); err == nil {
		t.Fatal("relocation must be refused while the old root still claims the id")
	}
	if ids := m.listIDsForTest(t); ids[pc.CollectionID] != oldRoot {
		t.Fatalf("registration must be unchanged: %v", ids)
	}
}

func writeProjectConfigBody(t *testing.T, root, body string) {
	t.Helper()
	path := config.ProjectConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertUnchanged(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s changed:\n got: %q\nwant: %q", path, got, want)
	}
}
