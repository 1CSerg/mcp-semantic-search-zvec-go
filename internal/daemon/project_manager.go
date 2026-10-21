package daemon

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
	"gopkg.in/yaml.v3"
)

// ProjectManager owns the user-level project registry:
//
//	<userdir>/projects.d/<collection_id>.yaml   id + root + description
//	<project>/.mcp-semantic-search-zvec-go/project.yaml
//
// It is the only writer of both files. Collection data lives under
// <userdir>/data/collections/<collection_id> and is never deleted here.
type ProjectManager struct {
	userDir string
}

// NewProjectManager creates a manager rooted at the user-level install dir.
func NewProjectManager(userDir string) *ProjectManager {
	return &ProjectManager{userDir: userDir}
}

// ErrProjectNotRegistered reports an operation on a root with no registration.
var ErrProjectNotRegistered = errors.New("project is not registered")

// errCollectionBusy reports that a collection still has open handles or an
// indexing job, so its root cannot be changed.
var errCollectionBusy = errors.New("collection is busy")

// RelocateOptions controls an explicit project relocation.
type RelocateOptions struct {
	// Confirmed must be true: relocation is never inferred from a missing root.
	Confirmed bool
	// CanRelocate is consulted before the root is changed; a non-nil error
	// aborts the relocation with the registry untouched.
	CanRelocate func(collectionID string) error
	// Description optionally replaces the registered description.
	Description string
}

// projectRegistrationFile is the new registration format: exactly these fields,
// no index/config paths (they are derived from the user-level install).
type projectRegistrationFile struct {
	ID          string `yaml:"id"`
	Root        string `yaml:"root"`
	Description string `yaml:"description"`
}

// registration is one loaded projects.d entry.
type registration struct {
	ID          string
	Root        string
	Description string
	Path        string
}

func (m *ProjectManager) projectsDir() string {
	return filepath.Join(m.userDir, "projects.d")
}

func (m *ProjectManager) registrationPath(collectionID string) string {
	return filepath.Join(m.projectsDir(), collectionID+".yaml")
}

func (m *ProjectManager) collectionsDir() string {
	return filepath.Join(m.userDir, "data", "collections")
}

// managementLockWait bounds how long a management operation waits for another
// process (or another goroutine) to finish its read-modify-write cycle.
const managementLockWait = 30 * time.Second

// lockManagement takes the cross-process management lock that serializes
// read-modify-write cycles over projects.d and the project.yaml files. The lock
// is held briefly, so contention is waited out rather than reported as an error.
func (m *ProjectManager) lockManagement() (func(), error) {
	l := lock.NewWithName(filepath.Join(m.userDir, "runtime"), "projects.lock", 60)
	deadline := time.Now().Add(managementLockWait)
	backoff := 5 * time.Millisecond
	for {
		err := l.TryAcquire()
		if err == nil {
			return func() { _ = l.Release() }, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("acquire projects management lock: %w", err)
		}
		time.Sleep(backoff)
		if backoff < 100*time.Millisecond {
			backoff *= 2
		}
	}
}

// loadRegistrations reads every projects.d entry strictly. A malformed or
// empty file is an error: an unreadable registry must never look like an empty
// one, otherwise the next write could drop registrations.
func (m *ProjectManager) loadRegistrations() ([]registration, error) {
	dir := m.projectsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read projects dir %s: %w", dir, err)
	}
	var out []registration
	seenID := make(map[string]string)
	seenRoot := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		path := filepath.Join(dir, name)
		reg, err := loadRegistrationFile(path, strings.TrimSuffix(name, filepath.Ext(name)))
		if err != nil {
			return nil, err
		}
		idKey := strings.ToLower(reg.ID)
		if other, ok := seenID[idKey]; ok {
			return nil, fmt.Errorf("projects.d: duplicate collection_id %q (%s and %s)", reg.ID, other, path)
		}
		seenID[idKey] = path
		rootKey := config.NormalizeAbsolutePath(reg.Root)
		if other, ok := seenRoot[rootKey]; ok {
			return nil, fmt.Errorf("projects.d: root %q registered twice (%s and %s)", reg.Root, other, path)
		}
		seenRoot[rootKey] = path
		out = append(out, reg)
	}
	return out, nil
}

