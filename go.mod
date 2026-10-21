module github.com/1CSerg/mcp-semantic-search-zvec-go

go 1.26.3

require (
	github.com/bmatcuk/doublestar/v4 v4.9.1
	github.com/fsnotify/fsnotify v1.9.0
	github.com/modelcontextprotocol/go-sdk v1.6.1
	github.com/sugarme/tokenizer v0.3.0
	github.com/tree-sitter/go-tree-sitter v0.25.0
	github.com/tree-sitter/tree-sitter-bsl v0.0.0-00010101000000-000000000000
	github.com/tree-sitter/tree-sitter-go v0.23.4
	github.com/tree-sitter/tree-sitter-javascript v0.23.1
	github.com/tree-sitter/tree-sitter-python v0.23.6
	github.com/tree-sitter/tree-sitter-typescript v0.23.2
	github.com/yalue/onnxruntime_go v1.31.0
	github.com/zvec-ai/zvec-go v0.7.0
	golang.org/x/sync v0.20.0
	golang.org/x/sys v0.42.0
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.52.0
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/ebitengine/purego v0.10.1 // indirect
	github.com/emirpasic/gods v1.18.1 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-pointer v0.0.1 // indirect
	github.com/mitchellh/colorstring v0.0.0-20190213212951-d06e56a500db // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/niemeyer/pretty v0.0.0-20200227124842-a10e7caefd8e // indirect
	github.com/patrickmn/go-cache v2.1.0+incompatible // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/schollz/progressbar/v2 v2.15.0 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/sugarme/regexpset v0.0.0-20200920021344-4d4ec8eaf93c // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/text v0.25.0 // indirect
	gopkg.in/check.v1 v1.0.0-20200227125254-8fa46927fb4f // indirect
	modernc.org/libc v1.72.3 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)

replace github.com/zvec-ai/zvec-go => ./.deps/zvec-go

replace github.com/tree-sitter/tree-sitter-bsl => github.com/alkoleft/tree-sitter-bsl v0.1.6
