// Package release looks up the newest GitHub release of a repository and
// compares versions. ws uses it for agent-tmux's cached version nudge and for
// `ws version --check`, which asks about ws itself.
package release

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// CacheTTL is how long a cached answer is trusted.
	CacheTTL = 24 * time.Hour
	// Timeout bounds one lookup.
	Timeout = 3 * time.Second
)

// Cache is the cache file: the last answer and when it was fetched.
type Cache struct {
	CheckedAt time.Time `json:"checked_at"`
	ETag      string    `json:"etag"`
	Tag       string    `json:"tag_name"`
}

func readCache(path string) Cache {
	var c Cache
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &c)
	}
	return c
}

func writeCache(path string, c Cache) {
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		_ = os.Rename(tmp, path)
	}
}

// Fetch asks url (a GitHub latest-release API) for the newest tag, with no
// cache. It gives up after Timeout.
func Fetch(ctx context.Context, url string) (string, error) {
	tag, _, err := fetch(ctx, url, "")
	return tag, err
}

// fetch sends the request; etag, when set, makes it conditional. A 304 comes
// back as notModified.
func fetch(ctx context.Context, url, etag string) (tag string, resp *http.Response, err error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ws")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer r.Body.Close()
	switch r.StatusCode {
	case http.StatusNotModified:
		return "", r, nil
	case http.StatusOK:
		var rel struct {
			Tag string `json:"tag_name"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&rel); err != nil {
			return "", r, err
		}
		if rel.Tag == "" {
			return "", r, fmt.Errorf("%s: no tag_name", url)
		}
		return rel.Tag, r, nil
	}
	return "", r, fmt.Errorf("%s: %s", url, r.Status)
}

// Latest returns the newest tag: from the cache file when it is younger than
// CacheTTL, else from url (a 304 keeps the cached tag).
func Latest(ctx context.Context, url, cachePath string) (string, error) {
	c := readCache(cachePath)
	now := time.Now()
	if c.Tag != "" && now.Sub(c.CheckedAt) < CacheTTL && !c.CheckedAt.After(now) {
		return c.Tag, nil
	}
	etag := ""
	if c.ETag != "" && c.Tag != "" {
		etag = c.ETag
	}
	tag, resp, err := fetch(ctx, url, etag)
	if err != nil {
		return "", err
	}
	if resp.StatusCode == http.StatusNotModified {
		if c.Tag == "" {
			return "", fmt.Errorf("%s: 304 without a cached release", url)
		}
		c.CheckedAt = now
		writeCache(cachePath, c)
		return c.Tag, nil
	}
	writeCache(cachePath, Cache{CheckedAt: now, ETag: resp.Header.Get("ETag"), Tag: tag})
	return tag, nil
}

// semver is a parsed vMAJOR.MINOR.PATCH[-pre][+build].
type semver struct {
	num [3]int
	pre string
}

func parseSemver(v string) (semver, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v, _, _ = strings.Cut(v, "+")
	core, pre, _ := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var s semver
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p == "" {
			return semver{}, false
		}
		s.num[i] = n
	}
	s.pre = pre
	return s, true
}

// Parseable reports whether v is a semantic version ("dev" is not).
func Parseable(v string) bool {
	_, ok := parseSemver(v)
	return ok
}

// Older reports whether installed is an older version than latest. Either one
// unparseable is false. A pre-release is older than its release; two
// pre-releases of one version compare as text.
func Older(installed, latest string) bool {
	a, okA := parseSemver(installed)
	b, okB := parseSemver(latest)
	if !okA || !okB {
		return false
	}
	for i := range a.num {
		if a.num[i] != b.num[i] {
			return a.num[i] < b.num[i]
		}
	}
	switch {
	case a.pre == b.pre:
		return false
	case a.pre == "":
		return false
	case b.pre == "":
		return true
	}
	return a.pre < b.pre
}
