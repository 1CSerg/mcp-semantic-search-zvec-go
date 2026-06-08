package crash

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

// Report describes a fatal process exit.
type Report struct {
	Timestamp     string `json:"timestamp"`
	Version       string `json:"version"`
	WorkspaceRoot string `json:"workspace_root"`
	Error         string `json:"error"`
	Stack         string `json:"stack,omitempty"`
}

// Write saves last_crash.json under logDir.
func Write(logDir, version, workspaceRoot string, fatal any) error {
	if fatal == nil {
		return nil
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return err
	}
	var msg string
	switch v := fatal.(type) {
	case error:
		msg = v.Error()
	default:
		msg = fmt.Sprint(v)
	}
	report := Report{
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Version:       version,
		WorkspaceRoot: workspaceRoot,
		Error:         msg,
		Stack:         string(debug.Stack()),
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(logDir, "last_crash.json.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(logDir, "last_crash.json"))
}
