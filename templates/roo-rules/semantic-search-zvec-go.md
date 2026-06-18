# Semantic search (mcp-semantic-search-zvec-go)

> This file is managed by `mcp-semantic-search-zvec-go` install. Do not edit manually — re-run install to update.
>
> managedBy: mcp-semantic-search-zvec-go

MCP server for semantic code search (zvec + embeddings). Install dir: `.mcp-semantic-search-zvec-go/`.

## MCP tools

| Tool | Use |
|------|-----|
| `semantic_search` | Natural-language query → ranked code chunks |
| `index_status` | Paths, counts, indexing progress |
| `reindex` | Full or incremental reindex |
| `check_update` | Installed vs latest GitHub release (1h cache; `CHECK_UPDATE_DISABLE=true`; stub build returns placeholder) |

## Agent rules

- **Explore the codebase** → call `semantic_search` first; avoid full-repo walks or broad grep when MCP can answer.
- **Plan or review** (code review, audit, refactor plan, architecture survey) → **before** drafting the plan: `index_status`, then several `semantic_search` queries per theme (concurrency, errors, security, indexing, transport, etc.). Do not substitute Task/explore subagents, broad grep, or full-repo file walks when MCP is available.
- **Implement fixes** → still start with `semantic_search` for related code; use Read/Grep only after semantic hits or for line-level edits.
- **Index status** → call `index_status`; do not grep MCP tool implementation under `.mcp-semantic-search-zvec-go/`.
- While **`indexing.running`**, `semantic_search` results may be incomplete; check the `indexing` field in the response.
- **Do not read** `.mcp-semantic-search-zvec-go/data/index/` wholesale; on errors, tail `.mcp-semantic-search-zvec-go/logs/server.log`.
- If search is empty while indexing is idle → `reindex`; retry semantic search before falling back to broad exploration.

## `semantic_search` parameters

- `query` (required) — natural-language search string
- `limit` (integer, default 10) — number of results
- `path_glob` (optional) — narrow results to matching paths

Example: `{"query": "authentication middleware", "limit": 15}`

Search results may include **`symbol_name`**, **`symbol_kind`**, **`parent_scope`**, **`chunk_strategy`** after a hybrid AST reindex (empty on legacy indexes). `snippet` is raw source text.

## When to `reindex`

- Empty search while indexing is idle
- Embedding profile changed in `config.yaml`
- Chunking settings changed (`chunking.strategy`, `chunking.version`, `languages.*`, `context_prefix`, embed budget) or upgrade to a `treesitter` binary for AST on `.go`
- `index_owner_mismatch` or `identity_mismatch` → `reindex` with `force: true`

Native install (`AUTO_INDEX_ON_START=true`) reindexes on MCP start; shared daemon + `--stdio-proxy` does not — call `reindex` manually.
