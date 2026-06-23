package docker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sxwebdev/devc/pkg/types"
)

func TestExpandHome(t *testing.T) {
	home := "/home/me"
	cases := map[string]string{
		"~":                "/home/me",
		"~/.agents/skills": "/home/me/.agents/skills",
		"/abs/path":        "/abs/path",
		"relative":         "relative",
	}
	for in, want := range cases {
		if got := expandHome(in, home); got != want {
			t.Errorf("expandHome(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveSkillsMount_Disabled(t *testing.T) {
	m, env, err := resolveSkillsMount(nil, "/home/me")
	if err != nil || m != nil || env != "" {
		t.Errorf("disabled skills should yield nothing, got m=%v env=%q err=%v", m, env, err)
	}

	m, _, err = resolveSkillsMount(&types.SkillsConfig{Enabled: false}, "/home/me")
	if err != nil || m != nil {
		t.Errorf("explicitly disabled skills should yield nothing, got m=%v err=%v", m, err)
	}
}

func TestResolveSkillsMount_DefaultsReadOnly(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "skills")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}

	m, env, err := resolveSkillsMount(&types.SkillsConfig{Enabled: true, Source: src}, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil {
		t.Fatal("expected a mount")
	}
	if !m.ReadOnly {
		t.Error("skills mount should default to read-only")
	}
	if m.Target != "/skills" {
		t.Errorf("expected default target /skills, got %q", m.Target)
	}
	if env != "AGENT_SKILLS_DIR=/skills" {
		t.Errorf("expected AGENT_SKILLS_DIR env, got %q", env)
	}
}

func TestResolveSkillsMount_MissingOptionalSkips(t *testing.T) {
	m, env, err := resolveSkillsMount(&types.SkillsConfig{Enabled: true, Source: "/nonexistent/skills/path"}, "/home/me")
	if err != nil {
		t.Errorf("missing optional source should not error, got %v", err)
	}
	if m != nil || env != "" {
		t.Errorf("missing optional source should skip mount, got m=%v env=%q", m, env)
	}
}

func TestResolveSkillsMount_MissingRequiredErrors(t *testing.T) {
	_, _, err := resolveSkillsMount(&types.SkillsConfig{Enabled: true, Source: "/nonexistent/skills/path", Required: true}, "/home/me")
	if err == nil {
		t.Error("missing required source should error")
	}
}

func TestReadonlySecretMounts(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, ".env"), []byte("X=1"), 0o644); err != nil {
		t.Fatal(err)
	}

	custom := &types.DevcCustomization{
		WorkspaceSecretsPolicy: &types.WorkspaceSecretsPolicy{
			Enabled: true,
			Mode:    types.SecretsModeReadonly,
		},
	}

	mounts, err := readonlySecretMounts(custom, ws, "/workspaces/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 1 {
		t.Fatalf("expected 1 read-only mount, got %d", len(mounts))
	}
	if !mounts[0].ReadOnly {
		t.Error("secret mount must be read-only")
	}
	if mounts[0].Target != "/workspaces/app/.env" {
		t.Errorf("unexpected target %q", mounts[0].Target)
	}
}

func TestMaskSecretMounts(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, ".env"), []byte("SECRET=1"), 0o644); err != nil {
		t.Fatal(err)
	}

	custom := &types.DevcCustomization{
		WorkspaceSecretsPolicy: &types.WorkspaceSecretsPolicy{Enabled: true, Mode: types.SecretsModeMask},
	}

	mounts, err := maskSecretMounts(custom, ws, "/workspaces/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 1 {
		t.Fatalf("expected 1 mask mount, got %d", len(mounts))
	}
	m := mounts[0]
	if !m.ReadOnly {
		t.Error("mask mount must be read-only")
	}
	if m.Target != "/workspaces/app/.env" {
		t.Errorf("unexpected target %q", m.Target)
	}
	// The source must be an existing, empty file (not the real secret).
	if m.Source == filepath.Join(ws, ".env") {
		t.Error("mask source must not be the real secret file")
	}
	info, statErr := os.Stat(m.Source)
	if statErr != nil {
		t.Fatalf("mask source should exist: %v", statErr)
	}
	if info.Size() != 0 {
		t.Errorf("mask source should be empty, got size %d", info.Size())
	}
}

func TestMaskSecretMounts_OnlyForMaskMode(t *testing.T) {
	ws := t.TempDir()
	_ = os.WriteFile(filepath.Join(ws, ".env"), []byte("X=1"), 0o644)

	custom := &types.DevcCustomization{
		WorkspaceSecretsPolicy: &types.WorkspaceSecretsPolicy{Enabled: true, Mode: types.SecretsModeFail},
	}
	mounts, err := maskSecretMounts(custom, ws, "/workspaces/app")
	if err != nil {
		t.Fatal(err)
	}
	if mounts != nil {
		t.Errorf("expected no mounts for fail mode, got %v", mounts)
	}
}

func TestReadonlySecretMounts_OnlyForReadonlyMode(t *testing.T) {
	ws := t.TempDir()
	_ = os.WriteFile(filepath.Join(ws, ".env"), []byte("X=1"), 0o644)

	custom := &types.DevcCustomization{
		WorkspaceSecretsPolicy: &types.WorkspaceSecretsPolicy{Enabled: true, Mode: types.SecretsModeFail},
	}
	mounts, err := readonlySecretMounts(custom, ws, "/workspaces/app")
	if err != nil {
		t.Fatal(err)
	}
	if mounts != nil {
		t.Errorf("expected no mounts for fail mode, got %v", mounts)
	}
}
