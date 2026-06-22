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