func loadRegistrationFile(path, fileID string) (registration, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return registration{}, fmt.Errorf("read project registration %s: %w", path, err)
	}
	var reg projectRegistrationFile
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&reg); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return registration{}, fmt.Errorf("read project registration %s: %w", path, err)
		}
		return registration{}, fmt.Errorf("parse project registration %s: %w", path, err)
	}
	reg.ID = strings.TrimSpace(reg.ID)
	if reg.ID == "" {
		return registration{}, fmt.Errorf("project registration %s: id is required", path)
	}
	if err := config.ValidateCollectionID(reg.ID); err != nil {
		return registration{}, fmt.Errorf("project registration %s: %w", path, err)
	}
	if reg.ID != fileID {
		return registration{}, fmt.Errorf("project registration %s: id %q does not match file name %q", path, reg.ID, fileID)
	}
	root := strings.TrimSpace(reg.Root)
	if root == "" {
		return registration{}, fmt.Errorf("project registration %s: root is required", path)
	}
	if !filepath.IsAbs(root) {
		return registration{}, fmt.Errorf("project registration %s: root %q must be absolute", path, root)
	}
	return registration{
		ID:          reg.ID,
		Root:        filepath.Clean(root),
		Description: strings.TrimSpace(reg.Description),
		Path:        path,
	}, nil
}

func (m *ProjectManager) writeRegistration(reg registration) error {
	body, err := yaml.Marshal(projectRegistrationFile{
		ID:          reg.ID,
		Root:        reg.Root,
		Description: reg.Description,
	})
	if err != nil {
		return fmt.Errorf("encode project registration: %w", err)
	}
	return writeFileAtomic(reg.Path, body, 0o600)
}

// findRegistrationByRoot matches roots after canonicalization so cosmetic path
// differences and symlink aliases resolve to the same project.
func findRegistrationByRoot(regs []registration, root string) *registration {
	want := config.NormalizeAbsolutePath(root)
	for i := range regs {
		if config.NormalizeAbsolutePath(regs[i].Root) == want {
			return &regs[i]
		}
	}
	return nil
}

// findRegistrationByID matches ids case-insensitively on every platform, because
// Windows and macOS treat them as case-insensitive path names.
func findRegistrationByID(regs []registration, id string) *registration {
	for i := range regs {
		if strings.EqualFold(regs[i].ID, id) {
			return &regs[i]
		}
	}
	return nil
}

// canonicalProjectRoot resolves an existing project root for registration.
func canonicalProjectRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("project root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve project root %q: %w", root, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("project root %q: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project root %q is not a directory", abs)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks for %q: %w", abs, err)
	}
	return filepath.Clean(resolved), nil
}

// newCollectionID returns a UUID v4 generated from crypto/rand.
func newCollectionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate collection id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// uniqueCollectionID generates an id that collides with no existing registration.
func (m *ProjectManager) uniqueCollectionID(regs []registration) (string, error) {
	for attempt := 0; attempt < 8; attempt++ {
		id, err := newCollectionID()
		if err != nil {
			return "", err
		}
		if findRegistrationByID(regs, id) == nil {
			return id, nil
		}
	}
	return "", fmt.Errorf("could not generate a unique collection id")
}

// writeFileAtomic writes data to a temp file in the same directory and renames
// it over path, so a crash leaves either the old or the new content, never a
// truncated file.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// writeProjectConfig writes project.yaml atomically and returns its parsed form.
func writeProjectConfig(root string, pc *config.ProjectConfig) (*config.ProjectConfig, error) {
	body, err := config.MarshalProjectConfig(pc)
	if err != nil {
		return nil, err
	}
	// Re-parse what we are about to write: the file on disk must always satisfy
	// the strict schema.
	if _, err := config.ParseProjectConfig(body); err != nil {
		return nil, fmt.Errorf("generated project config is invalid: %w", err)
	}
	path := config.ProjectConfigPath(root)
	if err := writeFileAtomic(path, body, 0o644); err != nil {
		return nil, err
	}
	return config.LoadProjectConfig(path)
}

