// Package secretmatch decides whether a workspace-relative path is a protected
// secret, given gitignore-style deny and allow patterns.
//
// It is the shared matching logic between devc (which passes the patterns into
// the container) and the devc-secretfs FUSE helper (which evaluates them live on
// every filesystem lookup, so files created at any time and any depth are
// hidden from the agent).
//
// Semantics, kept compatible with internal/secrets:
//   - A pattern without "/" or "**" is matched against the path's BASE NAME, so
//     e.g. "config.yaml" or "*.pem" hides matches at any depth.
//   - A pattern containing "/" or "**" is matched against the full relative path
//     with doublestar (so "internal/*.secret.yaml" and "**/dist/*.key" work). A
//     path glob without a leading "/" or "**/" is also tried with a "**/" prefix
//     so it can match at any depth.
package secretmatch

import (
	"path"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Matcher evaluates deny/allow patterns against relative paths.
type Matcher struct {
	deny  []string
	allow []string
}

// New returns a Matcher for the given deny and allow pattern lists.
func New(deny, allow []string) *Matcher {
	return &Matcher{deny: deny, allow: allow}
}

// Match reports whether rel (a slash-separated, workspace-relative path) is a
// protected secret: it matches a deny pattern and no allow pattern.
func (m *Matcher) Match(rel string) bool {
	rel = strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(rel, "/")), "/")
	if rel == "" || rel == "." {
		return false
	}
	if matchAny(rel, m.allow) {
		return false
	}
	return matchAny(rel, m.deny)
}

func matchAny(rel string, patterns []string) bool {
	base := path.Base(rel)
	for _, p := range patterns {
		if p == "" {
			continue
		}
		if strings.Contains(p, "/") || strings.Contains(p, "**") {
			if ok, err := doublestar.Match(p, rel); err == nil && ok {
				return true
			}
			if !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "**/") {
				if ok, err := doublestar.Match("**/"+p, rel); err == nil && ok {
					return true
				}
			}
		} else if ok, err := doublestar.Match(p, base); err == nil && ok {
			return true
		}
	}
	return false
}
