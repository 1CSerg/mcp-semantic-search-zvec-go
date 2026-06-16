//go:build windows

package gui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lifecycle"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/lock"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/service"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/store/zvec"
	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/version"
)

type indexStatus struct {
	WorkspaceRoot         string         `json:"workspace_root"`
	IndexDir              string         `json:"index_dir"`
	ConfigPath            string         `json:"config_path"`
	ServerVersion         string         `json:"server_version"`
	IndexedFiles          int            `json:"indexed_files"`
	IndexedChunksManifest int            `json:"indexed_chunks_manifest"`
	ZvecDocCount          int            `json:"zvec_doc_count"`
	ZvecOpenOK            bool           `json:"zvec_open_ok"`
	Message               string         `json:"message"`
	Indexing              map[string]any `json:"indexing"`
	FileWatcher           map[string]any `json:"file_watcher"`
	SearchPerformance     map[string]any `json:"search_performance"`
	Diagnostics           map[string]any `json:"diagnostics"`
	ZvecError             string         `json:"zvec_error"`
	EmbeddingModelPath    string         `json:"embedding_model_path"`
	IndexZvecGoVersion    string         `json:"index_zvec_go_version"`
	ZvecCollection        string         `json:"zvec_collection"`
	ZvecCollectionPath    string         `json:"zvec_collection_path"`
	IndexMetaPresent      bool           `json:"index_meta_present"`
}

type searchResponse struct {
	Query       string                     `json:"query"`
	Results     []service.SearchResultItem `json:"results"`
	Message     string                     `json:"message"`
	Performance map[string]any             `json:"performance"`
	Indexing    map[string]any             `json:"indexing"`
}

type appUI struct {
	settings *config.Settings
	svc      service.Service
	window   fyne.Window

	versionLabel  *widget.Label
	statusLabel   *widget.Label
	messageLabel  *widget.Label
	progress      *widget.ProgressBar
	detailText    *widget.Entry
	killButton    *widget.Button
	reclaimButton *widget.Button

	queryEntry    *widget.Entry
	limitEntry    *widget.Entry
	pathGlobEntry *widget.Entry
	searchButton  *widget.Button
	searchStatus  *widget.Label
	resultList    *widget.List
	snippetText   *widget.Entry
	results       []service.SearchResultItem
}

func windowTitle() string {
	return fmt.Sprintf("Семантический поиск - %s v%s", version.Name, version.Version)
}

func formatVersionLabel() string {
	return fmt.Sprintf("Версия %s", version.Version)
}

// PrepareWorkspaceLocks stops stale MCP stdio instances and reclaims orphaned zvec LOCK files.
func PrepareWorkspaceLocks(settings *config.Settings) {
	if settings == nil {
		return
	}
	stopped, err := lifecycle.StopStdioForWorkspace(settings.WorkspaceRoot, settings.IndexDir)
	if err != nil {
		slog.Warn("gui workspace lock prep: stop stdio failed", "err", err)
	} else if len(stopped) > 0 {
		slog.Info("gui stopped stale mcp processes", "pids", stopped, "workspace", settings.WorkspaceRoot)
	}
	profile, err := settings.ActiveProfile()
	if err != nil {
		return
	}
	cfg := zvec.Config{
		IndexDir:      settings.IndexDir,
		WorkspaceRoot: settings.WorkspaceRoot,
		ProfileName:   settings.App.ActiveProfile,
		Dimensions:    profile.Dimensions,
	}
	if zvec.ReclaimCollectionLock(cfg) {
		slog.Info("gui reclaimed orphaned zvec collection lock", "collection", zvec.CollectionPath(cfg))
	}
}

// Run starts the Windows desktop GUI.
func Run(ctx context.Context, settings *config.Settings, svc service.Service) error {
	PrepareWorkspaceLocks(settings)

	a := app.NewWithID("github.com.1CSerg.mcp-semantic-search-zvec-go")
	w := a.NewWindow(windowTitle())
	w.Resize(fyne.NewSize(1100, 760))

	ui := &appUI{
		settings: settings,
		svc:      svc,
		window:   w,
	}
	w.SetContent(ui.build())

	ui.refreshStatus()
	go ui.pollStatus(ctx)
	go func() {
		<-ctx.Done()
		fyne.Do(func() {
			w.Close()
		})
	}()

	w.ShowAndRun()
	return nil
}

