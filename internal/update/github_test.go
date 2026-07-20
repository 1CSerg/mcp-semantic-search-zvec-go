package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCheckerGitHubLatest(t *testing.T) {
	t.Setenv(envDisable, "false")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/releases/latest" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"tag_name":"v0.2.0","html_url":"https://github.com/owner/repo/releases/tag/v0.2.0"}`))
	}))
	defer srv.Close()

	checker := NewChecker("owner/repo")
	checker.apiBase = srv.URL
	checker.client = srv.Client()
	info := checker.Check(context.Background(), "0.1.0")
	if !info.UpdateAvailable {
		t.Fatalf("info=%+v", info)
	}
	if info.LatestVersion != "0.2.0" {
		t.Fatalf("latest=%q", info.LatestVersion)
	}
	if info.ReleaseURL == "" {
		t.Fatal("expected release_url")
	}
}

func TestCheckerCachesErrorsBriefly(t *testing.T) {
	t.Setenv(envDisable, "false")
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	checker := NewChecker("owner/repo")
	checker.apiBase = srv.URL
	checker.client = srv.Client()

	info1 := checker.Check(context.Background(), "0.1.0")
	if info1.Message == "" {
		t.Fatalf("expected error message: %+v", info1)
	}
	info2 := checker.Check(context.Background(), "0.1.0")
	if info2.Message == "" {
		t.Fatalf("expected cached error: %+v", info2)
	}
	if calls != 1 {
		t.Fatalf("calls=%d want 1 (error cached briefly)", calls)
	}
}

func TestCheckerRefetchesAfterErrorCacheExpires(t *testing.T) {
	t.Setenv(envDisable, "false")
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	checker := NewChecker("owner/repo")
	checker.apiBase = srv.URL
	checker.client = srv.Client()
	checker.ttl = time.Hour
	// errorCacheTTL is fixed at 1 minute; override cachedAt after first fetch.
	checker.Check(context.Background(), "0.1.0")

	checker.mu.Lock()
	checker.cachedAt = time.Now().Add(-errorCacheTTL - time.Second)
	checker.mu.Unlock()

	checker.Check(context.Background(), "0.1.0")
	if calls != 2 {
		t.Fatalf("calls=%d want 2 after error cache expiry", calls)
	}
}

func TestVersionGreater(t *testing.T) {
	if !versionGreater("0.2.0", "0.1.9") {
		t.Fatal("expected 0.2.0 > 0.1.9")
	}
	if versionGreater("1.0.0", "1.0.0") {
		t.Fatal("equal versions")
	}
	if !versionGreater("v2.0.0", "1.9.9") {
		t.Fatal("v prefix")
	}

	// Pre-release handling (semver): a release is greater than any pre-release of
	// the same numeric version. Previously the lexicographic fallback inverted
	// this and falsely reported updates available.
	if versionGreater("1.2.3-beta", "1.2.3") {
		t.Fatal("release must be greater than its pre-release: 1.2.3-beta > 1.2.3 is false")
	}
	if !versionGreater("1.2.3", "1.2.3-beta") {
		t.Fatal("expected 1.2.3 > 1.2.3-beta")
	}
	// Two pre-releases compare lexically.
	if !versionGreater("1.2.3-rc2", "1.2.3-rc1") {
		t.Fatal("expected 1.2.3-rc2 > 1.2.3-rc1")
	}
	if versionGreater("1.2.3-alpha", "1.2.3-beta") {
		t.Fatal("expected 1.2.3-alpha < 1.2.3-beta")
	}
	// Build metadata is ignored.
	if versionGreater("1.2.3+build5", "1.2.3+build1") {
		t.Fatal("build metadata must not affect ordering")
	}

	// Dotted pre-release: the trailing numeric identifier must NOT be folded into
	// the core version (the legacy parser produced [1,0,0,1] for "1.0.0-rc.1"
	// and concluded 1.0.0 < 1.0.0-rc.1, which is backwards).
	if !versionGreater("1.0.0", "1.0.0-rc.1") {
		t.Fatal("expected 1.0.0 > 1.0.0-rc.1 (release > dotted pre-release)")
	}
	if versionGreater("1.0.0-rc.1", "1.0.0") {
		t.Fatal("expected 1.0.0-rc.1 < 1.0.0 (pre-release < release)")
	}
	if !versionGreater("1.0.0-rc.1", "1.0.0-alpha.1") {
		t.Fatal("expected 1.0.0-rc.1 > 1.0.0-alpha.1")
	}

	// Numeric pre-release identifiers compared as INTEGERS, not lexically:
	// rc.10 must be greater than rc.2.
	if !versionGreater("1.2.3-rc.10", "1.2.3-rc.2") {
		t.Fatal("expected 1.2.3-rc.10 > 1.2.3-rc.2 (numeric pre-release ids)")
	}
	if versionGreater("1.2.3-rc.2", "1.2.3-rc.10") {
		t.Fatal("expected 1.2.3-rc.2 < 1.2.3-rc.10")
	}

	// Numeric pre-release identifier has LOWER precedence than alphanumeric.
	if !versionGreater("1.2.3-abc", "1.2.3-1") {
		t.Fatal("expected 1.2.3-abc > 1.2.3-1 (alphanumeric > numeric)")
	}

	// Longer pre-release wins when all shared identifiers are equal.
	if !versionGreater("1.2.3-rc.1.1", "1.2.3-rc.1") {
		t.Fatal("expected 1.2.3-rc.1.1 > 1.2.3-rc.1 (more identifiers)")
	}
}

