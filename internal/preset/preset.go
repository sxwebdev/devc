// Package preset provides named bundles of devc customization defaults.
//
// A preset expands into a *types.DevcCustomization that is layered between the
// global defaults and the project-level config: global defaults < preset <
// project. This lets a project opt into a curated secure configuration with a
// single field while still overriding any individual setting explicitly.
package preset

import (
	"sort"

	"github.com/sxwebdev/devc/pkg/types"
)

// builders maps preset names to functions that produce a fresh customization.
// Each call returns a new value so callers can mutate the result safely.
var builders = map[string]func() *types.DevcCustomization{
	"secure-local-agent":  secureLocalAgent,
	"secure-local":        secureLocalAgent, // alias
	"secure-local-strict": secureLocalStrict,
}

// Apply returns the customization for a named preset, or nil if the name is
// unknown or empty.
func Apply(name string) *types.DevcCustomization {
	if name == "" {
		return nil
	}
	if build, ok := builders[name]; ok {
		return build()
	}
	return nil
}

// Exists reports whether a preset name is known.
func Exists(name string) bool {
	_, ok := builders[name]
	return ok
}

// Names returns the canonical preset names, sorted.
func Names() []string {
	names := make([]string, 0, len(builders))
	for n := range builders {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// secureLocalAgent is the curated safe-by-default workflow: the agent edits the
// workspace and commits, but host credentials are withheld, in-repo secrets are
// hidden from the agent (dynamically, via the FUSE filter — the host keeps full
// access and the container always starts), `git push` is disabled, and the agent
// runs without per-edit confirmation prompts because the sandbox is the boundary.
func secureLocalAgent() *types.DevcCustomization {
	readonly := true
	return &types.DevcCustomization{
		SecurityProfile:     "moderate",
		CredentialPolicy:    types.CredentialPolicyAgentOnly,
		GitPolicy:           types.GitPolicyCommitOnly,
		AgentPermissionMode: types.AgentPermissionBypass,
		WorkspaceSecretsPolicy: &types.WorkspaceSecretsPolicy{
			Enabled: true,
			Mode:    types.SecretsModeHide,
		},
		Skills: &types.SkillsConfig{
			Enabled:  true,
			Source:   "~/.agents/skills",
			Target:   "/skills",
			ReadOnly: &readonly,
		},
	}
}

// secureLocalStrict is the maximum-isolation variant of secureLocalAgent: it
// withholds ALL host credentials (including the agent's own — the agent must
// authenticate container-locally) and turns the network allowlist into a real
// enforced egress firewall. Everything else matches secureLocalAgent.
func secureLocalStrict() *types.DevcCustomization {
	c := secureLocalAgent()
	c.CredentialPolicy = types.CredentialPolicyNone
	c.Network = &types.NetworkConfig{
		Mode:    "restricted",
		Enforce: true,
		// Baseline package-registry and source-control egress. Agent profile
		// allowlists (e.g. api.anthropic.com for Claude) are merged on top at
		// firewall-application time via egressDomains.
		Allowlist: []string{
			"api.anthropic.com",
			"registry.npmjs.org",
			"pypi.org",
			"files.pythonhosted.org",
			"proxy.golang.org",
			"github.com",
		},
	}
	return c
}
