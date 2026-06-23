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