func TestCheckerDisabled(t *testing.T) {
	t.Setenv(envDisable, "true")
	c := NewChecker("owner/repo")
	info := c.Check(context.Background(), "0.1.0")
	if info.UpdateAvailable {
		t.Fatal("expected no update when disabled")
	}
	if info.Message != "update check disabled" {
		t.Fatalf("message=%q", info.Message)
	}
}

func TestCheckerEmptyRepo(t *testing.T) {
	t.Setenv(envDisable, "false")
	c := NewChecker("")
	info := c.Check(context.Background(), "0.1.0")
	if info.Message != "github repo not configured" {
		t.Fatalf("message=%q", info.Message)
	}
}

func TestCheckerCachesSuccess(t *testing.T) {
	t.Setenv(envDisable, "false")
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"tag_name":"v0.2.0","html_url":"https://github.com/owner/repo/releases/tag/v0.2.0"}`))
	}))
	defer srv.Close()

	checker := NewChecker("owner/repo")
	checker.apiBase = srv.URL
	checker.client = srv.Client()
	checker.ttl = time.Hour

	info1 := checker.Check(context.Background(), "0.1.0")
	if !info1.UpdateAvailable {
		t.Fatalf("info=%+v", info1)
	}
	checker.Check(context.Background(), "0.1.0")
	if calls != 1 {
		t.Fatalf("calls=%d want 1 (success cached)", calls)
	}
}

func TestCheckerInvalidJSON(t *testing.T) {
	t.Setenv(envDisable, "false")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	checker := NewChecker("owner/repo")
	checker.apiBase = srv.URL
	checker.client = srv.Client()
	info := checker.Check(context.Background(), "0.1.0")
	if info.Message == "" || !strings.Contains(info.Message, "invalid JSON") {
		t.Fatalf("message=%q", info.Message)
	}
}

func TestCheckerEmptyReleaseTag(t *testing.T) {
	t.Setenv(envDisable, "false")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"","name":"","html_url":""}`))
	}))
	defer srv.Close()

	checker := NewChecker("owner/repo")
	checker.apiBase = srv.URL
	checker.client = srv.Client()
	info := checker.Check(context.Background(), "0.1.0")
	if info.Message != "update check failed: empty release tag" {
		t.Fatalf("message=%q", info.Message)
	}
}

func TestSplitSemverNonNumeric(t *testing.T) {
	// A non-numeric core segment is treated as 0 in its own position (rather
	// than collapsing the whole version to [0] as the legacy parser did), which
	// yields more useful ordering for tags like "1.x.0".
	core, pre := splitSemver("1.x.0")
	if len(core) != 3 || core[0] != 1 || core[1] != 0 || core[2] != 0 {
		t.Fatalf("core=%v", core)
	}
	if pre != nil {
		t.Fatalf("prerelease=%v want nil", pre)
	}
}

func TestSplitSemverPrerelease(t *testing.T) {
	cases := []struct {
		in           string
		core         []int
		prerelease   []string
	}{
		{"1.2.3", []int{1, 2, 3}, nil},
		{"1.2.3-rc.1", []int{1, 2, 3}, []string{"rc", "1"}},
		{"1.2.3+build5", []int{1, 2, 3}, nil},
		{"1.2.3-beta.1+a", []int{1, 2, 3}, []string{"beta", "1"}},
		{"1.2", []int{1, 2}, nil},
	}
	for _, tc := range cases {
		core, pre := splitSemver(tc.in)
		if !slicesEqual(core, tc.core) {
			t.Errorf("splitSemver(%q) core=%v want %v", tc.in, core, tc.core)
		}
		if !slicesStrEqual(pre, tc.prerelease) {
			t.Errorf("splitSemver(%q) pre=%v want %v", tc.in, pre, tc.prerelease)
		}
	}
}

func slicesEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func slicesStrEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
