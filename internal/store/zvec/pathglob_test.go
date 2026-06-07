package zvec

import "testing"

func TestMatchPathGlob(t *testing.T) {
	tests := []struct {
		path, glob string
		want       bool
	}{
		{"internal/auth/middleware.go", "", true},
		{"internal/auth/middleware.go", "**/*.go", true},
		{"internal/auth/middleware.go", "**/*.py", false},
		{"cmd/main.go", "*.go", true},
		{"cmd/main.go", "cmd/*.go", true},
		{"other/main.go", "cmd/*.go", false},
	}
	for _, tc := range tests {
		if got := matchPathGlob(tc.path, tc.glob); got != tc.want {
			t.Errorf("matchPathGlob(%q, %q) = %v want %v", tc.path, tc.glob, got, tc.want)
		}
	}
}
