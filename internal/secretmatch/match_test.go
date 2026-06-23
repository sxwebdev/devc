package secretmatch

import (
	"testing"

	"github.com/sxwebdev/devc/internal/secrets"
)

func TestMatchDefaults(t *testing.T) {
	m := New(secrets.DefaultPatterns(), secrets.DefaultAllowPatterns())

	deny := []string{
		"config.yaml",            // literal at root
		"internal/config.yaml",   // literal nested (base-name match)
		"a/b/c/secrets.yml",      // deep nested
		"id_rsa",                 // ssh key
		"deploy/prod.pem",        // *.pem nested
		".env",                   // dotfile
		"sub/.env.production",    // .env.* nested
		"pkg/app.secret.json",    // *.secret.*
		"x/private-data.json",    // private*.json
		"svc/my-credentials.txt", // *-credentials.*
	}
	for _, p := range deny {
		if !m.Match(p) {
			t.Errorf("expected %q to be hidden (denied), but it was not", p)
		}
	}

	allow := []string{
		"config.example.yaml",
		"internal/.env.example",
		"main.go",
		"README.md",
		"src/index.ts",
		"deploy/values.yaml",
		"config.sample.yml",
	}
	for _, p := range allow {
		if m.Match(p) {
			t.Errorf("expected %q to be visible (not denied), but it was hidden", p)
		}
	}
}

func TestMatchDoublestarPaths(t *testing.T) {
	m := New([]string{"internal/**/*.key", "build/secret.txt"}, nil)

	cases := map[string]bool{
		"internal/a/b/server.key": true,
		"internal/server.key":     true, // doublestar ** matches zero intermediate dirs
		"build/secret.txt":        true,
		"build/sub/secret.txt":    false, // exact path, not nested
		"other/server.key":        false,
		"main.go":                 false,
	}
	for p, want := range cases {
		if got := m.Match(p); got != want {
			t.Errorf("Match(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestEmptyAndDot(t *testing.T) {
	m := New([]string{"*"}, nil)
	if m.Match("") || m.Match(".") || m.Match("/") {
		t.Error("empty/root paths must never be treated as secrets")
	}
}
