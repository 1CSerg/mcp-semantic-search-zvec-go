//go:build windows && cgo

package gui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lifecycle"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/service"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

type mockGUIService struct {
	getIndexStatus func(ctx context.Context) (json.RawMessage, error)
	semanticSearch func(ctx context.Context, req service.SearchRequest) (json.RawMessage, error)
	reindex        func(ctx context.Context, req service.ReindexRequest) (json.RawMessage, error)
}

func (m *mockGUIService) SemanticSearch(ctx context.Context, req service.SearchRequest) (json.RawMessage, error) {
	if m.semanticSearch != nil {
		return m.semanticSearch(ctx, req)
	}
	return nil, nil
}

func (m *mockGUIService) GetIndexStatus(ctx context.Context) (json.RawMessage, error) {
	if m.getIndexStatus != nil {
		return m.getIndexStatus(ctx)
	}
	return json.RawMessage(`{"indexing":{"state":"idle"}}`), nil
}

func (m *mockGUIService) Reindex(ctx context.Context, req service.ReindexRequest) (json.RawMessage, error) {
	if m.reindex != nil {
		return m.reindex(ctx, req)
	}
	return json.RawMessage(`{"ok":true}`), nil
}

func (m *mockGUIService) CheckUpdate(context.Context) (json.RawMessage, error) {
	return json.RawMessage(`{"version":"test"}`), nil
}

func (m *mockGUIService) Ready(context.Context) error { return nil }

