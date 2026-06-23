package docker

import (
	"sort"
	"strings"

	"github.com/moby/moby/api/types/mount"

	"github.com/sxwebdev/devc/internal/agent"
	"github.com/sxwebdev/devc/internal/credpolicy"
)

// buildCredentialEnv applies the credential policy to assemble credential and
// passthrough environment variables. lookupEnv and resolveCreds are injected so
// the gating can be unit-tested without touching the host environment or
// Keychain. Static (non-secret) agent EnvVars are handled by the caller.
func buildCredentialEnv(
	cred credpolicy.Decision,
	agentProfiles []*agent.Profile,
	customPassthrough []string,
	lookupEnv func(string) (string, bool),
	resolveCreds func(*agent.Profile) *agent.ResolvedCredentials,
) []string {
	var env []string

	// Host env passthrough (deduplicated, deterministic order).
	passthroughSet := make(map[string]bool)
	if cred.AllowAgentCreds {
		for _, p := range agentProfiles {
			for _, name := range p.EnvPassthrough {
				if cred.FilterGitCloud && credpolicy.IsGitCloudCredEnv(name) {
					continue
				}
				passthroughSet[name] = true
			}
		}
	}
	if cred.AllowCustomEnvPass {
		for _, name := range customPassthrough {
			passthroughSet[name] = true
		}
	}
	names := make([]string, 0, len(passthroughSet))
	for name := range passthroughSet {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if val, ok := lookupEnv(name); ok {
			env = append(env, name+"="+val)
		}
	}

	// Resolved agent credentials (Keychain, credential files, env).
	if cred.AllowAgentCreds {
		credSet := make(map[string]bool)
		for _, p := range agentProfiles {
			creds := resolveCreds(p)
			for _, e := range creds.Env {
				key := strings.SplitN(e, "=", 2)[0]
				if cred.FilterGitCloud && credpolicy.IsGitCloudCredEnv(key) {
					continue
				}
				if !credSet[key] {
					credSet[key] = true
					env = append(env, e)
				}
			}
		}
	}

	return env
}

// buildCredentialMounts returns the host-credential bind mounts permitted by the
// policy: agent config (non-copy) entries and common auth (SSH keys, git
// config). exists is injected for testability. SSH agent socket forwarding is
// handled separately by the caller since it is platform-specific.
func buildCredentialMounts(
	cred credpolicy.Decision,
	agentProfiles []*agent.Profile,
	home, containerHome string,
	commonAuth []agent.MountSpec,
	exists func(string) bool,
) []mount.Mount {
	if home == "" {
		return nil
	}

	var mounts []mount.Mount
	seen := make(map[string]bool)

	if cred.AllowHostAgentConfig {
		for _, p := range agentProfiles {
			for _, m := range p.ConfigMounts {
				if m.Copy {
					continue // copied after start, not bind-mounted
				}
				src := home + "/" + m.HostPath
				if !exists(src) {
					continue
				}
				dst := m.ContainerPath
				if dst == "" {
					dst = containerHome + "/" + m.HostPath
				}
				if seen[dst] {
					continue
				}
				seen[dst] = true
				mounts = append(mounts, mount.Mount{
					Type:     mount.TypeBind,
					Source:   src,
					Target:   dst,
					ReadOnly: m.ReadOnly,
				})
			}
		}
	}

	if cred.AllowCommonAuth {
		for _, m := range commonAuth {
			src := home + "/" + m.HostPath
			if !exists(src) {
				continue
			}
			mounts = append(mounts, mount.Mount{
				Type:     mount.TypeBind,
				Source:   src,
				Target:   containerHome + "/" + m.HostPath,
				ReadOnly: true,
			})
		}
	}

	return mounts
}
