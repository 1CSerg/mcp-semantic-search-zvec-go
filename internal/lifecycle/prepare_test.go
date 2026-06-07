package lifecycle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

func TestPrepareStdio(t *testing.T) {
	dir := t.TempDir()
	settings := &config.Settings{
		WorkspaceRoot: dir,
		IndexDir:      filepath.Join(dir, config.DefaultInstallDirName, config.DefaultIndexSubdir),
	}
	if err := PrepareStdio(settings); err != nil {
		t.Fatalf("PrepareStdio: %v", err)
	}
	logDir := settings.LogsDir()
	info, err := os.Stat(logDir)
	if err != nil {
		t.Fatalf("log dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("log dir is not a directory")
	}
}
