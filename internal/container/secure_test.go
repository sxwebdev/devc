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
	// The wrapper must resolve the subcommand past leading global options so
	// `git -C dir push` / `git -c k=v push` cannot bypass the block.
	if !strings.Contains(gitWrapperScript, "--git-dir") || !strings.Contains(gitWrapperScript, "skip=1") {
		t.Error("git wrapper must skip leading global options when finding the subcommand")
	}
	// Idempotent: the real binary is preserved at a stable path and reused.
	if !strings.Contains(gitWrapperScript, "/usr/local/bin/git.real") {
		t.Error("git wrapper installer must preserve the real binary at a stable path")
	}
	// The wrapper carries a marker so the installer can tell the real git apart
	// from a previously-installed wrapper when both live at /usr/local/bin/git
	// (as in the agent-dev-base image), instead of bailing out.
	if !strings.Contains(gitWrapperScript, "devc-git-wrapper") {
		t.Error("git wrapper must embed an identifying marker")
	}
	if !strings.Contains(gitWrapperScript, `grep -q "devc-git-wrapper" /usr/local/bin/git`) {
		t.Error("installer must use the marker to detect an already-installed wrapper")
	}
}

func TestServiceKeyValidation(t *testing.T) {
	valid := []string{"postgres", "redis", "my-db", "pg2"}
	for _, s := range valid {
		if !serviceKeyRe.MatchString(s) {
			t.Errorf("expected %q to be a valid service key", s)
		}
	}
	invalid := []string{"My_DB", "pg_main", "Postgres", "-bad", "bad-", "a b", ""}
	for _, s := range invalid {
		if serviceKeyRe.MatchString(s) {
			t.Errorf("expected %q to be rejected as a service key", s)
		}
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

	t.Run("hide does not block startup", func(t *testing.T) {
		// hide is enforced live by the FUSE filter, so startup is never blocked
		// even when protected files are present.
		c := &types.DevcCustomization{WorkspaceSecretsPolicy: &types.WorkspaceSecretsPolicy{Enabled: true, Mode: types.SecretsModeHide}}
		if err := enforceWorkspaceSecrets(ws, c); err != nil {
			t.Errorf("hide mode should not block startup, got %v", err)
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
