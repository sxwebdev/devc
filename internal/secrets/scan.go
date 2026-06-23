// Package secrets scans a workspace for credential-bearing files that should
// not be exposed to an AI agent (e.g. .env, secrets.yaml, service-account JSON).
//
// Matching is intentionally simple: each pattern is a shell-style glob compared
// against a file's base name. Example files (.env.example and friends) are
// excluded via the allow list. The .git directory is always skipped.
package secrets

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sxwebdev/devc/pkg/types"
)

// defaultPatterns lists files that commonly carry secrets. It mirrors the
// patterns documented for workspaceSecretsPolicy.
var defaultPatterns = []string{
	".env", ".env.*", "*.env",
	"config.yaml", "config.yml",
	"secrets.yaml", "secrets.yml", "secret.yaml", "secret.yml",
	"credentials.json",
	"service-account*.json", "*service-account*.json",
	"firebase-adminsdk*.json", "google-credentials*.json",
	".npmrc", ".pypirc", ".netrc", ".envrc",
	"application-local.yml", "application-local.yaml",
	"application-secrets.yml", "application-secrets.yaml",
	"*.local.yaml", "*.local.yml",
	"*.secret.*",
	"private*.json",
	"*-credentials.*",
	"*.pem", "id_rsa", "id_ed25519",
}

// defaultAllowPatterns lists example/sample files that look like secrets but are
// safe to expose.
var defaultAllowPatterns = []string{
	".env.example", ".env.sample", ".env.template",
	"*.example.env", "*.sample.env", "*.template.env",
	"config.example.yaml", "config.example.yml",
	"config.sample.yaml", "config.sample.yml",
	"*.example.yaml", "*.example.yml",
	"*.sample.yaml", "*.sample.yml",
}

// DefaultPatterns returns a copy of the built-in protected-file patterns.
func DefaultPatterns() []string { return append([]string(nil), defaultPatterns...) }

// DefaultAllowPatterns returns a copy of the built-in allow-list patterns.
func DefaultAllowPatterns() []string { return append([]string(nil), defaultAllowPatterns...) }

// Scan walks workspaceFolder and returns the relative paths of protected files,
// sorted. Patterns/allowPatterns fall back to the built-in defaults when empty.
func Scan(workspaceFolder string, patterns, allowPatterns []string) ([]string, error) {
	if len(patterns) == 0 {
		patterns = defaultPatterns
	}
	if len(allowPatterns) == 0 {
		allowPatterns = defaultAllowPatterns
	}

	var findings []string
	err := filepath.WalkDir(workspaceFolder, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable entries rather than aborting the scan
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		base := d.Name()
		if matchesAny(base, allowPatterns) {
			return nil
		}
		if matchesAny(base, patterns) {
			rel, relErr := filepath.Rel(workspaceFolder, path)
			if relErr != nil {
				rel = path
			}
			findings = append(findings, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(findings)
	return findings, nil
}

// matchesAny reports whether name matches any glob pattern (by base name).
func matchesAny(name string, patterns []string) bool {
	for _, p := range patterns {
		if ok, err := filepath.Match(p, name); err == nil && ok {
			return true
		}
	}
	return false
}

// FormatFailure builds the user-facing message printed when mode=fail blocks a
// startup because protected files are present.
func FormatFailure(presetName string, findings []string) string {
	label := presetName
	if label == "" {
		label = "secure workspace"
	}
	var b strings.Builder
	b.WriteString("Refusing to start ")
	b.WriteString(label)
	b.WriteString(" because protected workspace files would be visible to the agent:\n\n")
	for _, f := range findings {
		b.WriteString("- ")
		b.WriteString(f)
		b.WriteString("\n")
	}
	b.WriteString("\nMove secrets outside the repository, add safe example files instead, " +
		"or set customizations.devc.workspaceSecretsPolicy.mode to \"off\" or \"readonly\" " +
		"if you understand the risk.")
	return b.String()
}

// IsEnabled reports whether a workspace secrets policy is active.
func IsEnabled(p *types.WorkspaceSecretsPolicy) bool {
	return p != nil && p.Enabled
}
