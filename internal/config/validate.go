package config

import (
	"fmt"
	"strings"

	"github.com/sxwebdev/devc/internal/agent"
	"github.com/sxwebdev/devc/internal/security"
	"github.com/sxwebdev/devc/pkg/types"
)

// validNetworkModes is the canonical set of network.mode values.
var validNetworkModes = []string{"none", "restricted", "host"}

// validEnum reports whether v is one of allowed. An empty v means "use the
// default" and is always accepted; any other unrecognized value yields an
// actionable error listing the valid options.
func validEnum(field, v string, allowed ...string) error {
	if v == "" {
		return nil
	}
	for _, a := range allowed {
		if v == a {
			return nil
		}
	}
	return fmt.Errorf("unknown %s %q; valid: %s", field, v, strings.Join(allowed, ", "))
}

// ValidateSecurityProfile checks a security-profile name against the profiles
// defined in internal/security (the source of truth, via security.ProfileNames).
func ValidateSecurityProfile(v string) error {
	return validEnum("security profile", v, security.ProfileNames()...)
}

// ValidateNetworkMode checks a network.mode value.
func ValidateNetworkMode(v string) error {
	return validEnum("network mode", v, validNetworkModes...)
}

// ValidateEnums checks the enum-valued fields of a devc customization. Empty
// fields are left to their defaults. Agent names are NOT checked here because
// `up` tolerates an unknown agent by skipping it; use ValidateCustomization when
// an unknown agent should be a hard error (explicit edit/validate operations).
func ValidateEnums(c *types.DevcCustomization) error {
	if c == nil {
		return nil
	}

	if err := ValidateSecurityProfile(c.SecurityProfile); err != nil {
		return err
	}
	if c.Network != nil {
		if err := ValidateNetworkMode(c.Network.Mode); err != nil {
			return err
		}
	}
	if err := validEnum("credential policy", c.CredentialPolicy,
		types.CredentialPolicyNone, types.CredentialPolicyAgentOnly,
		types.CredentialPolicyDeveloper, types.CredentialPolicyLegacy); err != nil {
		return err
	}
	if err := validEnum("git policy", c.GitPolicy,
		types.GitPolicyNone, types.GitPolicyCommitOnly, types.GitPolicyFull); err != nil {
		return err
	}
	if c.WorkspaceSecretsPolicy != nil {
		if err := validEnum("workspace secrets mode", c.WorkspaceSecretsPolicy.Mode,
			types.SecretsModeOff, types.SecretsModeFail, types.SecretsModeMask, types.SecretsModeReadonly); err != nil {
			return err
		}
	}
	if c.Filesystem != nil {
		if err := validEnum("project mount mode", c.Filesystem.ProjectMountMode, "rw", "ro", "overlay"); err != nil {
			return err
		}
	}
	return nil
}

// ValidateCustomization runs ValidateEnums and additionally requires every
// configured agent name to resolve to a known profile. Mistakes are surfaced at
// edit/validate time with a clear message instead of an opaque runtime failure.
func ValidateCustomization(c *types.DevcCustomization) error {
	if err := ValidateEnums(c); err != nil {
		return err
	}
	if c == nil {
		return nil
	}
	for _, name := range c.ResolvedAgents() {
		if agent.GetProfile(name) == nil {
			return fmt.Errorf("unknown agent %q; use 'devc init --list-agents' to see options", name)
		}
	}
	return nil
}
