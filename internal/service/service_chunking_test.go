package service

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

type keywordTestEmbedder struct {
	dims int
}

func (e *keywordTestEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		out[i] = keywordVector(text, e.dims)
	}
	return out, nil
}

func (e *keywordTestEmbedder) EmbedQuery(_ context.Context, query string) ([]float32, error) {
	return keywordVector(query, e.dims), nil
}

func (e *keywordTestEmbedder) Dimensions() int { return e.dims }

func (e *keywordTestEmbedder) HealthCheck(context.Context) error { return nil }

func keywordVector(text string, dims int) []float32 {
	if dims <= 0 {
		dims = 64
	}
	vec := make([]float32, dims)
	for _, term := range strings.Fields(strings.ToLower(text)) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(term))
		vec[int(h.Sum32())%dims] += 1
	}
	return vec
}

func dotProduct(a, b []float32) float32 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var sum float32
	for i := 0; i < n; i++ {
		sum += a[i] * b[i]
	}
	return sum
}

type keywordSearchStore struct {
	mu      sync.Mutex
	chunks  map[string]zvec.Chunk
	vectors map[string][]float32
	open    bool
}

func newKeywordSearchStore() *keywordSearchStore {
	return &keywordSearchStore{
		chunks:  map[string]zvec.Chunk{},
		vectors: map[string][]float32{},
	}
}

func (s *keywordSearchStore) Open() error {
	s.open = true
	return nil
}
func (s *keywordSearchStore) IsOpen() bool { return s.open }
func (s *keywordSearchStore) Close() error {
	s.open = false
	return nil
}
func (s *keywordSearchStore) DocCount() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.chunks), nil
}
func (s *keywordSearchStore) UpsertChunks(chunks []zvec.Chunk, vectors [][]float32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, ch := range chunks {
		s.chunks[ch.DocID] = ch
		if i < len(vectors) {
			s.vectors[ch.DocID] = vectors[i]
		}
	}
	return nil
}
func (s *keywordSearchStore) DeleteByIDs(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		delete(s.chunks, id)
		delete(s.vectors, id)
	}
	return nil
}
func (s *keywordSearchStore) WipeCollection() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chunks = map[string]zvec.Chunk{}
	s.vectors = map[string][]float32{}
	return nil
}
func (s *keywordSearchStore) Search(vec []float32, limit int, pathGlob string) ([]zvec.SearchHit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	type scored struct {
		hit   zvec.SearchHit
		score float32
	}
	var hits []scored
	for id, ch := range s.chunks {
		if pathGlob != "" && !matchPathGlob(ch.RelativePath, pathGlob) {
			continue
		}
		score := dotProduct(vec, s.vectors[id])
		if score <= 0 {
			continue
		}
		hits = append(hits, scored{
			hit: zvec.SearchHit{
				Path:          ch.RelativePath,
				StartLine:     ch.StartLine,
				EndLine:       ch.EndLine,
				Score:         float64(score),
				Snippet:       ch.Snippet,
				SymbolName:    ch.SymbolName,
				SymbolKind:    ch.SymbolKind,
				ParentScope:   ch.ParentScope,
				ChunkStrategy: ch.ChunkStrategy,
			},
			score: score,
		})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score == hits[j].score {
			return hits[i].hit.Path < hits[j].hit.Path
		}
		return hits[i].score > hits[j].score
	})
	if limit <= 0 {
		limit = 10
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]zvec.SearchHit, len(hits))
	for i, h := range hits {
		out[i] = h.hit
	}
	return out, nil
}

func (s *keywordSearchStore) storedChunks() []zvec.Chunk {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]zvec.Chunk, 0, len(s.chunks))
	for _, ch := range s.chunks {
		out = append(out, ch)
	}
	return out
}

func matchPathGlob(path, glob string) bool {
	glob = strings.ReplaceAll(glob, "\\", "/")
	path = strings.ReplaceAll(path, "\\", "/")
	if strings.Contains(glob, "*") {
		prefix := strings.TrimSuffix(glob, "*")
		return strings.HasPrefix(path, prefix) || strings.Contains(path, strings.Trim(prefix, "/"))
	}
	return strings.Contains(path, glob)
}

