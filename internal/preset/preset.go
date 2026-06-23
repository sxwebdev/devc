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
	"secure-local-agent": secureLocalAgent,
	"secure-local":       secureLocalAgent, // alias
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
// workspace and commits, but host credentials are withheld, in-repo secrets
// block startup, and `git push` is disabled.
func secureLocalAgent() *types.DevcCustomization {
	readonly := true
	return &types.DevcCustomization{
		SecurityProfile:  "moderate",
		CredentialPolicy: types.CredentialPolicyAgentOnly,
		GitPolicy:        types.GitPolicyCommitOnly,
		WorkspaceSecretsPolicy: &types.WorkspaceSecretsPolicy{
			Enabled: true,
			Mode:    types.SecretsModeFail,
		},
		Skills: &types.SkillsConfig{
			Enabled:  true,
			Source:   "~/.agent/skills",
			Target:   "/skills",
			ReadOnly: &readonly,
		},
	}
}
