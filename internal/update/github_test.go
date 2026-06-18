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

func TestParseVersionPartsNonNumeric(t *testing.T) {
	parts := parseVersionParts("1.x.0")
	if len(parts) != 1 || parts[0] != 0 {
		t.Fatalf("parts=%v", parts)
	}
}
