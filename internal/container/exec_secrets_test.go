package container

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSecretWorkspace creates a temp workspace with a devcontainer.json that
// enables workspaceSecretsPolicy=fail plus a protected .env file, and returns
// the workspace path.
func writeSecretWorkspace(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	dcDir := filepath.Join(ws, ".devcontainer")
	if err := os.MkdirAll(dcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	devcontainer := `{
  "image": "mcr.microsoft.com/devcontainers/base:ubuntu",
  "customizations": {
    "devc": {
      "workspaceSecretsPolicy": { "enabled": true, "mode": "fail" }
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dcDir, "devcontainer.json"), []byte(devcontainer), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".env"), []byte("SECRET=1"), 0o644); err != nil {
		t.Fatal(err)
	}
	return ws
}

// The secrets gate must run before any Docker call, so a Manager with a nil
// Docker client returns the policy error instead of touching Docker.

func TestExec_BlocksOnWorkspaceSecret(t *testing.T) {
	ws := writeSecretWorkspace(t)
	m := &Manager{}
	err := m.Exec(ws, []string{"echo", "hi"})
	if err == nil {
		t.Fatal("expected Exec to be blocked by the secrets policy")
	}
	if !strings.Contains(err.Error(), ".env") {
		t.Errorf("expected error to reference .env, got %v", err)
	}
}

func TestAttach_BlocksOnWorkspaceSecret(t *testing.T) {
	ws := writeSecretWorkspace(t)
	m := &Manager{}
	err := m.Attach(ws, "/bin/bash")
	if err == nil {
		t.Fatal("expected Attach to be blocked by the secrets policy")
	}
	if !strings.Contains(err.Error(), ".env") {
		t.Errorf("expected error to reference .env, got %v", err)
	}
}
