package preset

import (
	"testing"

	"github.com/sxwebdev/devc/pkg/types"
)

func TestApply_SecureLocalAgent(t *testing.T) {
	for _, name := range []string{"secure-local-agent", "secure-local"} {
		c := Apply(name)
		if c == nil {
			t.Fatalf("Apply(%q) returned nil", name)
		}
		if c.CredentialPolicy != types.CredentialPolicyAgentOnly {
			t.Errorf("credentialPolicy = %q, want agentOnly", c.CredentialPolicy)
		}
		if c.GitPolicy != types.GitPolicyCommitOnly {
			t.Errorf("gitPolicy = %q, want commitOnly", c.GitPolicy)
		}
		if c.WorkspaceSecretsPolicy == nil || !c.WorkspaceSecretsPolicy.Enabled || c.WorkspaceSecretsPolicy.Mode != types.SecretsModeFail {
			t.Errorf("workspaceSecretsPolicy = %+v, want enabled fail", c.WorkspaceSecretsPolicy)
		}
		if c.Skills == nil || !c.Skills.Enabled || c.Skills.Target != "/skills" {
			t.Errorf("skills = %+v, want enabled /skills", c.Skills)
		}
		if c.Skills.ReadOnly == nil || !*c.Skills.ReadOnly {
			t.Error("skills should be read-only")
		}
	}
}

func TestApply_SecureLocalStrict(t *testing.T) {
	c := Apply("secure-local-strict")
	if c == nil {
		t.Fatal("Apply(secure-local-strict) returned nil")
	}
	if c.CredentialPolicy != types.CredentialPolicyNone {
		t.Errorf("credentialPolicy = %q, want none", c.CredentialPolicy)
	}
	// Inherits the rest of the secure-local-agent posture.
	if c.GitPolicy != types.GitPolicyCommitOnly {
		t.Errorf("gitPolicy = %q, want commitOnly", c.GitPolicy)
	}
	if c.WorkspaceSecretsPolicy == nil || !c.WorkspaceSecretsPolicy.Enabled || c.WorkspaceSecretsPolicy.Mode != types.SecretsModeFail {
		t.Errorf("workspaceSecretsPolicy = %+v, want enabled fail", c.WorkspaceSecretsPolicy)
	}
	// Enforced egress firewall with a non-empty allowlist.
	if c.Network == nil {
		t.Fatal("expected a network config")
	}
	if !c.Network.Enforce {
		t.Error("strict preset must enforce the egress firewall")
	}
	if len(c.Network.Allowlist) == 0 {
		t.Error("strict preset must ship a non-empty allowlist")
	}
}

func TestApply_Unknown(t *testing.T) {
	if Apply("nope") != nil {
		t.Error("expected nil for unknown preset")
	}
	if Apply("") != nil {
		t.Error("expected nil for empty preset")
	}
}

func TestExistsAndNames(t *testing.T) {
	if !Exists("secure-local-agent") {
		t.Error("expected secure-local-agent to exist")
	}
	if Exists("nope") {
		t.Error("did not expect nope to exist")
	}
	if len(Names()) == 0 {
		t.Error("expected at least one preset name")
	}
}