// readProjectConfig loads an existing project.yaml, distinguishing "missing"
// from "malformed" so a corrupt file is never silently regenerated.
func readProjectConfig(root string) (*config.ProjectConfig, error) {
	pc, err := config.LoadProjectConfig(config.ProjectConfigPath(root))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return pc, nil
}

// FindRegistration returns the registration for a collection id, or nil when it
// is not registered. Matching is case-insensitive on every platform.
func (m *ProjectManager) FindRegistration(collectionID string) (*registration, error) {
	if strings.TrimSpace(collectionID) == "" {
		return nil, fmt.Errorf("collection_id is required")
	}
	regs, err := m.loadRegistrations()
	if err != nil {
		return nil, err
	}
	if reg := findRegistrationByID(regs, collectionID); reg != nil {
		copied := *reg
		return &copied, nil
	}
	return nil, nil
}

// Registrations returns a read-only snapshot of the registry, used by the GUI and
// by status commands that must not mutate anything.
func (m *ProjectManager) Registrations() ([]Registration, error) {
	regs, err := m.loadRegistrations()
	if err != nil {
		return nil, err
	}
	out := make([]Registration, 0, len(regs))
	for _, reg := range regs {
		out = append(out, Registration{ID: reg.ID, Root: reg.Root, Description: reg.Description})
	}
	return out, nil
}

// Registration is the public view of one projects.d entry.
type Registration struct {
	ID          string
	Root        string
	Description string
}

// RemoveRegistrationOnly drops the projects.d entry for a collection without
// touching the project directory. Rollback uses it to discard a registration it
// created before restoring the previous one.
func (m *ProjectManager) RemoveRegistrationOnly(collectionID string) error {
	if err := config.ValidateCollectionID(collectionID); err != nil {
		return err
	}
	unlock, err := m.lockManagement()
	if err != nil {
		return err
	}
	defer unlock()
	path := m.registrationPath(collectionID)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove registration %s: %w", path, err)
	}
	return nil
}

// RestoreRegistration writes a registration back verbatim. It exists for
// rollback: a management command that changed the registry and then had its
// snapshot refused by the daemon must be able to undo its own write.
func (m *ProjectManager) RestoreRegistration(reg Registration) error {
	if err := config.ValidateCollectionID(reg.ID); err != nil {
		return err
	}
	root := strings.TrimSpace(reg.Root)
	if root == "" || !filepath.IsAbs(root) {
		return fmt.Errorf("registration root %q must be absolute", reg.Root)
	}
	unlock, err := m.lockManagement()
	if err != nil {
		return err
	}
	defer unlock()
	return m.writeRegistration(registration{
		ID:          reg.ID,
		Root:        filepath.Clean(root),
		Description: reg.Description,
		Path:        m.registrationPath(reg.ID),
	})
}

