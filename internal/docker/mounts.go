package docker

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/moby/moby/api/types/mount"

	"github.com/sxwebdev/devc/internal/secrets"
	"github.com/sxwebdev/devc/pkg/types"
)

const (
	defaultSkillsSource = "~/.agent/skills"
	defaultSkillsTarget = "/skills"
)

// resolveSkillsMount builds the read-only skills bind mount and the
// AGENT_SKILLS_DIR env var from the skills config. It returns (nil, "", nil)
// when skills are disabled. A missing source path is a hard error only when
// Required is set; otherwise it warns and skips the mount.
func resolveSkillsMount(cfg *types.SkillsConfig, hostHome string) (*mount.Mount, string, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, "", nil
	}

	source := cfg.Source
	if source == "" {
		source = defaultSkillsSource
	}
	source = expandHome(source, hostHome)

	target := cfg.Target
	if target == "" {
		target = defaultSkillsTarget
	}

	readOnly := true
	if cfg.ReadOnly != nil {
		readOnly = *cfg.ReadOnly
	}

	if _, err := os.Stat(source); err != nil {
		if cfg.Required {
			return nil, "", fmt.Errorf("skills source %q does not exist (required)", source)
		}
		_, _ = fmt.Fprintf(os.Stderr, "warning: skills source %q does not exist; skipping skills mount\n", source)
		return nil, "", nil
	}

	m := &mount.Mount{
		Type:     mount.TypeBind,
		Source:   source,
		Target:   target,
		ReadOnly: readOnly,
	}
	return m, "AGENT_SKILLS_DIR=" + target, nil
}

// readonlySecretMounts returns read-only bind mounts that shadow each protected
// workspace file when workspaceSecretsPolicy.mode is "readonly". The workspace
// itself stays writable; only the matched files are pinned read-only.
func readonlySecretMounts(custom *types.DevcCustomization, workspaceFolder, wsTarget string) ([]mount.Mount, error) {
	sp := custom.WorkspaceSecretsPolicy
	if !secrets.IsEnabled(sp) || sp.Mode != types.SecretsModeReadonly {
		return nil, nil
	}

	findings, err := secrets.Scan(workspaceFolder, sp.Patterns, sp.AllowPatterns)
	if err != nil {
		return nil, fmt.Errorf("scanning workspace secrets: %w", err)
	}

	mounts := make([]mount.Mount, 0, len(findings))
	for _, rel := range findings {
		src := filepath.Join(workspaceFolder, rel)
		// Container paths always use forward slashes regardless of host OS.
		dst := path.Join(wsTarget, filepath.ToSlash(rel))
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   src,
			Target:   dst,
			ReadOnly: true,
		})
	}
	return mounts, nil
}

// maskSecretMounts returns read-only bind mounts that shadow each protected
// workspace file with an empty file when workspaceSecretsPolicy.mode is "mask".
// The agent sees the file as empty rather than seeing its real contents. This is
// a technical control, not a prompt rule. Files created after container start
// are not masked.
func maskSecretMounts(custom *types.DevcCustomization, workspaceFolder, wsTarget string) ([]mount.Mount, error) {
	sp := custom.WorkspaceSecretsPolicy
	if !secrets.IsEnabled(sp) || sp.Mode != types.SecretsModeMask {
		return nil, nil
	}

	findings, err := secrets.Scan(workspaceFolder, sp.Patterns, sp.AllowPatterns)
	if err != nil {
		return nil, fmt.Errorf("scanning workspace secrets: %w", err)
	}
	if len(findings) == 0 {
		return nil, nil
	}

	emptySrc, err := ensureMaskSource()
	if err != nil {
		return nil, fmt.Errorf("preparing mask source: %w", err)
	}

	mounts := make([]mount.Mount, 0, len(findings))
	for _, rel := range findings {
		dst := path.Join(wsTarget, filepath.ToSlash(rel))
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   emptySrc,
			Target:   dst,
			ReadOnly: true,
		})
	}
	return mounts, nil
}

// ensureMaskSource returns the path to a stable empty file used as the source
// for mask bind mounts. It lives under ~/.devc so it persists for the container
// lifetime (a bind mount source must remain present on the host).
func ensureMaskSource() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".devc", "mask")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "empty")
	if _, statErr := os.Stat(p); statErr != nil {
		if writeErr := os.WriteFile(p, []byte{}, 0o444); writeErr != nil {
			return "", writeErr
		}
	}
	return p, nil
}

// netModeAllowsEgressFilter reports whether the effective network mode supports
// an iptables egress filter (i.e. the container has its own network namespace).
func netModeAllowsEgressFilter(custom *types.DevcCustomization, profile *types.SecurityProfile) bool {
	mode := profile.Network.Mode
	if custom.Network != nil && custom.Network.Mode != "" {
		mode = custom.Network.Mode
	}
	return mode != "none" && mode != "host"
}

// appendUnique appends items not already present in s.
func appendUnique(s []string, items ...string) []string {
	seen := make(map[string]bool, len(s))
	for _, x := range s {
		seen[x] = true
	}
	for _, it := range items {
		if !seen[it] {
			seen[it] = true
			s = append(s, it)
		}
	}
	return s
}

// fileExists reports whether a host path exists.
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// expandHome replaces a leading ~ (or ~/) with the given home directory.
func expandHome(p, home string) string {
	if home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}