func copyChunkMinirepoTo(t *testing.T, destRoot string) {
	t.Helper()
	srcRoot := filepath.Join("..", "indexer", "chunk", "testdata", "integration", "minirepo")
	err := filepath.Walk(srcRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destRoot, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func hybridChunkingSettings(t *testing.T, workspaceRoot, indexDir, embedURL string, version int) *config.Settings {
	t.Helper()
	return &config.Settings{
		WorkspaceRoot: workspaceRoot,
		WorkspaceID:   "ws-chunking",
		IndexDir:      indexDir,
		App: config.AppConfig{
			ActiveProfile: "test",
			Profiles: map[string]config.EmbeddingProfile{
				"test": {
					Provider:         "openai_compatible",
					Model:            "test-model",
					Dimensions:       64,
					BaseURL:          embedURL,
					MaxInputTokens:   8192,
					EmbedBudgetRatio: 0.9,
					BatchSize:        16,
				},
			},
			Indexing: config.IndexingConfig{
				Extensions: []string{".go", ".py", ".js", ".jsx", ".ts", ".tsx", ".bsl", ".dcs", ".md", ".markdown", ".mdc", ".txt"},
				Chunking: config.ChunkingConfig{
					Strategy:          "hybrid",
					Version:           version,
					ContextPrefix:     false,
					ProseOverlapRatio: 0.12,
					Languages: map[string]config.LanguageConfig{
						"go":         {Enabled: true},
						"python":     {Enabled: true},
						"javascript": {Enabled: true},
						"typescript": {Enabled: true},
						"bsl":        {Enabled: true, IncludeSDBL: true},
					},
					LineWindow: config.LineWindowConfig{WindowLines: 5, OverlapLines: 1},
				},
			},
		},
	}
}

func runPhase1Reindex(t *testing.T, p *Phase1, force bool) {
	t.Helper()
	t.Cleanup(func() { releasePhase1TestResources(t, p) })
	raw, err := p.Reindex(context.Background(), ReindexRequest{Force: force})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["started"] != true {
		t.Fatalf("reindex not started: %v", payload)
	}
	waitCoordinatorIdle(t, p)
}

func newPhase1ChunkingTest(settings *config.Settings, embed *keywordTestEmbedder, store zvec.Store, coord *indexer.Coordinator, zcfg zvec.Config) *Phase1 {
	return &Phase1{
		Settings:    settings,
		embed:       embed,
		zvec:        store,
		zvecCfg:     zcfg,
		coordinator: coord,
		searchStats: NewSearchStats(settings.App.Search),
	}
}

func assertSearchResult(t *testing.T, raw json.RawMessage, wantPathSuffix, wantSymbolName, wantSymbolKind, wantStrategy string) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	results, ok := payload["results"].([]any)
	if !ok || len(results) == 0 {
		t.Fatalf("no results: %v", payload)
	}
	item, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result item=%v", results[0])
	}
	path, _ := item["path"].(string)
	if wantPathSuffix != "" && !strings.Contains(path, wantPathSuffix) {
		t.Fatalf("path=%q want suffix %q", path, wantPathSuffix)
	}
	if wantSymbolName != "" {
		symbol, _ := item["symbol_name"].(string)
		snippet, _ := item["snippet"].(string)
		if symbol != wantSymbolName && !strings.Contains(snippet, wantSymbolName) {
			t.Fatalf("symbol_name=%q snippet missing %q", symbol, wantSymbolName)
		}
	}
	if wantSymbolKind != "" {
		kind, _ := item["symbol_kind"].(string)
		if kind != wantSymbolKind {
			t.Fatalf("symbol_kind=%q want %q", kind, wantSymbolKind)
		}
	}
	if wantStrategy != "" {
		strategy, _ := item["chunk_strategy"].(string)
		if strategy != wantStrategy {
			t.Fatalf("chunk_strategy=%q want %q", strategy, wantStrategy)
		}
	}
}

func TestSemanticSearch_NewFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"index": 0, "embedding": []float64{0.1, 0.2, 0.3}}},
		})
	}))
	defer srv.Close()

	p, err := NewPhase1(phase1Settings(t, srv.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	p.zvec = &mockZvecStore{
		hits: []zvec.SearchHit{{
			Path:          "auth/middleware.go",
			StartLine:     5,
			EndLine:       18,
			Score:         0.91,
			Snippet:       "func AuthMiddleware",
			SymbolName:    "AuthMiddleware",
			SymbolKind:    "function",
			ParentScope:   "auth",
			ChunkStrategy: "ast",
		}},
	}

	raw, err := p.SemanticSearch(context.Background(), SearchRequest{Query: "auth middleware"})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	results, ok := payload["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results=%v", payload["results"])
	}
	item, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result item=%v", results[0])
	}
	for _, key := range []string{"symbol_name", "symbol_kind", "parent_scope", "chunk_strategy"} {
		if item[key] == nil || item[key] == "" {
			t.Fatalf("missing %s in result: %v", key, item)
		}
	}
	if item["symbol_name"] != "AuthMiddleware" || item["chunk_strategy"] != "ast" {
		t.Fatalf("unexpected fields: %v", item)
	}
}

