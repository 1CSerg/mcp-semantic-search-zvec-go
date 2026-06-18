//go:build realworld && zvec

package scenarios

import (
	"strings"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/tests/realworld/harness"
)

func ensureIndexedHTTP(t *testing.T, repo string, port int) *harness.ServerProcess {
	t.Helper()
	srv := harness.StartHTTPServer(t, repo, port)
	harness.ForceReindex(t, srv.HTTPBase)
	harness.WaitIndexIdle(t, srv.HTTPBase)
	return srv
}

func TestChunkingASTLanguages(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	srv := ensureIndexedHTTP(t, repo, 19310)
	base := srv.HTTPBase

	cases := []struct {
		query    string
		path     string
		marker   string
		strategy string
	}{
		{"REALWORLD_GO_AUTH_GATE", "middleware.go", "REALWORLD_GO_AUTH_GATE", "ast"},
		{"REALWORLD_PY_HANDLER", "handlers.py", "REALWORLD_PY_HANDLER", "ast"},
		{"REALWORLD_JS_UTIL", "utils.js", "REALWORLD_JS_UTIL", "ast"},
		{"REALWORLD_JSX_LEGACY", "LegacyButton.jsx", "REALWORLD_JSX_LEGACY", "ast"},
		{"REALWORLD_TSX_BUTTON", "Button.tsx", "REALWORLD_TSX_BUTTON", "ast"},
		{"REALWORLD_BSL_PROCEDURE", "Module.bsl", "REALWORLD_BSL_PROCEDURE", "ast"},
		{"REALWORLD_BSL_OS_MODULE", "CommonModule.os", "REALWORLD_BSL_OS_MODULE", "ast"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			hit := harness.AssertSearchHit(t, base, tc.query, tc.path, "", tc.strategy)
			snippet, _ := hit["snippet"].(string)
			if !strings.Contains(snippet, tc.marker) {
				t.Fatalf("snippet missing %q: %q", tc.marker, snippet)
			}
			if sym, _ := hit["symbol_name"].(string); sym == "" {
				t.Fatalf("expected non-empty symbol_name for AST hit: %v", hit)
			}
		})
	}
}

func TestChunkingProse(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	srv := ensureIndexedHTTP(t, repo, 19311)
	base := srv.HTTPBase

	for _, tc := range []struct {
		query, path, strategy string
	}{
		{"REALWORLD_MD_SECTION", "guide.md", "prose"},
		{"REALWORLD_MD_SECTION", "architecture.markdown", "prose"},
		{"REALWORLD_MDC_RULE", "cursor-rule.mdc", "prose"},
		{"REALWORLD_TXT_PARAGRAPH", "notes.txt", "prose"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			hit := harness.AssertSearchHit(t, base, tc.query, tc.path, "", tc.strategy)
			if kind, _ := hit["symbol_kind"].(string); kind == "" {
				t.Fatalf("expected symbol_kind for prose hit: %v", hit)
			}
		})
	}
}

func TestChunkingSDBLQuery(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	srv := ensureIndexedHTTP(t, repo, 19312)
	hit := harness.AssertSearchHit(t, srv.HTTPBase, "REALWORLD_SDBL_SELECT", "Report.dcs", "", "")
	if ct, _ := hit["chunk_type"].(string); ct != "query" {
		t.Fatalf("chunk_type=%q want query", ct)
	}
}

func TestChunkingLineWindow(t *testing.T) {
	repo := harness.RequireHarness(t)
	defer harness.AssertNoLeftovers(t, repo)

	srv := ensureIndexedHTTP(t, repo, 19313)
	base := srv.HTTPBase

	for _, tc := range []struct {
		query, path, marker string
	}{
		{"REALWORLD_SQL_SCHEMA", "schema.sql", "REALWORLD_SQL_SCHEMA"},
		{"REALWORLD_SH_DEPLOY", "deploy.sh", "REALWORLD_SH_DEPLOY"},
		{"REALWORLD_JAVA_APP", "App.java", "REALWORLD_JAVA_APP"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			hit := harness.AssertSearchHit(t, base, tc.query, tc.path, "", "line_window")
			snippet, _ := hit["snippet"].(string)
			if !strings.Contains(snippet, tc.marker) {
				t.Fatalf("snippet missing %q: %q", tc.marker, snippet)
			}
		})
	}
}