// AddProject registers root and returns its project config. The operation is
// idempotent for an already registered root with the same collection id.
func (m *ProjectManager) AddProject(root string) (*config.ProjectConfig, error) {
	rootAbs, err := canonicalProjectRoot(root)
	if err != nil {
		return nil, err
	}
	unlock, err := m.lockManagement()
	if err != nil {
		return nil, err
	}
	defer unlock()

	regs, err := m.loadRegistrations()
	if err != nil {
		return nil, err
	}
	existing := findRegistrationByRoot(regs, rootAbs)
	pc, err := readProjectConfig(rootAbs)
	if err != nil {
		return nil, err
	}

	if pc == nil {
		// No project.yaml: either repair a registration whose file went missing
		// or create a brand new collection identity.
		if existing != nil {
			pc = &config.ProjectConfig{
				SchemaVersion: config.ProjectSchemaVersion,
				CollectionID:  existing.ID,
				Description:   existing.Description,
			}
			written, err := writeProjectConfig(rootAbs, pc)
			if err != nil {
				return nil, err
			}
			return written, nil
		}
		id, err := m.uniqueCollectionID(regs)
		if err != nil {
			return nil, err
		}
		pc = &config.ProjectConfig{
			SchemaVersion: config.ProjectSchemaVersion,
			CollectionID:  id,
			Description:   filepath.Base(rootAbs),
		}
		return m.persistNewProject(rootAbs, pc, regs)
	}

	// project.yaml exists: it is the authority for the identity of this root.
	if existing != nil && !strings.EqualFold(existing.ID, pc.CollectionID) {
		return nil, fmt.Errorf(
			"project %s is already registered as collection %q but its project.yaml declares %q; "+
				"run --project-remove %q first, or --project-new-collection to fork a copy",
			rootAbs, existing.ID, pc.CollectionID, rootAbs)
	}
	if other := findRegistrationByID(regs, pc.CollectionID); other != nil {
		if config.NormalizeAbsolutePath(other.Root) != config.NormalizeAbsolutePath(rootAbs) {
			return nil, fmt.Errorf(
				"collection_id %q is already registered for root %s; refusing to point it at %s. "+
					"Run --project-new-collection %q to give this copy its own collection",
				other.ID, other.Root, rootAbs, rootAbs)
		}
	}

	description := pc.Description
	if description == "" {
		description = filepath.Base(rootAbs)
	}
	if err := m.writeRegistration(registration{
		ID:          pc.CollectionID,
		Root:        rootAbs,
		Description: description,
		Path:        m.registrationPath(pc.CollectionID),
	}); err != nil {
		return nil, err
	}
	return pc, nil
}

// persistNewProject writes project.yaml then the registration, restoring the
// previous project.yaml (or removing it) if the registration write fails, so a
// registration never points at a root whose project.yaml disagrees.
func (m *ProjectManager) persistNewProject(root string, pc *config.ProjectConfig, regs []registration) (*config.ProjectConfig, error) {
	cfgPath := config.ProjectConfigPath(root)
	previous, readErr := os.ReadFile(cfgPath)
	hadPrevious := readErr == nil

	written, err := writeProjectConfig(root, pc)
	if err != nil {
		return nil, err
	}
	description := pc.Description
	if description == "" {
		description = filepath.Base(root)
	}
	regErr := m.writeRegistration(registration{
		ID:          pc.CollectionID,
		Root:        root,
		Description: description,
		Path:        m.registrationPath(pc.CollectionID),
	})
	if regErr == nil {
		return written, nil
	}
	if hadPrevious {
		_ = writeFileAtomic(cfgPath, previous, 0o644)
	} else {
		_ = os.Remove(cfgPath)
	}
	return nil, regErr
}

// NewProjectCollection gives root a fresh collection id, keeping its settings.
// Used for a copied project: the original registration and its data are never
// touched, and the old collection directory is not removed.
func (m *ProjectManager) NewProjectCollection(root string) (*config.ProjectConfig, error) {
	rootAbs, err := canonicalProjectRoot(root)
	if err != nil {
		return nil, err
	}
	unlock, err := m.lockManagement()
	if err != nil {
		return nil, err
	}
	defer unlock()

	regs, err := m.loadRegistrations()
	if err != nil {
		return nil, err
	}
	pc, err := readProjectConfig(rootAbs)
	if err != nil {
		return nil, err
	}
	if pc == nil {
		pc = &config.ProjectConfig{Description: filepath.Base(rootAbs)}
	}
	oldID := pc.CollectionID

	newID, err := m.uniqueCollectionID(regs)
	if err != nil {
		return nil, err
	}
	forked := *pc
	forked.SchemaVersion = config.ProjectSchemaVersion
	forked.CollectionID = newID
	forked.DaemonURL = ""
	if forked.Description == "" {
		forked.Description = filepath.Base(rootAbs)
	}

	written, err := m.persistNewProject(rootAbs, &forked, regs)
	if err != nil {
		return nil, err
	}

	// The root now maps to the new id; drop only this root's old registration.
	if oldID != "" && !strings.EqualFold(oldID, newID) {
		if old := findRegistrationByID(regs, oldID); old != nil &&
			config.NormalizeAbsolutePath(old.Root) == config.NormalizeAbsolutePath(rootAbs) {
			if err := os.Remove(old.Path); err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("remove previous registration %s: %w", old.Path, err)
			}
		}
	}
	return written, nil
}

