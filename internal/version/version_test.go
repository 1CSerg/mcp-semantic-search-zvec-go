package version

import "testing"

func TestVersionConstants(t *testing.T) {
	if Name == "" || Version == "" {
		t.Fatalf("name=%q version=%q", Name, Version)
	}
}
