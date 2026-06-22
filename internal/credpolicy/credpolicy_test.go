package credpolicy

import (
	"testing"

	"github.com/sxwebdev/devc/pkg/types"
)

func TestDecide(t *testing.T) {
	tests := []struct {
		policy               string
		allowAgentCreds      bool
		allowCommonAuth      bool
		allowSSHAgent        bool
		allowCustomEnvPass   bool
		allowHostAgentConfig bool
		filterGitCloud       bool
	}{
		{types.CredentialPolicyNone, false, false, false, false, false, false},
		{types.CredentialPolicyAgentOnly, true, false, false, false, false, true},
		{types.CredentialPolicyDeveloper, true, true, true, true, true, false},
		{types.CredentialPolicyLegacy, true, true, true, true, true, false},
		{"", true, true, true, true, true, false}, // empty == legacy
		{"bogus", true, true, true, true, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.policy, func(t *testing.T) {
			d := Decide(tt.policy)
			if d.AllowAgentCreds != tt.allowAgentCreds {
				t.Errorf("AllowAgentCreds = %v, want %v", d.AllowAgentCreds, tt.allowAgentCreds)
			}
			if d.AllowCommonAuth != tt.allowCommonAuth {
				t.Errorf("AllowCommonAuth = %v, want %v", d.AllowCommonAuth, tt.allowCommonAuth)
			}
			if d.AllowSSHAgent != tt.allowSSHAgent {
				t.Errorf("AllowSSHAgent = %v, want %v", d.AllowSSHAgent, tt.allowSSHAgent)
			}
			if d.AllowCustomEnvPass != tt.allowCustomEnvPass {
				t.Errorf("AllowCustomEnvPass = %v, want %v", d.AllowCustomEnvPass, tt.allowCustomEnvPass)
			}
			if d.AllowHostAgentConfig != tt.allowHostAgentConfig {
				t.Errorf("AllowHostAgentConfig = %v, want %v", d.AllowHostAgentConfig, tt.allowHostAgentConfig)
			}
			if d.FilterGitCloud != tt.filterGitCloud {
				t.Errorf("FilterGitCloud = %v, want %v", d.FilterGitCloud, tt.filterGitCloud)
			}
		})
	}
}

func TestIsGitCloudCredEnv(t *testing.T) {
	gitCloud := []string{"GH_TOKEN", "GITHUB_TOKEN", "GITLAB_TOKEN", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "KUBECONFIG", "GOOGLE_APPLICATION_CREDENTIALS", "azure_client_secret"}
	for _, name := range gitCloud {
		if !IsGitCloudCredEnv(name) {
			t.Errorf("expected %q to be a git/cloud credential", name)
		}
	}

	notCreds := []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "PATH", "HOME", "GEMINI_API_KEY"}
	for _, name := range notCreds {
		if IsGitCloudCredEnv(name) {
			t.Errorf("did not expect %q to be a git/cloud credential", name)
		}
	}
}