func newTestAppUI(t *testing.T, svc service.Service, settings *config.Settings) *appUI {
	t.Helper()
	testApp := test.NewApp()
	w := testApp.NewWindow("test")
	if settings == nil {
		settings = &config.Settings{
			WorkspaceRoot: t.TempDir(),
			IndexDir:      t.TempDir(),
			App: config.AppConfig{
				Indexing: config.IndexingConfig{LockStaleSeconds: 300},
			},
		}
	}
	ui := &appUI{
		settings:      settings,
		svc:           svc,
		window:        w,
		statusLabel:   widget.NewLabel(""),
		messageLabel:  widget.NewLabel(""),
		progress:      widget.NewProgressBar(),
		detailText:    widget.NewMultiLineEntry(),
		killButton:    widget.NewButton("kill", nil),
		reclaimButton: widget.NewButton("reclaim", nil),
		versionLabel:  widget.NewLabel(""),
		queryEntry:    widget.NewEntry(),
		limitEntry:    widget.NewEntry(),
		pathGlobEntry: widget.NewEntry(),
		searchButton:  widget.NewButton("search", nil),
		searchStatus:  widget.NewLabel(""),
		snippetText:   widget.NewMultiLineEntry(),
	}
	ui.resultList = widget.NewList(
		func() int { return len(ui.results) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(_ widget.ListItemID, _ fyne.CanvasObject) {},
	)
	return ui
}

func labelText(l *widget.Label) string {
	var text string
	fyne.DoAndWait(func() {
		text = l.Text
	})
	return text
}

func buttonVisible(b *widget.Button) bool {
	var visible bool
	fyne.DoAndWait(func() {
		visible = b.Visible()
	})
	return visible
}

func waitForFyneIdle(t *testing.T, idle func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if idle() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timeout waiting for async GUI work")
}

func waitForSearchIdle(t *testing.T, ui *appUI) {
	t.Helper()
	waitForFyneIdle(t, func() bool { return !ui.searchInFlight.Load() })
}

func waitForRefreshStatus(t *testing.T, ui *appUI, wantSubstring string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var done bool
		fyne.DoAndWait(func() {
			done = !ui.statusRefreshBusy.Load() && strings.Contains(ui.statusLabel.Text, wantSubstring)
		})
		if done {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("statusLabel=%q, want substring %q", labelText(ui.statusLabel), wantSubstring)
}

func TestRemainingText(t *testing.T) {
	for _, tc := range []struct {
		seconds int
		want    string
	}{
		{seconds: -1, want: "осталось неизвестно"},
		{seconds: 0, want: "0 с осталось"},
		{seconds: 45, want: "45 с осталось"},
		{seconds: 90, want: "1 мин 30 с осталось"},
		{seconds: 3700, want: "1 ч 01 мин осталось"},
	} {
		if got := remainingText(tc.seconds); got != tc.want {
			t.Fatalf("remainingText(%d)=%q, want %q", tc.seconds, got, tc.want)
		}
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "  ", "value", "other"); got != "value" {
		t.Fatalf("firstNonEmpty=%q, want %q", got, "value")
	}
	if got := firstNonEmpty("", "   "); got != "" {
		t.Fatalf("firstNonEmpty=%q, want empty", got)
	}
}

func TestStringValue(t *testing.T) {
	m := map[string]any{"state": "running", "n": 1}
	if got := stringValue(m, "state", "fallback"); got != "running" {
		t.Fatalf("stringValue=%q", got)
	}
	if got := stringValue(m, "n", "fallback"); got != "fallback" {
		t.Fatalf("stringValue type mismatch=%q", got)
	}
	if got := stringValue(nil, "state", "fallback"); got != "fallback" {
		t.Fatalf("stringValue nil=%q", got)
	}
}

func TestIntValue(t *testing.T) {
	m := map[string]any{
		"a": 7,
		"b": int64(8),
		"c": float64(9),
		"d": json.Number("10"),
		"e": "nope",
	}
	for key, want := range map[string]int{"a": 7, "b": 8, "c": 9, "d": 10} {
		if got := intValue(m, key, -1); got != want {
			t.Fatalf("intValue(%q)=%d, want %d", key, got, want)
		}
	}
	if got := intValue(m, "e", -1); got != -1 {
		t.Fatalf("intValue(string)=%d, want fallback", got)
	}
	if got := intValue(m, "missing", 42); got != 42 {
		t.Fatalf("intValue(missing)=%d, want 42", got)
	}
}

func TestFloatValue(t *testing.T) {
	m := map[string]any{
		"a": float64(1.5),
		"b": float32(2.5),
		"c": 3,
		"d": int64(4),
		"e": json.Number("5.5"),
		"f": "nope",
	}
	for key, want := range map[string]float64{"a": 1.5, "b": 2.5, "c": 3, "d": 4, "e": 5.5} {
		if got := floatValue(m, key, -1); got != want {
			t.Fatalf("floatValue(%q)=%v, want %v", key, got, want)
		}
	}
	if got := floatValue(m, "f", -1); got != -1 {
		t.Fatalf("floatValue(string)=%v, want fallback", got)
	}
}

func TestFormatMap(t *testing.T) {
	if got := formatMap(nil); got != "  -" {
		t.Fatalf("formatMap(nil)=%q", got)
	}
	got := formatMap(map[string]any{"b": 2, "a": 1})
	if !strings.Contains(got, "a: 1") || !strings.Contains(got, "b: 2") {
		t.Fatalf("formatMap=%q", got)
	}
	if strings.Index(got, "a: 1") > strings.Index(got, "b: 2") {
		t.Fatalf("formatMap not sorted: %q", got)
	}
}

func TestPrettyJSON(t *testing.T) {
	if got := prettyJSON([]byte(`{"a":1}`)); !strings.Contains(got, "\"a\": 1") {
		t.Fatalf("prettyJSON=%q", got)
	}
	if got := prettyJSON([]byte("not json")); got != "not json" {
		t.Fatalf("prettyJSON(invalid)=%q", got)
	}
}

func TestSearchSummary(t *testing.T) {
	resp := searchResponse{
		Results:     []service.SearchResultItem{{Path: "a.go"}, {Path: "b.go"}},
		Message:     "partial",
		Performance: map[string]any{"total_ms": float64(12.3)},
		Indexing:    map[string]any{"running": true},
	}
	got := searchSummary(resp)
	for _, want := range []string{"2 результат(ов)", "partial", "12.3 мс", "индексация ещё идёт"} {
		if !strings.Contains(got, want) {
			t.Fatalf("searchSummary=%q, missing %q", got, want)
		}
	}
}

func TestFormatStatusDetail(t *testing.T) {
	status := indexStatus{
		WorkspaceRoot:      "/ws",
		IndexDir:           "/idx",
		ServerVersion:      "1.2.3",
		EmbeddingModelPath: "/model",
		ZvecError:          "boom",
		Indexing:           map[string]any{"state": "idle"},
	}
	raw := []byte(`{"workspace_root":"/ws"}`)
	got := formatStatusDetail(status, raw)
	for _, want := range []string{"Рабочая папка: /ws", "Модель эмбеддингов: /model", "Ошибка zvec: boom", "Исходный статус:"} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatStatusDetail missing %q in %q", want, got)
		}
	}
}

