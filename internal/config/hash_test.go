package config

import (
	"testing"

	"github.com/sxwebdev/devc/pkg/types"
)

func TestConfigHash_Deterministic(t *testing.T) {
	devCfg := &types.DevContainerConfig{Image: "ubuntu:22.04"}
	custom := &types.DevcCustomization{Agent: "claude", SecurityProfile: "moderate"}

	h1 := ConfigHash(devCfg, custom)
	h2 := ConfigHash(devCfg, custom)

	if h1 != h2 {
		t.Errorf("same config should produce same hash: %q != %q", h1, h2)
	}
}

func TestConfigHash_DifferentOnChange(t *testing.T) {
	devCfg := &types.DevContainerConfig{Image: "ubuntu:22.04"}
	custom := &types.DevcCustomization{Agent: "claude", SecurityProfile: "moderate"}

	h1 := ConfigHash(devCfg, custom)

	custom.Agent = "codex"
	h2 := ConfigHash(devCfg, custom)

	if h1 == h2 {
		t.Error("different agent should produce different hash")
	}
}

func TestConfigHash_ImageChange(t *testing.T) {
	custom := &types.DevcCustomization{SecurityProfile: "moderate"}

	h1 := ConfigHash(&types.DevContainerConfig{Image: "ubuntu:22.04"}, custom)
	h2 := ConfigHash(&types.DevContainerConfig{Image: "ubuntu:24.04"}, custom)

	if h1 == h2 {
		t.Error("different image should produce different hash")
	}
}

func TestConfigHash_LegacyUnchanged(t *testing.T) {
	// A config that sets none of the new secure fields must hash identically to
	// before so existing containers are not force-rebuilt on upgrade.
	devCfg := &types.DevContainerConfig{Image: "ubuntu:22.04"}
	custom := &types.DevcCustomization{Agent: "claude", SecurityProfile: "moderate"}

	withEmpty := *custom
	withEmpty.WorkspaceSecretsPolicy = nil
	withEmpty.Skills = nil
	withEmpty.Services = nil

	if ConfigHash(devCfg, custom) != ConfigHash(devCfg, &withEmpty) {
		t.Error("empty new fields must not change the hash")
	}
}

func TestConfigHash_SecretsModeChange(t *testing.T) {
	devCfg := &types.DevContainerConfig{Image: "ubuntu:22.04"}
	base := &types.DevcCustomization{
		SecurityProfile:        "moderate",
		WorkspaceSecretsPolicy: &types.WorkspaceSecretsPolicy{Enabled: true, Mode: "readonly"},
	}
	masked := &types.DevcCustomization{
		SecurityProfile:        "moderate",
		WorkspaceSecretsPolicy: &types.WorkspaceSecretsPolicy{Enabled: true, Mode: "mask"},
	}
	if ConfigHash(devCfg, base) == ConfigHash(devCfg, masked) {
		t.Error("changing workspaceSecretsPolicy.mode must change the hash")
	}
}

func TestConfigHash_NetworkEnforceChange(t *testing.T) {
	devCfg := &types.DevContainerConfig{Image: "ubuntu:22.04"}
	off := &types.DevcCustomization{Network: &types.NetworkConfig{Mode: "restricted"}}
	on := &types.DevcCustomization{Network: &types.NetworkConfig{Mode: "restricted", Enforce: true}}
	if ConfigHash(devCfg, off) == ConfigHash(devCfg, on) {
		t.Error("toggling network.enforce must change the hash")
	}
}

func TestConfigHash_FeatureChange(t *testing.T) {
	custom := &types.DevcCustomization{SecurityProfile: "moderate"}

	cfg1 := &types.DevContainerConfig{
		Image:    "ubuntu:22.04",
		Features: map[string]any{"ghcr.io/devcontainers/features/node:1": map[string]any{}},
	}
	cfg2 := &types.DevContainerConfig{
		Image: "ubuntu:22.04",
	}

	h1 := ConfigHash(cfg1, custom)
	h2 := ConfigHash(cfg2, custom)

	if h1 == h2 {
		t.Error("adding features should change hash")
	}
}
