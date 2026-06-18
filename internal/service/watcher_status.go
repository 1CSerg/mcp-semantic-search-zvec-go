package service

import "encoding/json"

func (p *Phase1) fileWatcherStatus() map[string]any {
	p.startupMu.RLock()
	w := p.watcherInst
	p.startupMu.RUnlock()
	if w != nil {
		return watcherStatusMap(w.Snapshot())
	}
	return map[string]any{
		"enabled_in_config": p.Settings.App.FileWatcher.Enabled,
		"running":           false,
		"run_as_daemon":     p.Settings.App.FileWatcher.RunAsDaemon,
		"daemon_supported":  false,
	}
}

func watcherStatusMap(st any) map[string]any {
	data, err := json.Marshal(st)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return out
}