func TestTranslateIndexState(t *testing.T) {
	for state, want := range map[string]string{
		"idle":     "ожидание",
		"running":  "выполняется",
		"complete": "завершено",
		"error":    "ошибка",
		"custom":   "custom",
	} {
		if got := translateIndexState(state); got != want {
			t.Fatalf("translateIndexState(%q)=%q, want %q", state, got, want)
		}
	}
}

func TestJoinPIDs(t *testing.T) {
	if got := joinPIDs([]int{1, 22, 333}); got != "1, 22, 333" {
		t.Fatalf("joinPIDs=%q", got)
	}
	if got := joinPIDs(nil); got != "" {
		t.Fatalf("joinPIDs(nil)=%q, want empty", got)
	}
}

func TestConcurrentStdioNotice(t *testing.T) {
	got := concurrentStdioNotice(4242)
	for _, want := range []string{"PID 4242", "Cursor/MCP stdio", "без автоиндексации"} {
		if !strings.Contains(got, want) {
			t.Fatalf("notice=%q, missing %q", got, want)
		}
	}
}

func TestApplyStatus(t *testing.T) {
	ui := newTestAppUI(t, &mockGUIService{}, nil)
	status := indexStatus{
		WorkspaceRoot:         "/ws",
		IndexedChunksManifest: 3,
		Message:               "ready",
		Indexing:              map[string]any{"state": "running", "percent": 42.5, "remaining_seconds": 90, "files_done": 1, "files_total": 2, "chunks_indexed": 5},
	}
	ui.applyStatus(status, []byte(`{"indexing":{"state":"running"}}`))
	if !strings.Contains(labelText(ui.statusLabel), "выполняется") {
		t.Fatalf("statusLabel=%q", labelText(ui.statusLabel))
	}
	if !strings.Contains(labelText(ui.statusLabel), "42.5%") {
		t.Fatalf("statusLabel=%q", labelText(ui.statusLabel))
	}
	if buttonVisible(ui.killButton) {
		t.Fatal("kill button should stay hidden without concurrent stdio")
	}
	if ui.detailText.Text == "" {
		t.Fatal("expected detail text")
	}
}

func TestSearchValidation(t *testing.T) {
	ui := newTestAppUI(t, &mockGUIService{}, nil)
	ui.search()
	if labelText(ui.searchStatus) != "Запрос обязателен." {
		t.Fatalf("searchStatus=%q", labelText(ui.searchStatus))
	}

	ui.queryEntry.SetText("find auth")
	ui.limitEntry.SetText("-1")
	ui.search()
	if labelText(ui.searchStatus) != "Лимит должен быть неотрицательным числом." {
		t.Fatalf("searchStatus=%q", labelText(ui.searchStatus))
	}
}

func TestSearchSuccess(t *testing.T) {
	resp, err := json.Marshal(searchResponse{
		Results: []service.SearchResultItem{{
			Path: "pkg/auth.go", StartLine: 1, EndLine: 10, Score: 0.9, Snippet: "func Auth()",
		}},
		Message:     "ok",
		Performance: map[string]any{"total_ms": 12.0},
	})
	if err != nil {
		t.Fatal(err)
	}
	ui := newTestAppUI(t, &mockGUIService{
		semanticSearch: func(_ context.Context, req service.SearchRequest) (json.RawMessage, error) {
			if req.Query != "auth middleware" || req.Limit != 5 {
				t.Fatalf("req=%+v", req)
			}
			return resp, nil
		},
	}, nil)
	ui.queryEntry.SetText("auth middleware")
	ui.limitEntry.SetText("5")
	ui.search()

	waitForSearchIdle(t, ui)
	var resultsLen int
	var statusText string
	fyne.DoAndWait(func() {
		resultsLen = len(ui.results)
		statusText = ui.searchStatus.Text
	})
	if resultsLen != 1 {
		t.Fatalf("results=%v", ui.results)
	}
	if !strings.Contains(statusText, "1 результат") {
		t.Fatalf("searchStatus=%q", statusText)
	}
}

