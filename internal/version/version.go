package version

const (
	Name    = "mcp-semantic-search-zvec-go"
	Version = "0.1.7"

	// ZvecGoVersion must match go.mod require github.com/zvec-ai/zvec-go (tag vX.Y.Z).
	// Windows Unicode index paths: patched via replace ./.deps/zvec-go (ACP path encoding).
	ZvecGoVersion = "v0.5.0"
)
