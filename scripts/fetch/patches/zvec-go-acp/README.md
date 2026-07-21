# zvec-go ACP path patch (Windows Unicode)

Local patch on top of upstream [zvec-ai/zvec-go](https://github.com/zvec-ai/zvec-go) so collection open/create paths with non-ASCII characters (e.g. Cyrillic `INDEX_DIR`) are passed to the native C API via system ACP (`WideCharToMultiByte`), not raw UTF-8 `C.CString`.

Applied automatically by `fetch-zvec-libs.sh` / `fetch-zvec-libs.ps1` after checkout of `ZVEC_GO_TAG`.

| File | Role |
|------|------|
| `collection.go.patch` | Wire `CreateAndOpen` / `Open` through `cStringPath` |
| `cpath.go` | CGO helper: normalize + ACP encode |
| `path_windows.go` / `path_unix.go` | Platform path helpers |
| `path_*.go` tests | Unit / integration coverage |

When bumping zvec-go: re-check that `collection.go.patch` still applies; regenerate with `git diff -- collection.go` from a patched `.deps/zvec-go` checkout if upstream changed those functions.
