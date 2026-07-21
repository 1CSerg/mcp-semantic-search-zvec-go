package version

import (
	"os"
	"strings"
	"testing"
)

func TestZvecGoVersionMatchesGoMod(t *testing.T) {
	data, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	var modVer string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "github.com/zvec-ai/zvec-go ") {
			modVer = strings.TrimSpace(strings.TrimPrefix(line, "github.com/zvec-ai/zvec-go "))
			break
		}
	}
	if modVer == "" {
		t.Fatal("zvec-go require not found in go.mod")
	}
	if modVer != ZvecGoVersion {
		t.Fatalf("ZvecGoVersion=%q go.mod=%q", ZvecGoVersion, modVer)
	}
}
