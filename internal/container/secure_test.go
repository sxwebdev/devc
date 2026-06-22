package container

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sxwebdev/devc/pkg/types"
)

func TestGitWrapperScript_BlocksPush(t *testing.T) {
	if !strings.Contains(gitWrapperScript, "git push is disabled") {
		t.Error("git wrapper must reject push with a clear message")
	}
	if !strings.Contains(gitWrapperScript, `"push"`) {
		t.Error("git wrapper must branch on the push subcommand")
	}
	if !strings.Contains(gitWrapperScript, "exec $REAL_GIT") {
		t.Error("git wrapper must delegate non-push commands to the real git")
	}
}

func TestEnforceWorkspaceSecrets(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, ".env"), []byte("X=1"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("off does nothing", func(t *testing.T) {
		c := &types.DevcCustomization{WorkspaceSecretsPolicy: &types.WorkspaceSecretsPolicy{Enabled: true, Mode: types.SecretsModeOff}}
		if err := enforceWorkspaceSecrets(ws, c); err != nil {
			t.Errorf("off mode should not error, got %v", err)
		}
	})

	t.Run("disabled does nothing", func(t *testing.T) {
		c := &types.DevcCustomization{}
		if err := enforceWorkspaceSecrets(ws, c); err != nil {
			t.Errorf("nil policy should not error, got %v", err)
		}
	})

	t.Run("fail blocks on protected file", func(t *testing.T) {
		c := &types.DevcCustomization{
			Preset:                 "secure-local-agent",
			WorkspaceSecretsPolicy: &types.WorkspaceSecretsPolicy{Enabled: true, Mode: types.SecretsModeFail},
		}
		err := enforceWorkspaceSecrets(ws, c)
		if err == nil {
			t.Fatal("expected fail mode to block startup")
		}
		if !strings.Contains(err.Error(), ".env") {
			t.Errorf("expected message to list .env, got %v", err)
		}
	})

	t.Run("mask does not block startup", func(t *testing.T) {
		// mask is applied at mount time, so pre-startup enforcement is a no-op
		// even when protected files are present.
		c := &types.DevcCustomization{WorkspaceSecretsPolicy: &types.WorkspaceSecretsPolicy{Enabled: true, Mode: types.SecretsModeMask}}
		if err := enforceWorkspaceSecrets(ws, c); err != nil {
			t.Errorf("mask mode should not block startup, got %v", err)
		}
	})

	t.Run("fail passes when clean", func(t *testing.T) {
		clean := t.TempDir()
		_ = os.WriteFile(filepath.Join(clean, ".env.example"), []byte("X="), 0o644)
		c := &types.DevcCustomization{WorkspaceSecretsPolicy: &types.WorkspaceSecretsPolicy{Enabled: true, Mode: types.SecretsModeFail}}
		if err := enforceWorkspaceSecrets(clean, c); err != nil {
			t.Errorf("expected clean workspace to pass, got %v", err)
		}
	})
}
