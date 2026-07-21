package version

const (
	Name    = "mcp-semantic-search-zvec-go"
	Version = "0.2.0"

	// ZvecGoVersion must match go.mod require github.com/zvec-ai/zvec-go (tag vX.Y.Z).
	// Windows Unicode index paths: ACP patch applied by fetch-zvec-libs (scripts/fetch/patches/zvec-go-acp).
	ZvecGoVersion = "v0.6.0"
)
