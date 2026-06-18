package prose

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/indexer/chunk/token"
)

func TestWriteGoldenFiles(t *testing.T) {
	if os.Getenv("WRITE_GOLDEN") == "" {
		t.Skip("set WRITE_GOLDEN=1 to regenerate")
	}
	counter := token.CharCounter{}
	dir := filepath.Join("..", "testdata", "prose")
	for _, fx := range goldenFixtures {
		data, err := loadFixture(fx.file)
		if err != nil {
			t.Fatal(err)
		}
		chunks := collectChunks(t, fx.rel, data, testCfg(fx.budget))
		got := chunksToGolden(chunks, counter)
		out, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, '\n')
		path := filepath.Join(dir, fx.golden)
		if err := os.WriteFile(path, out, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s (%d chunks)", path, len(got))
	}
}
