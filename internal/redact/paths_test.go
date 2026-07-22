package redact

import (
	"strings"
	"testing"
)

func TestSanitizeErrorTextWindowsUNCAndCyrillic(t *testing.T) {
	in := `failed on \\server\share\proj\файл.go and C:\Users\Иван\secret.txt`
	out := SanitizeErrorText(in)
	if out == in {
		t.Fatalf("expected redaction, got %q", out)
	}
	if containsAny(out, `\\server`, `C:\Users`, "файл.go", "Иван") {
		t.Fatalf("paths leaked: %q", out)
	}
}

func TestSanitizeErrorTextPathsWithSpaces(t *testing.T) {
	tests := []struct {
		name string
		in   string
		leak []string
	}{
		{
			name: "windows spaced",
			in:   `open failed: C:\Program Files\App\bin\run.exe (access denied)`,
			leak: []string{`Program Files`, `\App\bin`, `run.exe`},
		},
		{
			name: "unix spaced",
			in:   `read error at /home/user/My Projects/repo/main.go: permission denied`,
			leak: []string{`/home/user`, `My Projects`, `main.go`},
		},
		{
			name: "unc spaced",
			in:   `copy from \\server\share\My Folder\data.bin failed`,
			leak: []string{`\\server`, `My Folder`, `data.bin`},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := SanitizeErrorText(tc.in)
			if out == tc.in {
				t.Fatalf("expected redaction, got %q", out)
			}
			if containsAny(out, tc.leak...) {
				t.Fatalf("paths leaked: %q", out)
			}
			if !strings.Contains(out, "<redacted>") {
				t.Fatalf("expected redacted marker in %q", out)
			}
		})
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if sub != "" && strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