func (ui *appUI) build() fyne.CanvasObject {
	ui.versionLabel = widget.NewLabel(formatVersionLabel())
	ui.statusLabel = widget.NewLabel("Индексация: загрузка...")
	ui.statusLabel.TextStyle = fyne.TextStyle{Bold: true}
	ui.messageLabel = widget.NewLabel("")
	ui.progress = widget.NewProgressBar()
	ui.progress.Min = 0
	ui.progress.Max = 100

	ui.detailText = widget.NewMultiLineEntry()
	ui.detailText.Wrapping = fyne.TextWrapWord
	ui.detailText.SetMinRowsVisible(12)
	ui.detailText.Disable()

	ui.killButton = widget.NewButton("Завершить MCP-процесс", ui.killCompetingProcess)
	ui.killButton.Hide()
	ui.reclaimButton = widget.NewButton("Освободить LOCK", ui.reclaimZvecLock)
	ui.reclaimButton.Hide()

	top := container.NewVBox(
		widget.NewLabelWithStyle("Семантический поиск", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		ui.versionLabel,
		ui.statusLabel,
		ui.progress,
		ui.messageLabel,
		ui.killButton,
		ui.reclaimButton,
	)

	details := widget.NewAccordion(widget.NewAccordionItem("Подробный статус индексации", ui.detailText))

	ui.queryEntry = widget.NewEntry()
	ui.queryEntry.SetPlaceHolder("Опишите, что нужно найти...")
	ui.limitEntry = widget.NewEntry()
	ui.limitEntry.SetText(strconv.Itoa(config.DefaultSearchLimit))
	ui.pathGlobEntry = widget.NewEntry()
	ui.pathGlobEntry.SetPlaceHolder("Необязательная маска пути, например **/*.go")
	ui.searchStatus = widget.NewLabel("Введите запрос для поиска по индексированной рабочей папке.")
	ui.searchButton = widget.NewButton("Искать", ui.search)

	incrementalButton := widget.NewButton("Переиндексировать", func() {
		ui.reindex(false)
	})
	forceButton := widget.NewButton("Полная пересборка", func() {
		confirmDialog := dialog.NewCustomConfirm("Полная пересборка", "Пересобрать", "Отмена", widget.NewLabel("Текущий индекс будет удалён и создан заново. Продолжить?"), func(ok bool) {
			if ok {
				ui.reindex(true)
			}
		}, ui.window)
		confirmDialog.Show()
	})

	form := container.NewVBox(
		widget.NewForm(
			widget.NewFormItem("Запрос", ui.queryEntry),
			widget.NewFormItem("Лимит", ui.limitEntry),
			widget.NewFormItem("Маска пути", ui.pathGlobEntry),
		),
		container.NewHBox(ui.searchButton, incrementalButton, forceButton),
		ui.searchStatus,
	)

	ui.resultList = widget.NewList(
		func() int { return len(ui.results) },
		func() fyne.CanvasObject {
			return container.NewVBox(widget.NewLabel("путь"), widget.NewLabel("оценка"))
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			item := ui.results[id]
			box := obj.(*fyne.Container)
			box.Objects[0].(*widget.Label).SetText(fmt.Sprintf("%s:%d-%d", item.Path, item.StartLine, item.EndLine))
			box.Objects[1].(*widget.Label).SetText(fmt.Sprintf("оценка %.4f", item.Score))
		},
	)
	ui.resultList.OnSelected = func(id widget.ListItemID) {
		if id < 0 || id >= len(ui.results) {
			return
		}
		item := ui.results[id]
		ui.snippetText.SetText(fmt.Sprintf("%s:%d-%d\nоценка %.4f\n\n%s", item.Path, item.StartLine, item.EndLine, item.Score, item.Snippet))
	}

	ui.snippetText = widget.NewMultiLineEntry()
	ui.snippetText.Wrapping = fyne.TextWrapWord
	ui.snippetText.SetMinRowsVisible(20)
	ui.snippetText.Disable()

	searchPane := container.NewBorder(form, nil, nil, nil, container.NewHSplit(ui.resultList, ui.snippetText))
	content := container.NewBorder(top, nil, nil, nil, container.NewVSplit(details, searchPane))
	return content
}

func (ui *appUI) pollStatus(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ui.refreshStatus()
		}
	}
}

