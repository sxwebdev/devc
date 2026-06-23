package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sxwebdev/devc/internal/preset"
	"github.com/sxwebdev/devc/pkg/types"
)

const (
	DevcDir        = ".devc"
	ConfigFileName = "config.json"
)

// LoadDevcontainerConfig finds and parses devcontainer.json from the workspace.
func LoadDevcontainerConfig(workspaceFolder string) (*types.DevContainerConfig, error) {
	paths := []string{
		filepath.Join(workspaceFolder, ".devcontainer", "devcontainer.json"),
		filepath.Join(workspaceFolder, ".devcontainer.json"),
	}

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var cfg types.DevContainerConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", p, err)
		}
		return &cfg, nil
	}

	return nil, fmt.Errorf("no devcontainer.json found in %s; run 'devc init' to create one", workspaceFolder)
}

// LoadGlobalConfig reads ~/.devc/config.json.
func LoadGlobalConfig() (*types.GlobalConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return defaultGlobalConfig(), nil
	}

	p := filepath.Join(home, DevcDir, ConfigFileName)
	data, err := os.ReadFile(p)
	if err != nil {
		return defaultGlobalConfig(), nil
	}

	var cfg types.GlobalConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing global config: %w", err)
	}
	return &cfg, nil
}

// LoadMerged loads devcontainer.json and the global defaults for a workspace and
// returns the parsed config plus the merged devc customization. It is the single
// entry point for "give me the effective config" used by status/attach/config.
func LoadMerged(workspaceFolder string) (*types.DevContainerConfig, *types.DevcCustomization, error) {
	devCfg, err := LoadDevcontainerConfig(workspaceFolder)
	if err != nil {
		return nil, nil, err
	}
	globalCfg, err := LoadGlobalConfig()
	if err != nil {
		return nil, nil, err
	}
	custom, err := ExtractDevcCustomization(devCfg)
	if err != nil {
		return nil, nil, err
	}
	return devCfg, MergeCustomization(globalCfg, custom), nil
}

// ExtractDevcCustomization pulls the devc-specific customization from devcontainer.json.
func ExtractDevcCustomization(cfg *types.DevContainerConfig) (*types.DevcCustomization, error) {
	if cfg.Customizations == nil {
		return defaultDevcCustomization(), nil
	}

	raw, ok := cfg.Customizations["devc"]
	if !ok {
		return defaultDevcCustomization(), nil
	}

	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}

	var custom types.DevcCustomization
	if err := json.Unmarshal(data, &custom); err != nil {
		return nil, fmt.Errorf("parsing devc customization: %w", err)
	}
	return &custom, nil
}

// MergeCustomization layers configuration sources in order of increasing
// precedence: global defaults < preset < project. The preset is selected by the
// project's "preset" field (falling back to a preset named in the global
// defaults), so a project can adopt a curated bundle of secure defaults and
// still override any individual field explicitly.
func MergeCustomization(global *types.GlobalConfig, project *types.DevcCustomization) *types.DevcCustomization {
	merged := global.Defaults

	// Resolve the effective preset (project takes precedence over global) and
	// layer it between the global defaults and the project config.
	presetName := global.Defaults.Preset
	if project.Preset != "" {
		presetName = project.Preset
	}
	if presetName != "" {
		if base := preset.Apply(presetName); base != nil {
			applyOverrides(&merged, base)
		}
		merged.Preset = presetName
	}

	applyOverrides(&merged, project)

	return &merged
}

// applyOverrides copies every non-empty field from src onto dst. Empty values
// in src leave the corresponding dst field untouched, so layering preserves
// lower-precedence settings that the higher layer doesn't specify.
func applyOverrides(dst *types.DevcCustomization, src *types.DevcCustomization) {
	if len(src.Agents) > 0 {
		dst.Agents = src.Agents
	}
	if src.Agent != "" {
		dst.Agent = src.Agent
	}
	if src.SecurityProfile != "" {
		dst.SecurityProfile = src.SecurityProfile
	}
	if src.Network != nil {
		dst.Network = src.Network
	}
	if src.Resources != nil {
		dst.Resources = src.Resources
	}
	if src.Filesystem != nil {
		dst.Filesystem = src.Filesystem
	}
	if src.Session != nil {
		dst.Session = src.Session
	}
	if src.AgentMounts != nil {
		dst.AgentMounts = src.AgentMounts
	}
	if len(src.EnvPassthrough) > 0 {
		dst.EnvPassthrough = src.EnvPassthrough
	}
	if src.CredentialPolicy != "" {
		dst.CredentialPolicy = src.CredentialPolicy
	}
	if src.WorkspaceSecretsPolicy != nil {
		dst.WorkspaceSecretsPolicy = src.WorkspaceSecretsPolicy
	}
	if src.GitPolicy != "" {
		dst.GitPolicy = src.GitPolicy
	}
	if src.AgentPermissionMode != "" {
		dst.AgentPermissionMode = src.AgentPermissionMode
	}
	if src.Skills != nil {
		dst.Skills = src.Skills
	}
	if src.Services != nil {
		dst.Services = src.Services
	}
}

// ContainerName generates a deterministic container name from the workspace path.
func ContainerName(workspaceFolder string) string {
	abs, err := filepath.Abs(workspaceFolder)
	if err != nil {
		abs = workspaceFolder
	}

	base := filepath.Base(abs)
	hash := sha256.Sum256([]byte(abs))
	short := fmt.Sprintf("%x", hash[:4])

	// Sanitize base name for Docker container naming
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, base)

	return fmt.Sprintf("devc-%s-%s", safe, short)
}

// WorkspaceInContainer returns the workspace mount target path inside the container.
func WorkspaceInContainer(cfg *types.DevContainerConfig, workspaceFolder string) string {
	if cfg.WorkspaceFolder != "" {
		return cfg.WorkspaceFolder
	}
	return filepath.Join("/workspaces", filepath.Base(workspaceFolder))
}

func defaultGlobalConfig() *types.GlobalConfig {
	return &types.GlobalConfig{
		Defaults: *defaultDevcCustomization(),
	}
}

// GlobalConfigPath returns the path to the global config file (~/.devc/config.json).
func GlobalConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, DevcDir, ConfigFileName), nil
}

// DefaultGlobalConfig returns the built-in global defaults applied when no
// ~/.devc/config.json is present.
func DefaultGlobalConfig() *types.GlobalConfig {
	return defaultGlobalConfig()
}

// WriteGlobalConfig writes cfg to ~/.devc/config.json (creating ~/.devc if
// needed) and returns the path. It refuses to overwrite an existing file.
func WriteGlobalConfig(cfg *types.GlobalConfig) (string, error) {
	path, err := GlobalConfigPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err == nil {
		return path, fmt.Errorf("%s already exists; edit it directly", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func defaultDevcCustomization() *types.DevcCustomization {
	return &types.DevcCustomization{
		SecurityProfile: "moderate",
		Network: &types.NetworkConfig{
			Mode: "restricted",
		},
		Resources: &types.ResourceConfig{
			CPUs:      "4",
			Memory:    "8g",
			PidsLimit: 256,
		},
		Filesystem: &types.FilesystemConfig{
			ProjectMountMode: "rw",
		},
		Session: &types.SessionConfig{
			StopOnLastDetach: true,
		},
	}
}
