package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultCacheTTL = time.Hour
	errorCacheTTL   = time.Minute
	envDisable      = "CHECK_UPDATE_DISABLE"
)

// Info is the check_update response payload.
type Info struct {
	InstalledVersion string `json:"installed_version"`
	LatestVersion    string `json:"latest_version"`
	UpdateAvailable  bool   `json:"update_available"`
	GitHubRepo       string `json:"github_repo"`
	ReleaseURL       string `json:"release_url,omitempty"`
	Message          string `json:"message,omitempty"`
}

// Checker polls GitHub Releases for the configured repository.
type Checker struct {
	repo    string
	apiBase string
	client  *http.Client
	ttl     time.Duration

	mu           sync.Mutex
	cached       Info
	cachedAt     time.Time
	cacheSuccess bool
}

// NewChecker creates a GitHub release checker for owner/repo slug.
func NewChecker(repo string) *Checker {
	return &Checker{
		repo:    strings.TrimSpace(repo),
		apiBase: "https://api.github.com",
		client:  &http.Client{Timeout: 10 * time.Second},
		ttl:     defaultCacheTTL,
	}
}

// Check returns update metadata, using a short-lived cache.
func (c *Checker) Check(ctx context.Context, installedVersion string) Info {
	if disabled() {
		return Info{
			InstalledVersion: installedVersion,
			LatestVersion:    installedVersion,
			UpdateAvailable:  false,
			GitHubRepo:       c.repo,
			Message:          "update check disabled",
		}
	}
	if strings.TrimSpace(c.repo) == "" {
		return Info{
			InstalledVersion: installedVersion,
			LatestVersion:    installedVersion,
			UpdateAvailable:  false,
			Message:          "github repo not configured",
		}
	}

	c.mu.Lock()
	if !c.cachedAt.IsZero() {
		ttl := c.ttl
		if !c.cacheSuccess {
			ttl = errorCacheTTL
		}
		if time.Since(c.cachedAt) < ttl {
			out := c.cached
			out.InstalledVersion = installedVersion
			c.mu.Unlock()
			return out
		}
	}
	c.mu.Unlock()

	info := c.fetch(ctx, installedVersion)
	success := info.Message == ""

	c.mu.Lock()
	c.cached = info
	c.cachedAt = time.Now()
	c.cacheSuccess = success
	c.mu.Unlock()

	info.InstalledVersion = installedVersion
	return info
}

func disabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(envDisable)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func (c *Checker) fetch(ctx context.Context, installedVersion string) Info {
	base := Info{
		InstalledVersion: installedVersion,
		LatestVersion:    installedVersion,
		UpdateAvailable:  false,
		GitHubRepo:       c.repo,
	}

	url := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimRight(c.apiBase, "/"), c.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		base.Message = fmt.Sprintf("update check failed: %v", err)
		return base
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "mcp-semantic-search-zvec-go")

	resp, err := c.client.Do(req)
	if err != nil {
		base.Message = fmt.Sprintf("update check failed: %v", err)
		return base
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		base.Message = fmt.Sprintf("update check failed: %v", err)
		return base
	}
	if resp.StatusCode != http.StatusOK {
		base.Message = fmt.Sprintf("update check failed: GitHub HTTP %d", resp.StatusCode)
		return base
	}

	var payload struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Name    string `json:"name"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		base.Message = fmt.Sprintf("update check failed: invalid JSON: %v", err)
		return base
	}

	latest := normalizeVersion(payload.TagName)
	if latest == "" {
		latest = normalizeVersion(payload.Name)
	}
	if latest == "" {
		base.Message = "update check failed: empty release tag"
		return base
	}

	base.LatestVersion = latest
	base.ReleaseURL = payload.HTMLURL
	base.UpdateAvailable = versionGreater(latest, normalizeVersion(installedVersion))
	return base
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	return v
}

func versionGreater(a, b string) bool {
	a = normalizeVersion(a)
	b = normalizeVersion(b)
	if a == b {
		return false
	}
	pa := parseVersionParts(a)
	pb := parseVersionParts(b)
	maxLen := len(pa)
	if len(pb) > maxLen {
		maxLen = len(pb)
	}
	for i := 0; i < maxLen; i++ {
		var ai, bi int
		if i < len(pa) {
			ai = pa[i]
		}
		if i < len(pb) {
			bi = pb[i]
		}
		if ai != bi {
			return ai > bi
		}
	}
	return a > b
}

func parseVersionParts(v string) []int {
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			out = append(out, 0)
			continue
		}
		// Strip pre-release suffix (e.g. 1.2.3-beta)
		if i := strings.IndexAny(p, "-+"); i >= 0 {
			p = p[:i]
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return []int{0}
		}
		out = append(out, n)
	}
	return out
}