// RemoveProject drops the registration and the managed project.yaml for root.
// Collection data is preserved: removing a project is not a data deletion.
func (m *ProjectManager) RemoveProject(root string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return fmt.Errorf("project root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve project root %q: %w", root, err)
	}
	// The directory may already be gone; compare canonicalized forms when it
	// still exists, otherwise fall back to the cleaned absolute path.
	canonical := filepath.Clean(abs)
	if resolved, err := canonicalProjectRoot(abs); err == nil {
		canonical = resolved
	}

	unlock, err := m.lockManagement()
	if err != nil {
		return err
	}
	defer unlock()

	regs, err := m.loadRegistrations()
	if err != nil {
		return err
	}
	existing := findRegistrationByRoot(regs, canonical)
	if existing == nil {
		// Nothing registered: removal is idempotent for scripts.
		return nil
	}
	if err := os.Remove(existing.Path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove registration %s: %w", existing.Path, err)
	}
	cfgPath := config.ProjectConfigPath(canonical)
	if err := os.Remove(cfgPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove project config %s: %w", cfgPath, err)
	}
	return nil
}

// RelocateProject points an existing collection at a new root, keeping its id
// and its index. It requires explicit confirmation and an idle collection, and
// it refuses while the old root still claims the same collection id.
func (m *ProjectManager) RelocateProject(newRoot string, opts RelocateOptions) (*config.ProjectConfig, error) {
	if !opts.Confirmed {
		return nil, fmt.Errorf("relocation must be confirmed explicitly")
	}
	rootAbs, err := canonicalProjectRoot(newRoot)
	if err != nil {
		return nil, err
	}
	unlock, err := m.lockManagement()
	if err != nil {
		return nil, err
	}
	defer unlock()

	regs, err := m.loadRegistrations()
	if err != nil {
		return nil, err
	}
	pc, err := readProjectConfig(rootAbs)
	if err != nil {
		return nil, err
	}
	if pc == nil {
		return nil, fmt.Errorf("%s has no %s; relocate the project directory together with its config",
			rootAbs, config.ProjectFileName)
	}
	existing := findRegistrationByID(regs, pc.CollectionID)
	if existing == nil {
		return nil, fmt.Errorf("%w: collection %q", ErrProjectNotRegistered, pc.CollectionID)
	}
	if config.NormalizeAbsolutePath(existing.Root) == config.NormalizeAbsolutePath(rootAbs) {
		return pc, nil
	}
	if opts.CanRelocate != nil {
		if err := opts.CanRelocate(existing.ID); err != nil {
			return nil, fmt.Errorf("collection %q cannot be relocated: %w", existing.ID, err)
		}
	}

	// Refuse when the old root still holds a project.yaml for this collection:
	// that would leave one id claiming two roots.
	oldRoot := existing.Root
	if info, statErr := os.Stat(oldRoot); statErr == nil && info.IsDir() {
		oldCfg := config.ProjectConfigPath(oldRoot)
		if oldPC, loadErr := config.LoadProjectConfig(oldCfg); loadErr == nil &&
			strings.EqualFold(oldPC.CollectionID, existing.ID) {
			return nil, fmt.Errorf(
				"old root %s still declares collection %q; move or remove that project config first, "+
					"or use --project-new-collection for a copy", oldRoot, existing.ID)
		}
	}

	description := opts.Description
	if description == "" {
		description = existing.Description
	}
	if description == "" {
		description = filepath.Base(rootAbs)
	}
	if err := m.writeRegistration(registration{
		ID:          existing.ID,
		Root:        rootAbs,
		Description: description,
		Path:        existing.Path,
	}); err != nil {
		return nil, err
	}
	return pc, nil
}
