package update

import (
	"context"
	"encoding/json"
	"errors"
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
	defaultCacheTTL     = time.Hour
	errorCacheTTL       = time.Minute
	serverErrorCacheTTL = 10 * time.Second
	envDisable          = "CHECK_UPDATE_DISABLE"
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

	mu            sync.Mutex
	cached        Info
	cachedAt      time.Time
	cacheSuccess  bool
	cacheDuration time.Duration
	sf            singleflight.Group
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
	if err := ctx.Err(); err != nil {
		return Info{
			InstalledVersion: installedVersion,
			LatestVersion:    installedVersion,
			UpdateAvailable:  false,
			GitHubRepo:       c.repo,
			Message:          fmt.Sprintf("update check failed: %v", err),
		}
	}

	if info, ok := c.cachedInfo(installedVersion); ok {
		return info
	}

	v, _, _ := c.sf.Do(c.repo, func() (any, error) {
		if info, ok := c.cachedInfo(installedVersion); ok {
			return info, nil
		}
		timeout := c.clientTimeout()
		fetchCtx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		info, fetchErr := c.fetch(fetchCtx, installedVersion)
		if shouldCacheFetch(fetchErr) {
			success := fetchErr == nil
			c.mu.Lock()
			c.cached = info
			c.cachedAt = time.Now()
			c.cacheSuccess = success
			c.cacheDuration = c.cacheTTLFor(success, fetchErr)
			c.mu.Unlock()
		}
		info.InstalledVersion = installedVersion
		return info, nil
	})
	info := v.(Info)
	if err := ctx.Err(); err != nil {
		return Info{
			InstalledVersion: installedVersion,
			LatestVersion:    installedVersion,
			UpdateAvailable:  false,
			GitHubRepo:       c.repo,
			Message:          fmt.Sprintf("update check failed: %v", err),
		}
	}
	info.InstalledVersion = installedVersion
	return info
}

func (c *Checker) clientTimeout() time.Duration {
	if c.client != nil && c.client.Timeout > 0 {
		return c.client.Timeout
	}
	return 10 * time.Second
}

func shouldCacheFetch(err error) bool {
	if err == nil {
		return true
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

func (c *Checker) cacheTTLFor(success bool, err error) time.Duration {
	if success {
		return c.ttl
	}
	var he *httpStatusError
	if errors.As(err, &he) && he.code >= http.StatusInternalServerError {
		return serverErrorCacheTTL
	}
	return errorCacheTTL
}

type httpStatusError struct {
	code int
	msg  string
}

func (e *httpStatusError) Error() string { return e.msg }

// cachedInfo returns a copy of the cached Info with InstalledVersion set when
// the cache entry is still within its TTL.
func (c *Checker) cachedInfo(installedVersion string) (Info, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cachedAt.IsZero() {
		return Info{}, false
	}
	ttl := c.cacheDuration
	if ttl <= 0 {
		if c.cacheSuccess {
			ttl = c.ttl
		} else {
			ttl = errorCacheTTL
		}
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

func (c *Checker) fetch(ctx context.Context, installedVersion string) (Info, error) {
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
		return base, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "mcp-semantic-search-zvec-go")

	resp, err := c.client.Do(req)
	if err != nil {
		base.Message = fmt.Sprintf("update check failed: %v", err)
		return base, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		base.Message = fmt.Sprintf("update check failed: %v", err)
		return base, err
	}
	if resp.StatusCode != http.StatusOK {
		base.Message = fmt.Sprintf("update check failed: GitHub HTTP %d", resp.StatusCode)
		return base, &httpStatusError{code: resp.StatusCode, msg: base.Message}
	}

	var payload struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Name    string `json:"name"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		base.Message = fmt.Sprintf("update check failed: invalid JSON: %v", err)
		return base, fmt.Errorf("%s", base.Message)
	}

	latest := normalizeVersion(payload.TagName)
	if latest == "" {
		latest = normalizeVersion(payload.Name)
	}
	if latest == "" {
		base.Message = "update check failed: empty release tag"
		return base, errors.New(base.Message)
	}

	base.LatestVersion = latest
	base.ReleaseURL = payload.HTMLURL
	base.UpdateAvailable = versionGreater(latest, normalizeVersion(installedVersion))
	return base, nil
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
	return comparePrerelease(vpa, vpb) > 0
}

func splitSemver(v string) (core []int, prerelease []string) {
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

func comparePrerelease(a, b []string) int {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	if len(a) == 0 {
		return 1
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
	switch {
	case len(a) > len(b):
		return 1
	case len(a) < len(b):
		return -1
	default:
		return 0
	}
}

func comparePrereleaseIdentifier(a, b string) int {
	aNum := isNumericIdentifier(a)
	bNum := isNumericIdentifier(b)
	switch {
	case aNum && bNum:
		na, errA := strconv.ParseInt(a, 10, 64)
		nb, errB := strconv.ParseInt(b, 10, 64)
		if errA != nil || errB != nil {
			return strings.Compare(a, b)
		}
		switch {
		case na > nb:
			return 1
		case na < nb:
			return -1
		default:
			return 0
		}
	case aNum && !bNum:
		return -1
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