func TestIndexStatus_ChunkingMeta(t *testing.T) {
	settings := phase1Settings(t, "http://127.0.0.1:9/v1")
	settings.App.Indexing.Chunking = config.ChunkingConfig{Strategy: "hybrid", Version: 1}
	if err := os.MkdirAll(settings.IndexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zvec.EnsureIndexMeta(settings.IndexDir, zvec.IndexIdentity{
		WorkspaceID:      settings.WorkspaceID,
		WorkspaceRoot:    settings.WorkspaceRoot,
		Profile:          settings.App.ActiveProfile,
		Dimensions:       3,
		ChunkingVersion:  1,
		ChunkingStrategy: "hybrid",
	}); err != nil {
		t.Fatal(err)
	}
	p, err := NewPhase1(settings)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.GetIndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["chunking_strategy"] != "hybrid" {
		t.Fatalf("chunking_strategy=%v", payload["chunking_strategy"])
	}
	if payload["chunking_version"] != float64(1) {
		t.Fatalf("chunking_version=%v", payload["chunking_version"])
	}
	if payload["index_chunking_version"] != float64(1) {
		t.Fatalf("index_chunking_version=%v", payload["index_chunking_version"])
	}
}

func TestIndexStatus_ChunkingVersionReason(t *testing.T) {
	settings, indexDir := phase1MigrationSettings(t, false)
	settings.App.Profiles = map[string]config.EmbeddingProfile{
		"test": {Provider: "openai_compatible", Model: "m", Dimensions: 3, BaseURL: "http://127.0.0.1:9/v1"},
	}
	settings.App.Indexing.Chunking.Version = 2
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zvec.WriteIndexMeta(indexDir, zvec.IndexMeta{
		WorkspaceID:         settings.WorkspaceID,
		WorkspaceRoot:       settings.WorkspaceRoot,
		EmbeddingProfile:    settings.App.ActiveProfile,
		EmbeddingDimensions: 3,
		ZvecGoVersion:       version.ZvecGoVersion,
		ChunkingVersion:     1,
	}); err != nil {
		t.Fatal(err)
	}
	p, err := NewPhase1(settings)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.GetIndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["identity_mismatch"] != true {
		t.Fatalf("identity_mismatch=%v", payload["identity_mismatch"])
	}
	reason, _ := payload["identity_mismatch_reason"].(string)
	if !strings.Contains(reason, "chunking_version") {
		t.Fatalf("identity_mismatch_reason=%q", reason)
	}
}

func TestIndexStatus_AfterChunkingVersionReindex(t *testing.T) {
	settings, indexDir := phase1MigrationSettings(t, false)
	settings.App.Profiles = map[string]config.EmbeddingProfile{
		"test": {Provider: "openai_compatible", Model: "m", Dimensions: 3, BaseURL: "http://127.0.0.1:9/v1"},
	}
	settings.App.Indexing.Chunking.Version = 2
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zvec.WriteIndexMeta(indexDir, zvec.IndexMeta{
		WorkspaceID:         settings.WorkspaceID,
		WorkspaceRoot:       settings.WorkspaceRoot,
		EmbeddingProfile:    settings.App.ActiveProfile,
		EmbeddingDimensions: 3,
		CollectionName:      zvec.CollectionName(settings.WorkspaceRoot, settings.App.ActiveProfile, 3),
		ZvecGoVersion:       version.ZvecGoVersion,
		ChunkingVersion:     1,
		ChunkingStrategy:    "hybrid",
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settings.WorkspaceRoot, "main.go"), []byte("package main\n\nfunc Main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	profile := settings.App.Profiles["test"]
	store := newKeywordSearchStore()
	embed := &keywordTestEmbedder{dims: profile.Dimensions}
	zcfg := zvec.Config{
		IndexDir: indexDir, WorkspaceRoot: settings.WorkspaceRoot, ProfileName: settings.App.ActiveProfile, Dimensions: profile.Dimensions,
	}
	coord := indexer.NewCoordinator(settings, profile, embed, store, zcfg)
	p := newPhase1ChunkingTest(settings, embed, store, coord, zcfg)
	runPhase1Reindex(t, p, true)

	raw, err := p.GetIndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if v, ok := payload["identity_mismatch"].(bool); ok && v {
		t.Fatalf("identity_mismatch=true reason=%v", payload["identity_mismatch_reason"])
	}
	if payload["index_chunking_version"] != float64(2) {
		t.Fatalf("index_chunking_version=%v want 2", payload["index_chunking_version"])
	}
	meta, err := zvec.ReadIndexMeta(indexDir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ChunkingVersion != 2 {
		t.Fatalf("meta ChunkingVersion=%d want 2", meta.ChunkingVersion)
	}
}

func TestSharedDaemon_MultiTenant_Chunking(t *testing.T) {
	root := t.TempDir()
	wsA := filepath.Join(root, "ws-a")
	wsB := filepath.Join(root, "ws-b")
	doc := []byte("# Tenant Doc\n\nShared prose paragraph for indexing.\n")
	for _, ws := range []string{wsA, wsB} {
		if err := os.MkdirAll(filepath.Join(ws, "docs"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(ws, "docs", "readme.md"), doc, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	settingsA := &config.Settings{
		WorkspaceRoot: wsA,
		WorkspaceID:   "tenant-a",
		IndexDir:      filepath.Join(wsA, "index"),
		App: config.AppConfig{
			ActiveProfile: "test",
			Profiles: map[string]config.EmbeddingProfile{
				"test": {Provider: "openai_compatible", Model: "m", Dimensions: 64, MaxInputTokens: 512, EmbedBudgetRatio: 1.0, BatchSize: 8},
			},
			Indexing: config.IndexingConfig{
				Extensions: []string{".md"},
				Chunking:   config.ChunkingConfig{Strategy: "hybrid", Version: 1, ProseOverlapRatio: 0.12},
			},
		},
	}
	settingsB := &config.Settings{
		WorkspaceRoot: wsB,
		WorkspaceID:   "tenant-b",
		IndexDir:      filepath.Join(wsB, "index"),
		App: config.AppConfig{
			ActiveProfile: "test",
			Profiles: map[string]config.EmbeddingProfile{
				"test": {Provider: "openai_compatible", Model: "m", Dimensions: 64, MaxInputTokens: 512, EmbedBudgetRatio: 1.0, BatchSize: 8},
			},
			Indexing: config.IndexingConfig{
				Extensions: []string{".md"},
				Chunking:   config.ChunkingConfig{Strategy: "line_window", Version: 1, LineWindow: config.LineWindowConfig{WindowLines: 5, OverlapLines: 1}},
			},
		},
	}

	storeA := newKeywordSearchStore()
	storeB := newKeywordSearchStore()
	embed := &keywordTestEmbedder{dims: 64}
	profile := settingsA.App.Profiles["test"]

	coordA := indexer.NewCoordinator(settingsA, profile, embed, storeA, zvec.Config{
		IndexDir: settingsA.IndexDir, WorkspaceRoot: wsA, ProfileName: "test", Dimensions: 64,
	})
	coordB := indexer.NewCoordinator(settingsB, profile, embed, storeB, zvec.Config{
		IndexDir: settingsB.IndexDir, WorkspaceRoot: wsB, ProfileName: "test", Dimensions: 64,
	})
	pA := newPhase1ChunkingTest(settingsA, embed, storeA, coordA, zvec.Config{
		IndexDir: settingsA.IndexDir, WorkspaceRoot: wsA, ProfileName: "test", Dimensions: 64,
	})
	pB := newPhase1ChunkingTest(settingsB, embed, storeB, coordB, zvec.Config{
		IndexDir: settingsB.IndexDir, WorkspaceRoot: wsB, ProfileName: "test", Dimensions: 64,
	})
	runPhase1Reindex(t, pA, true)
	runPhase1Reindex(t, pB, true)

	strategiesA := map[string]int{}
	for _, ch := range storeA.storedChunks() {
		strategiesA[ch.ChunkStrategy]++
	}
	strategiesB := map[string]int{}
	for _, ch := range storeB.storedChunks() {
		strategiesB[ch.ChunkStrategy]++
	}
	if strategiesA["prose"] == 0 && strategiesA["partial"] == 0 {
		t.Fatalf("tenant A expected prose chunks, got %v", strategiesA)
	}
	if strategiesB["line_window"] == 0 {
		t.Fatalf("tenant B expected line_window chunks, got %v", strategiesB)
	}
	if strategiesB["prose"] > 0 {
		t.Fatalf("tenant B should not use prose strategy: %v", strategiesB)
	}

	rawA, err := pA.SemanticSearch(context.Background(), SearchRequest{Query: "prose paragraph tenant"})
	if err != nil {
		t.Fatal(err)
	}
	rawB, err := pB.SemanticSearch(context.Background(), SearchRequest{Query: "prose paragraph tenant"})
	if err != nil {
		t.Fatal(err)
	}
	assertSearchResult(t, rawA, "readme.md", "", "", "prose")
	assertSearchResult(t, rawB, "readme.md", "", "", "line_window")
}

func TestKeywordVectorNonZero(t *testing.T) {
	vec := keywordVector("auth middleware token", 64)
	var sum float32
	for _, v := range vec {
		sum += v
	}
	if sum <= 0 {
		t.Fatal("expected non-zero keyword vector")
	}
	if math.IsNaN(float64(sum)) {
		t.Fatal("nan vector")
	}
}
