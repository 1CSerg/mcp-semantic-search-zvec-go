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

	"golang.org/x/sync/singleflight"
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
	sf           singleflight.Group
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

	if info, ok := c.cachedInfo(installedVersion); ok {
		return info
	}

	v, _, _ := c.sf.Do(c.repo, func() (any, error) {
		// Another waiter may have filled the cache while we waited for the lock.
		if info, ok := c.cachedInfo(installedVersion); ok {
			return info, nil
		}
		info := c.fetch(ctx, installedVersion)
		success := info.Message == ""
		c.mu.Lock()
		c.cached = info
		c.cachedAt = time.Now()
		c.cacheSuccess = success
		c.mu.Unlock()
		info.InstalledVersion = installedVersion
		return info, nil
	})
	info := v.(Info)
	info.InstalledVersion = installedVersion
	return info
}

// cachedInfo returns a copy of the cached Info with InstalledVersion set when
// the cache entry is still within its TTL.
func (c *Checker) cachedInfo(installedVersion string) (Info, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cachedAt.IsZero() {
		return Info{}, false
	}
	ttl := c.ttl
	if !c.cacheSuccess {
		ttl = errorCacheTTL
	}
	if time.Since(c.cachedAt) >= ttl {
		return Info{}, false
	}
	out := c.cached
	out.InstalledVersion = installedVersion
	return out, true
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
	defer func() { _ = resp.Body.Close() }()

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
	va, vpa := splitSemver(a)
	vb, vpb := splitSemver(b)
	pa := va
	pb := vb
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
	// Numeric core versions are equal. Per semver, a release (no pre-release)
	// is greater than ANY pre-release of the same version (1.2.3 > 1.2.3-rc.1),
	// and two pre-releases compare dot-by-dot with numeric identifiers compared
	// as integers (1.2.3-rc.10 > 1.2.3-rc.2). build metadata never affects order.
	return comparePrerelease(vpa, vpb) > 0
}

// splitSemver separates a normalized version into its numeric core version parts
// and its pre-release identifiers. Build metadata (after '+') is discarded.
//
//	"1.2.3"         -> ([1,2,3],  nil)
//	"1.2.3-rc.1"    -> ([1,2,3],  ["rc","1"])
//	"1.2.3+build5"  -> ([1,2,3],  nil)
//	"1.2.3-beta.1+a" -> ([1,2,3], ["beta","1"])
//	"1.2"           -> ([1,2],    nil)
//
// A non-numeric core segment falls back to 0 for that position so that versions
// like "1.2.x" do not crash ordering; such tags are rare for release artifacts.
func splitSemver(v string) (core []int, prerelease []string) {
	// Drop build metadata.
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	pre := ""
	if i := strings.IndexByte(v, '-'); i >= 0 {
		pre = v[i+1:]
		v = v[:i]
	}
	for _, p := range strings.Split(v, ".") {
		p = strings.TrimSpace(p)
		n, err := strconv.Atoi(p)
		if err != nil {
			n = 0
		}
		core = append(core, n)
	}
	if pre != "" {
		prerelease = strings.Split(pre, ".")
	}
	return core, prerelease
}

// comparePrerelease implements the semver §11 pre-release ordering:
//   - A version WITHOUT a pre-release has HIGHER precedence than one WITH
//     (release > pre-release).
//   - Two pre-releases compare identifier-by-identifier (dot-separated). A
//     larger set of identifiers wins if all preceding ones are equal.
//   - Numeric identifiers (all digits) compare as integers; a numeric identifier
//     is LOWER than an alphanumeric one.
//   - Alphanumeric identifiers compare lexically by ASCII (semver §11.0.4).
//
// Returns >0 if a > b, 0 if equal, <0 if a < b.
func comparePrerelease(a, b []string) int {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	if len(a) == 0 {
		return 1 // release > pre-release
	}
	if len(b) == 0 {
		return -1
	}
	min := len(a)
	if len(b) < min {
		min = len(b)
	}
	for i := 0; i < min; i++ {
		if c := comparePrereleaseIdentifier(a[i], b[i]); c != 0 {
			return c
		}
	}
	// All shared identifiers equal: the longer pre-release wins.
	switch {
	case len(a) > len(b):
		return 1
	case len(a) < len(b):
		return -1
	default:
		return 0
	}
}

// comparePrereleaseIdentifier compares two single pre-release identifiers per
// semver: numeric < alphanumeric, numerics as integers, alphanumerics lexically.
func comparePrereleaseIdentifier(a, b string) int {
	aNum := isNumericIdentifier(a)
	bNum := isNumericIdentifier(b)
	switch {
	case aNum && bNum:
		na, _ := strconv.ParseInt(a, 10, 64)
		nb, _ := strconv.ParseInt(b, 10, 64)
		switch {
		case na > nb:
			return 1
		case na < nb:
			return -1
		default:
			return 0
		}
	case aNum && !bNum:
		return -1 // numeric has lower precedence
	case !aNum && bNum:
		return 1
	default:
		switch {
		case a > b:
			return 1
		case a < b:
			return -1
		default:
			return 0
		}
	}
}

// isNumericIdentifier reports whether s is a non-empty string of ASCII digits
// (semver's definition of a numeric identifier).
func isNumericIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
