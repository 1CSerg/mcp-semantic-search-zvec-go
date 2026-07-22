//go:build zvec

package zvec

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloseErrorPreservesHandle(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "keep")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	closeErr := errors.New("native close failed")
	s := &CollectionStore{
		path: dir,
		open: true,
		closeHook: func() error {
			return closeErr
		},
	}

	if err := s.Close(); !errors.Is(err, closeErr) {
		t.Fatalf("Close err=%v", err)
	}
	if !s.open {
		t.Fatal("open flag must stay true after close error")
	}

	err := s.WipeCollection()
	if err == nil || !strings.Contains(err.Error(), "close before wipe") {
		t.Fatalf("WipeCollection err=%v", err)
	}
	if !errors.Is(err, closeErr) {
		t.Fatalf("WipeCollection err=%v want wrap of closeErr", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("RemoveAll must not run after close error: %v", statErr)
	}
	if !s.open {
		t.Fatal("open flag must stay true after wipe close error")
	}
}