func (ui *appUI) refreshStatus() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		raw, err := ui.svc.GetIndexStatus(ctx)
		fyne.Do(func() {
			if err != nil {
				ui.statusLabel.SetText("Индексация: статус недоступен")
				ui.messageLabel.SetText(err.Error())
				return
			}
			var status indexStatus
			if err := json.Unmarshal(raw, &status); err != nil {
				ui.statusLabel.SetText("Индексация: неверный статус")
				ui.messageLabel.SetText(err.Error())
				return
			}
			ui.applyStatus(status, raw)
		})
	}()
}

func (ui *appUI) applyStatus(status indexStatus, raw []byte) {
	idx := status.Indexing
	state := stringValue(idx, "state", "idle")
	percent := floatValue(idx, "percent", 0)
	remaining := remainingText(intValue(idx, "remaining_seconds", -1))
	filesDone := intValue(idx, "files_done", 0)
	filesTotal := intValue(idx, "files_total", 0)
	chunks := intValue(idx, "chunks_indexed", status.IndexedChunksManifest)
	message := firstNonEmpty(stringValue(idx, "message", ""), status.Message)
	running, _ := idx["running"].(bool)

	if pid, notice := ui.concurrentStdioHolder(); notice != "" {
		ui.killButton.SetText(fmt.Sprintf("Завершить MCP-процесс (PID %d)", pid))
		ui.killButton.Show()
		if message == "" {
			message = notice
		} else {
			message = notice + " | " + message
		}
	} else {
		ui.killButton.Hide()
	}

	if isZvecLockStatus(status) {
		ui.reclaimButton.Show()
		lockMsg := zvecLockMessage(status)
		if message == "" {
			message = lockMsg
		} else {
			message = lockMsg + " | " + message
		}
	} else {
		ui.reclaimButton.Hide()
	}

	if running && !status.ZvecOpenOK {
		warn := "Прогресс manifest не означает рабочий поиск: zvec не открыт."
		if message == "" {
			message = warn
		} else if !strings.Contains(message, warn) {
			message = warn + " | " + message
		}
	}

	ui.progress.SetValue(percent)
	ui.statusLabel.SetText(fmt.Sprintf("Индексация: %s | %.1f%% | %s | файлов %d/%d | чанков %d", translateIndexState(state), percent, remaining, filesDone, filesTotal, chunks))
	ui.messageLabel.SetText(message)
	ui.detailText.SetText(formatStatusDetail(status, raw))
}

func isZvecLockStatus(status indexStatus) bool {
	return !status.ZvecOpenOK && lifecycle.IsZvecLockError(errors.New(status.ZvecError))
}

func zvecLockMessage(status indexStatus) string {
	path := status.ZvecCollectionPath
	if path == "" {
		path = status.IndexDir
	}
	return fmt.Sprintf("Индекс zvec заблокирован (%s). Нажмите «Освободить LOCK» или завершите другие процессы MCP.", path)
}

func (ui *appUI) reclaimZvecLock() {
	ui.reclaimButton.Disable()
	go func() {
		PrepareWorkspaceLocks(ui.settings)
		fyne.Do(func() {
			ui.reclaimButton.Enable()
			ui.searchStatus.SetText("Попытка освободить LOCK завершена. Обновляю статус...")
			ui.refreshStatus()
		})
	}()
}

func (ui *appUI) reindex(force bool) {
	go func() {
		PrepareWorkspaceLocks(ui.settings)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		raw, err := ui.svc.Reindex(ctx, service.ReindexRequest{Force: force})
		fyne.Do(func() {
			if err != nil {
				ui.showError(err)
				return
			}
			ui.searchStatus.SetText(string(raw))
			ui.refreshStatus()
		})
	}()
}