func TestRefreshStatusErrors(t *testing.T) {
	t.Run("service error", func(t *testing.T) {
		ui := newTestAppUI(t, &mockGUIService{
			getIndexStatus: func(context.Context) (json.RawMessage, error) {
				return nil, errors.New("status unavailable")
			},
		}, nil)
		ui.refreshStatus()
		waitForRefreshStatus(t, ui, "недоступен")
		if !strings.Contains(labelText(ui.messageLabel), "status unavailable") {
			t.Fatalf("messageLabel=%q", labelText(ui.messageLabel))
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		ui := newTestAppUI(t, &mockGUIService{
			getIndexStatus: func(context.Context) (json.RawMessage, error) {
				return json.RawMessage("{"), nil
			},
		}, nil)
		ui.refreshStatus()
		waitForRefreshStatus(t, ui, "неверный статус")
	})
}

func TestReindexCallsService(t *testing.T) {
	forceCh := make(chan bool, 1)
	ui := newTestAppUI(t, &mockGUIService{
		reindex: func(_ context.Context, req service.ReindexRequest) (json.RawMessage, error) {
			forceCh <- req.Force
			return json.RawMessage(`{"started":true}`), nil
		},
	}, nil)
	ui.reindex(true)

	select {
	case force := <-forceCh:
		if !force {
			t.Fatal("expected force reindex")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reindex was not called")
	}
	waitForRefreshStatus(t, ui, "ожидание")
}

func TestShowError(t *testing.T) {
	ui := newTestAppUI(t, &mockGUIService{}, nil)
	fyne.DoAndWait(func() {
		ui.showError(errors.New("boom"))
	})
}

func TestKillCompetingProcessNoPID(t *testing.T) {
	ui := newTestAppUI(t, &mockGUIService{}, nil)
	fyne.DoAndWait(func() {
		ui.killCompetingProcess()
	})
	waitForRefreshStatus(t, ui, "ожидание")
}

func TestBuildUI(t *testing.T) {
	ui := newTestAppUI(t, &mockGUIService{}, nil)
	fyne.DoAndWait(func() {
		content := ui.build()
		ui.window.SetContent(content)
		if content == nil {
			t.Fatal("expected UI content")
		}
	})
}

func TestSearchWithPathGlob(t *testing.T) {
	var gotGlob atomic.Value
	ui := newTestAppUI(t, &mockGUIService{
		semanticSearch: func(_ context.Context, req service.SearchRequest) (json.RawMessage, error) {
			if req.PathGlob != nil {
				gotGlob.Store(*req.PathGlob)
			}
			return json.RawMessage(`{"results":[]}`), nil
		},
	}, nil)
	ui.queryEntry.SetText("auth")
	ui.limitEntry.SetText("5")
	ui.pathGlobEntry.SetText("**/*.go")
	ui.search()
	waitForSearchIdle(t, ui)
	fyne.DoAndWait(func() {})
	if v := gotGlob.Load(); v != "**/*.go" {
		t.Fatalf("path glob=%v", v)
	}
}

func TestRefreshStatusSuccess(t *testing.T) {
	ui := newTestAppUI(t, &mockGUIService{
		getIndexStatus: func(context.Context) (json.RawMessage, error) {
			return json.RawMessage(`{"indexing":{"state":"running","percent":12.5},"message":"sync"}`), nil
		},
	}, nil)
	ui.refreshStatus()
	waitForRefreshStatus(t, ui, "выполняется")
}

func TestApplyStatusShowsKillButton(t *testing.T) {
	workspace := t.TempDir()
	indexDir := filepath.Join(workspace, ".mcp-semantic-search-zvec-go", "data", "index")
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	child := lifecycle.StartTestStdioHelper(t, workspace)
	defer func() { _ = child.Process.Kill() }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pid, ok := lifecycle.FindStdioForWorkspace(workspace, os.Getpid()); ok && pid != os.Getpid() {
			ui := newTestAppUI(t, &mockGUIService{}, &config.Settings{
				WorkspaceRoot: workspace,
				IndexDir:      indexDir,
				App: config.AppConfig{
					Indexing: config.IndexingConfig{LockStaleSeconds: 300},
				},
			})
			status := indexStatus{Indexing: map[string]any{"state": "idle"}}
			ui.applyStatus(status, []byte(`{"indexing":{"state":"idle"}}`))
			if !buttonVisible(ui.killButton) {
				t.Fatal("expected kill button visible")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("helper did not appear as live stdio MCP")
}

func TestConcurrentStdioHolderIgnoresDeadPID(t *testing.T) {
	indexDir := t.TempDir()
	pid := os.Getpid() + 100000
	if err := os.WriteFile(filepath.Join(indexDir, lock.StdioLockFileName), []byte(strings.Join([]string{
		strconv.Itoa(pid),
		"1",
		"1",
	}, " ")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ui := &appUI{
		settings: &config.Settings{
			WorkspaceRoot: indexDir,
			IndexDir:      indexDir,
			App: config.AppConfig{
				Indexing: config.IndexingConfig{LockStaleSeconds: 300},
			},
		},
	}
	gotPID, notice := ui.concurrentStdioHolder()
	if gotPID != 0 || notice != "" {
		t.Fatalf("concurrentStdioHolder()=%d notice=%q, want no stale holder", gotPID, notice)
	}
}

func TestConcurrentStdioHolderLiveStdio(t *testing.T) {
	workspace := t.TempDir()
	indexDir := filepath.Join(workspace, ".mcp-semantic-search-zvec-go", "data", "index")
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}

	child := lifecycle.StartTestStdioHelper(t, workspace)
	defer func() { _ = child.Process.Kill() }()

	deadline := time.Now().Add(5 * time.Second)
	var holderPID int
	for {
		if pid, ok := lifecycle.FindStdioForWorkspace(workspace, os.Getpid()); ok && pid != os.Getpid() {
			holderPID = pid
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper did not appear as live stdio MCP")
		}
		time.Sleep(50 * time.Millisecond)
	}

	ui := &appUI{
		settings: &config.Settings{
			WorkspaceRoot: workspace,
			IndexDir:      indexDir,
			App: config.AppConfig{
				Indexing: config.IndexingConfig{LockStaleSeconds: 300},
			},
		},
	}
	gotPID, notice := ui.concurrentStdioHolder()
	if gotPID != holderPID {
		t.Fatalf("concurrentStdioHolder pid=%d, want %d", gotPID, holderPID)
	}
	for _, want := range []string{"уже запущен процесс Cursor/MCP stdio", fmt.Sprintf("PID %d", holderPID), "без автоиндексации"} {
		if !strings.Contains(notice, want) {
			t.Fatalf("notice=%q, missing %q", notice, want)
		}
	}
}

func TestWindowTitle(t *testing.T) {
	got := windowTitle()
	if !strings.Contains(got, version.Name) {
		t.Fatalf("windowTitle=%q, missing name", got)
	}
	if !strings.Contains(got, version.Version) {
		t.Fatalf("windowTitle=%q, missing version", got)
	}
}

func TestFormatVersionLabel(t *testing.T) {
	got := formatVersionLabel()
	if got != fmt.Sprintf("Версия %s", version.Version) {
		t.Fatalf("formatVersionLabel=%q", got)
	}
}

func TestIsZvecLockStatus(t *testing.T) {
	if !isZvecLockStatus(indexStatus{
		ZvecOpenOK: false,
		ZvecError:  `zvec error [INTERNAL_ERROR]: Can't open lock file: /tmp/LOCK`,
	}) {
		t.Fatal("expected lock status")
	}
	if isZvecLockStatus(indexStatus{ZvecOpenOK: true, ZvecError: "lock file"}) {
		t.Fatal("expected false when zvec open")
	}
}

func TestApplyStatusZvecLockShowsReclaim(t *testing.T) {
	ui := newTestAppUI(t, &mockGUIService{}, nil)
	status := indexStatus{
		ZvecOpenOK:         false,
		ZvecError:          `Can't open lock file: D:\idx\zvec\ws_abc\LOCK`,
		ZvecCollectionPath: `D:\idx\zvec\ws_abc`,
		Indexing:           map[string]any{"state": "running", "running": true, "percent": 50.0},
	}
	ui.applyStatus(status, []byte(`{}`))
	if !buttonVisible(ui.reclaimButton) {
		t.Fatal("reclaim button should be visible on lock error")
	}
	if !strings.Contains(labelText(ui.messageLabel), "Индекс zvec заблокирован") {
		t.Fatalf("messageLabel=%q", labelText(ui.messageLabel))
	}
	if !strings.Contains(labelText(ui.messageLabel), "Прогресс manifest не означает рабочий поиск") {
		t.Fatalf("messageLabel=%q", labelText(ui.messageLabel))
	}
}

func TestShouldAutoResumeIndexing(t *testing.T) {
	base := indexStatus{
		ZvecOpenOK: true,
		Indexing: map[string]any{
			"state":       "idle",
			"running":     false,
			"message":     indexer.InterruptedMessage,
			"files_done":  100,
			"files_total": 500,
		},
	}
	if !shouldAutoResumeIndexing(base, 0) {
		t.Fatal("expected auto-resume for interrupted incomplete index")
	}
	if shouldAutoResumeIndexing(base, 1234) {
		t.Fatal("expected no auto-resume with concurrent stdio")
	}
	closed := base
	closed.ZvecOpenOK = false
	if shouldAutoResumeIndexing(closed, 0) {
		t.Fatal("expected no auto-resume when zvec closed")
	}
	complete := base
	complete.Indexing = map[string]any{
		"state":       "idle",
		"running":     false,
		"message":     indexer.InterruptedMessage,
		"files_done":  500,
		"files_total": 500,
	}
	if shouldAutoResumeIndexing(complete, 0) {
		t.Fatal("expected no auto-resume when complete")
	}
	legacy := base
	legacy.Indexing = map[string]any{
		"state":       "error",
		"running":     false,
		"error":       "indexing failed: foo: indexing embed: context canceled",
		"files_done":  100,
		"files_total": 500,
	}
	if shouldAutoResumeIndexing(legacy, 0) {
		t.Fatal("expected no auto-resume for unmigrated legacy error; RecoverInterruptedProgress migrates first")
	}
	migrated := base
	migrated.Indexing = map[string]any{
		"state":       "idle",
		"running":     false,
		"message":     indexer.InterruptedMessage,
		"files_done":  100,
		"files_total": 500,
	}
	if !shouldAutoResumeIndexing(migrated, 0) {
		t.Fatal("expected auto-resume after interrupted migration")
	}
	fatal := base
	fatal.Indexing = map[string]any{
		"state":       "error",
		"running":     false,
		"error":       "embedding provider down",
		"files_done":  100,
		"files_total": 500,
	}
	if shouldAutoResumeIndexing(fatal, 0) {
		t.Fatal("expected no auto-resume for fatal error")
	}
}

func TestApplyStatusInterrupted(t *testing.T) {
	ui := newTestAppUI(t, &mockGUIService{}, nil)
	status := indexStatus{
		ZvecOpenOK: true,
		Indexing: map[string]any{
			"state":       "idle",
			"running":     false,
			"message":     indexer.InterruptedMessage,
			"files_done":  100,
			"files_total": 500,
			"percent":     20.0,
		},
	}
	ui.applyStatus(status, []byte(`{}`))
	if !strings.Contains(labelText(ui.statusLabel), "прервана") {
		t.Fatalf("statusLabel=%q", labelText(ui.statusLabel))
	}
	if !strings.Contains(labelText(ui.messageLabel), "прервана") {
		t.Fatalf("messageLabel=%q", labelText(ui.messageLabel))
	}
}
