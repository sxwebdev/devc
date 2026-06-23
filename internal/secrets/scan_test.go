package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root, rel string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScan_DetectsAndAllows(t *testing.T) {
	root := t.TempDir()

	// Protected files that must be detected.
	detected := []string{
		".env",
		".env.local",
		"config.yaml",
		"secrets.yaml",
		".npmrc",
		"service-account.json",
		"firebase-adminsdk-abc.json",
		filepath.Join("backend", ".env"),
	}
	for _, f := range detected {
		writeFile(t, root, f)
	}

	// Allow-listed example files that must NOT be detected.
	allowed := []string{
		".env.example",
		".env.sample",
		"config.example.yaml",
		"app.sample.yaml",
	}
	for _, f := range allowed {
		writeFile(t, root, f)
	}

	// Files inside .git must be ignored even if they look like secrets.
	writeFile(t, root, filepath.Join(".git", ".env"))
	writeFile(t, root, filepath.Join(".git", "config.yaml"))

	// A plainly safe file.
	writeFile(t, root, "main.go")

	findings, err := Scan(root, nil, nil)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	got := make(map[string]bool)
	for _, f := range findings {
		got[filepath.ToSlash(f)] = true
		if strings.HasPrefix(f, ".git") {
			t.Errorf("scan should ignore .git internals, found %q", f)
		}
	}

	want := []string{
		".env", ".env.local", "config.yaml", "secrets.yaml", ".npmrc",
		"service-account.json", "firebase-adminsdk-abc.json", "backend/.env",
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("expected %q to be detected; findings=%v", w, findings)
		}
	}
	for _, a := range allowed {
		if got[filepath.ToSlash(a)] {
			t.Errorf("did not expect allow-listed %q to be detected", a)
		}
	}
	if got["main.go"] {
		t.Error("did not expect main.go to be detected")
	}
}

func TestScan_RelativePathsSorted(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, filepath.Join("z", ".env"))
	writeFile(t, root, ".env")

	findings, err := Scan(root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %v", findings)
	}
	if findings[0] != ".env" {
		t.Errorf("expected sorted, relative paths, got %v", findings)
	}
	for _, f := range findings {
		if filepath.IsAbs(f) {
			t.Errorf("expected relative path, got absolute %q", f)
		}
	}
}

func TestScan_CustomPatterns(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "mysecret.key")
	writeFile(t, root, ".env") // not in custom patterns

	findings, err := Scan(root, []string{"*.key"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0] != "mysecret.key" {
		t.Errorf("expected only mysecret.key, got %v", findings)
	}
}

func TestFormatFailure(t *testing.T) {
	msg := FormatFailure("secure-local-agent", []string{".env", "config.yaml"})
	if !strings.Contains(msg, "secure-local-agent") {
		t.Error("expected preset name in message")
	}
	if !strings.Contains(msg, ".env") || !strings.Contains(msg, "config.yaml") {
		t.Error("expected findings listed in message")
	}
}