func (ui *appUI) search() {
	query := strings.TrimSpace(ui.queryEntry.Text)
	if query == "" {
		ui.searchStatus.SetText("Запрос обязателен.")
		return
	}
	limit, err := strconv.Atoi(strings.TrimSpace(ui.limitEntry.Text))
	if err != nil || limit < 0 {
		ui.searchStatus.SetText("Лимит должен быть неотрицательным числом.")
		return
	}
	var pathGlob *string
	if value := strings.TrimSpace(ui.pathGlobEntry.Text); value != "" {
		pathGlob = &value
	}
	if _, notice := ui.concurrentStdioHolder(); notice != "" {
		ui.searchStatus.SetText(notice + " Поиск будет выполнен этим GUI-процессом и может быть недоступен, если индекс заблокирован.")
	}

	ui.searchButton.Disable()
	if strings.TrimSpace(ui.searchStatus.Text) == "" || ui.searchStatus.Text == "Введите запрос для поиска по индексированной рабочей папке." {
		ui.searchStatus.SetText("Идёт поиск...")
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		raw, err := ui.svc.SemanticSearch(ctx, service.SearchRequest{
			Query:    query,
			Limit:    limit,
			PathGlob: pathGlob,
		})
		fyne.Do(func() {
			ui.searchButton.Enable()
			if err != nil {
				ui.showError(err)
				ui.searchStatus.SetText("Поиск не удался.")
				return
			}
			var response searchResponse
			if err := json.Unmarshal(raw, &response); err != nil {
				ui.showError(err)
				ui.searchStatus.SetText("Поиск вернул неверный JSON.")
				return
			}
			ui.results = response.Results
			ui.resultList.Refresh()
			if len(ui.results) > 0 {
				ui.resultList.Select(0)
			} else {
				ui.snippetText.SetText("")
			}
			ui.searchStatus.SetText(searchSummary(response))
		})
	}()
}

func searchSummary(response searchResponse) string {
	parts := []string{fmt.Sprintf("%d результат(ов)", len(response.Results))}
	if response.Message != "" {
		parts = append(parts, response.Message)
	}
	if totalMS := floatValue(response.Performance, "total_ms", -1); totalMS >= 0 {
		parts = append(parts, fmt.Sprintf("%.1f мс", totalMS))
	}
	if running, _ := response.Indexing["running"].(bool); running {
		parts = append(parts, "индексация ещё идёт; результаты могут быть неполными")
	}
	return strings.Join(parts, " | ")
}

func formatStatusDetail(status indexStatus, raw []byte) string {
	lines := []string{
		"Рабочая папка: " + status.WorkspaceRoot,
		"Каталог индекса: " + status.IndexDir,
		"Конфигурация: " + status.ConfigPath,
		fmt.Sprintf("Версия: %s | версия zvec-индекса: %s", status.ServerVersion, status.IndexZvecGoVersion),
		fmt.Sprintf("Проиндексировано файлов: %d | чанков в manifest: %d | документов zvec: %d", status.IndexedFiles, status.IndexedChunksManifest, status.ZvecDocCount),
		fmt.Sprintf("zvec открыт: %t | метаданные индекса: %t", status.ZvecOpenOK, status.IndexMetaPresent),
	}
	if status.EmbeddingModelPath != "" {
		lines = append(lines, "Модель эмбеддингов: "+status.EmbeddingModelPath)
	}
	if status.ZvecCollection != "" {
		lines = append(lines, "Коллекция: "+status.ZvecCollection)
	}
	if status.ZvecCollectionPath != "" {
		lines = append(lines, "Путь к коллекции: "+status.ZvecCollectionPath)
	}
	if status.ZvecError != "" {
		lines = append(lines, "Ошибка zvec: "+status.ZvecError)
	}
	lines = append(lines,
		"",
		"Индексация:",
		formatMap(status.Indexing),
		"",
		"Наблюдение за файлами:",
		formatMap(status.FileWatcher),
		"",
		"Производительность поиска:",
		formatMap(status.SearchPerformance),
		"",
		"Диагностика:",
		formatMap(status.Diagnostics),
		"",
		"Исходный статус:",
		prettyJSON(raw),
	)
	return strings.Join(lines, "\n")
}

