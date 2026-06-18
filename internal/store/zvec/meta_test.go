package zvec

import (
	"errors"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

func testIdentity(wsID, wsRoot, profile string, dims int) IndexIdentity {
	return IndexIdentity{
		WorkspaceID:      wsID,
		WorkspaceRoot:    wsRoot,
		Profile:          profile,
		Dimensions:       dims,
		ChunkingVersion:  1,
		ChunkingStrategy: "hybrid",
	}
}

func TestIndexMetaRoundTrip(t *testing.T) {
	dir := t.TempDir()
	identity := testIdentity("ws1", "/proj", "smoke", 128)
	if err := EnsureIndexMeta(dir, identity); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.WorkspaceID != "ws1" || meta.EmbeddingDimensions != 128 {
		t.Fatalf("meta=%+v", meta)
	}
	if err := ValidateIndexMeta(dir, "ws2", "smoke", 128, 0, ""); err == nil {
		t.Fatal("expected owner mismatch")
	}
}

func TestIndexMetaPathPresent(t *testing.T) {
	dir := t.TempDir()
	if IndexMetaPresent(dir) {
		t.Fatal("expected missing")
	}
	if err := WriteIndexMeta(dir, IndexMeta{}); err != nil {
		t.Fatal(err)
	}
	if !IndexMetaPresent(dir) {
		t.Fatal("expected present")
	}
}

func TestReadIndexMetaMissing(t *testing.T) {
	_, err := ReadIndexMeta(t.TempDir())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureIndexMetaExisting(t *testing.T) {
	dir := t.TempDir()
	identity := testIdentity("ws1", "/proj", "smoke", 128)
	if err := EnsureIndexMeta(dir, identity); err != nil {
		t.Fatal(err)
	}
	if err := EnsureIndexMeta(dir, identity); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.WorkspaceID != "ws1" {
		t.Fatalf("meta=%+v", meta)
	}
}

func TestEnsureIndexMetaSetsZvecGoVersion(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureIndexMeta(dir, testIdentity("ws1", "/proj", "smoke", 128)); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ZvecGoVersion != version.ZvecGoVersion {
		t.Fatalf("ZvecGoVersion=%q want %q", meta.ZvecGoVersion, version.ZvecGoVersion)
	}
}

func TestValidateIndexMetaDimensions(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureIndexMeta(dir, testIdentity("ws1", "/proj", "smoke", 128)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateIndexMeta(dir, "ws1", "smoke", 64, 0, ""); err == nil {
		t.Fatal("expected dimension mismatch")
	}
	if err := ValidateIndexMeta(dir, "ws1", "other", 128, 0, ""); err == nil {
		t.Fatal("expected profile mismatch")
	}
}

func TestEnsureIndexMetaBackfillsIncomplete(t *testing.T) {
	dir := t.TempDir()
	if err := WriteIndexMeta(dir, IndexMeta{ZvecGoVersion: version.ZvecGoVersion}); err != nil {
		t.Fatal(err)
	}
	identity := IndexIdentity{
		WorkspaceID:      "ws1",
		WorkspaceRoot:    "/proj",
		Profile:          "smoke",
		Dimensions:       128,
		ChunkingVersion:  1,
		ChunkingStrategy: "hybrid",
	}
	if err := EnsureIndexMeta(dir, identity); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := indexMetaFromIdentity(identity, version.ZvecGoVersion)
	if meta.WorkspaceID != want.WorkspaceID ||
		meta.WorkspaceRoot != want.WorkspaceRoot ||
		meta.CollectionName != want.CollectionName ||
		meta.ZvecGoVersion != want.ZvecGoVersion ||
		meta.ChunkingVersion != want.ChunkingVersion ||
		meta.ChunkingStrategy != want.ChunkingStrategy {
		t.Fatalf("meta=%+v want=%+v", meta, want)
	}
}

func TestValidateIndexMetaChunkingVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	if err := WriteIndexMeta(dir, IndexMeta{
		WorkspaceID:         "ws1",
		EmbeddingProfile:    "smoke",
		EmbeddingDimensions: 128,
		ChunkingVersion:     0,
	}); err != nil {
		t.Fatal(err)
	}
	err := ValidateIndexMeta(dir, "ws1", "smoke", 128, 1, "hybrid")
	if err == nil || !errors.Is(err, ErrOwnerMismatch) {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateIndexMetaChunkingStrategyMismatch(t *testing.T) {
	dir := t.TempDir()
	if err := WriteIndexMeta(dir, IndexMeta{
		WorkspaceID:         "ws1",
		EmbeddingProfile:    "smoke",
		EmbeddingDimensions: 128,
		ChunkingVersion:     1,
		ChunkingStrategy:    "line_window",
	}); err != nil {
		t.Fatal(err)
	}
	err := ValidateIndexMeta(dir, "ws1", "smoke", 128, 1, "hybrid")
	if err == nil || !errors.Is(err, ErrOwnerMismatch) {
		t.Fatalf("err=%v", err)
	}
}

func TestWriteIndexMetaPreservesCreatedAt(t *testing.T) {
	dir := t.TempDir()
	const created = "2020-01-01T00:00:00Z"
	if err := WriteIndexMeta(dir, IndexMeta{CreatedAt: created, WorkspaceID: "ws1"}); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadIndexMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.CreatedAt != created {
		t.Fatalf("CreatedAt=%q want %q", meta.CreatedAt, created)
	}
	if meta.UpdatedAt == "" {
		t.Fatal("expected UpdatedAt to be set")
	}
}

func TestIndexMetaFromIdentityChunkingFields(t *testing.T) {
	identity := IndexIdentity{
		WorkspaceID:      "ws1",
		WorkspaceRoot:    "/proj",
		Profile:          "smoke",
		Dimensions:       128,
		ChunkingVersion:  1,
		ChunkingStrategy: "hybrid",
	}
	meta := indexMetaFromIdentity(identity, version.ZvecGoVersion)
	if meta.ChunkingVersion != 1 || meta.ChunkingStrategy != "hybrid" {
		t.Fatalf("meta=%+v", meta)
	}
}
