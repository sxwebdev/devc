// Package credpolicy translates a credentialPolicy setting into concrete
// decisions about which host credentials, mounts, and environment variables may
// be injected into an agent container.
//
// The policy is intentionally evaluated in one place so that every injection
// point in the container builder consults the same rules.
package credpolicy

import (
	"strings"

	"github.com/sxwebdev/devc/pkg/types"
)

// Decision captures the per-category outcome of a credential policy.
type Decision struct {
	// AllowAgentCreds permits resolving the agent's own auth (e.g. the Claude
	// OAuth token from the Keychain) and forwarding the agent profile's
	// EnvPassthrough variables.
	AllowAgentCreds bool
	// AllowCommonAuth permits bind-mounting host ~/.ssh and ~/.gitconfig.
	AllowCommonAuth bool
	// AllowSSHAgent permits forwarding SSH_AUTH_SOCK.
	AllowSSHAgent bool
	// AllowCustomEnvPass permits forwarding the host env vars listed in
	// customizations.devc.envPassthrough.
	AllowCustomEnvPass bool
	// AllowHostAgentConfig permits copying the host's full agent config files
	// (e.g. ~/.claude.json, ~/.config/gh) into the container. When false, only
	// minimal/derived config is created in-container.
	AllowHostAgentConfig bool
	// FilterGitCloud strips git/forge/cloud credential env vars even when they
	// originate from an agent profile (e.g. a Copilot GITHUB_TOKEN passthrough).
	FilterGitCloud bool
}

// Normalize returns the effective policy name, mapping the empty string to
// legacy for backwards compatibility.
func Normalize(policy string) string {
	if policy == "" {
		return types.CredentialPolicyLegacy
	}
	return policy
}

// Decide returns the Decision for a credentialPolicy value. Unknown values are
// treated as legacy so that misconfiguration never silently tightens access in
// a surprising way; the secure behavior must be opted into explicitly.
func Decide(policy string) Decision {
	switch Normalize(policy) {
	case types.CredentialPolicyNone:
		return Decision{}
	case types.CredentialPolicyAgentOnly:
		return Decision{
			AllowAgentCreds:      true,
			AllowHostAgentConfig: false,
			FilterGitCloud:       true,
		}
	case types.CredentialPolicyDeveloper:
		return Decision{
			AllowAgentCreds:      true,
			AllowCommonAuth:      true,
			AllowSSHAgent:        true,
			AllowCustomEnvPass:   true,
			AllowHostAgentConfig: true,
		}
	default: // legacy
		return Decision{
			AllowAgentCreds:      true,
			AllowCommonAuth:      true,
			AllowSSHAgent:        true,
			AllowCustomEnvPass:   true,
			AllowHostAgentConfig: true,
		}
	}
}

// IsGitCloudCredEnv reports whether an environment variable name carries a git
// forge or cloud-provider credential that must be withheld under agentOnly.
func IsGitCloudCredEnv(name string) bool {
	upper := strings.ToUpper(name)
	switch upper {
	case "GH_TOKEN", "GITHUB_TOKEN", "GITLAB_TOKEN", "GIT_TOKEN",
		"KUBECONFIG", "GOOGLE_APPLICATION_CREDENTIALS":
		return true
	}
	for _, prefix := range []string{"AWS_", "AZURE_", "GCP_"} {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}