func (ui *appUI) concurrentStdioHolder() (int, string) {
	self := os.Getpid()
	l := lock.NewStdio(ui.settings.IndexDir, ui.settings.App.Indexing.LockStaleSeconds)
	_ = l.ReclaimStale()

	if pid, ok := lifecycle.FindStdioForWorkspace(ui.settings.WorkspaceRoot, self); ok {
		return pid, concurrentStdioNotice(pid)
	}
	if pid, ok := l.LiveHolder(); ok && pid != self {
		return pid, concurrentStdioNotice(pid)
	}
	return 0, ""
}

func concurrentStdioNotice(pid int) string {
	return fmt.Sprintf("Для этой рабочей папки уже запущен процесс Cursor/MCP stdio (PID %d). GUI работает без автоиндексации и наблюдения за файлами.", pid)
}

func (ui *appUI) killCompetingProcess() {
	pid, _ := ui.concurrentStdioHolder()
	if pid == 0 {
		ui.showInformation("Процесс не найден", "Конкурирующий MCP-процесс больше не обнаружен.")
		ui.refreshStatus()
		return
	}

	confirmDialog := dialog.NewCustomConfirm(
		"Завершить MCP-процесс",
		"Завершить",
		"Отмена",
		widget.NewLabel(fmt.Sprintf("Завершить процесс MCP Cursor (PID %d)? Cursor может перезапустить MCP автоматически.", pid)),
		func(ok bool) {
			if !ok {
				return
			}
			ui.killButton.Disable()
			go ui.stopCompetingProcess()
		},
		ui.window,
	)
	confirmDialog.Show()
}

func (ui *appUI) stopCompetingProcess() {
	stopped, err := lifecycle.StopStdioForWorkspace(ui.settings.WorkspaceRoot, ui.settings.IndexDir)
	fyne.Do(func() {
		ui.killButton.Enable()
		if err != nil {
			ui.showError(err)
			ui.refreshStatus()
			return
		}
		if len(stopped) == 0 {
			ui.showInformation("Процесс не найден", "Конкурирующие MCP-процессы не найдены.")
		} else {
			ui.showInformation("Процесс завершён", "Завершены PID: "+joinPIDs(stopped))
		}
		ui.searchStatus.SetText("Конкурирующий процесс завершён. Запускаю переиндексацию...")
		ui.reindex(false)
		ui.refreshStatus()
	})
}

func (ui *appUI) showError(err error) {
	ui.showInformation("Ошибка", err.Error())
}

func (ui *appUI) showInformation(title, message string) {
	d := dialog.NewCustom(title, "ОК", widget.NewLabel(message), ui.window)
	d.Show()
}

func formatMap(values map[string]any) string {
	if len(values) == 0 {
		return "  -"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("  %s: %v", key, values[key]))
	}
	return strings.Join(lines, "\n")
}

func prettyJSON(raw []byte) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(out)
}

func remainingText(seconds int) string {
	if seconds < 0 {
		return "осталось неизвестно"
	}
	if seconds < 60 {
		return fmt.Sprintf("%d с осталось", seconds)
	}
	minutes := seconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%d мин %02d с осталось", minutes, seconds%60)
	}
	return fmt.Sprintf("%d ч %02d мин осталось", minutes/60, minutes%60)
}

func translateIndexState(state string) string {
	switch state {
	case "idle":
		return "ожидание"
	case "running":
		return "выполняется"
	case "complete":
		return "завершено"
	case "error":
		return "ошибка"
	default:
		return state
	}
}

func joinPIDs(pids []int) string {
	parts := make([]string, 0, len(pids))
	for _, pid := range pids {
		parts = append(parts, strconv.Itoa(pid))
	}
	return strings.Join(parts, ", ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func stringValue(values map[string]any, key, fallback string) string {
	if values == nil {
		return fallback
	}
	if value, ok := values[key].(string); ok {
		return value
	}
	return fallback
}

func intValue(values map[string]any, key string, fallback int) int {
	if values == nil {
		return fallback
	}
	switch value := values[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		if n, err := value.Int64(); err == nil {
			return int(n)
		}
	}
	return fallback
}

func floatValue(values map[string]any, key string, fallback float64) float64 {
	if values == nil {
		return fallback
	}
	switch value := values[key].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case json.Number:
		if n, err := value.Float64(); err == nil {
			return n
		}
	}
	return fallback
}
