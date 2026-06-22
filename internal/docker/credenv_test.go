package docker

import (
	"strings"
	"testing"

	"github.com/sxwebdev/devc/internal/agent"
	"github.com/sxwebdev/devc/internal/credpolicy"
	"github.com/sxwebdev/devc/pkg/types"
)

func testProfiles() []*agent.Profile {
	return []*agent.Profile{
		{
			Name:           "claude",
			EnvPassthrough: []string{"ANTHROPIC_API_KEY"},
			ResolveCreds: func() *agent.ResolvedCredentials {
				return &agent.ResolvedCredentials{Env: []string{"CLAUDE_CODE_OAUTH_TOKEN=tok"}}
			},
		},
		{
			Name:           "copilot",
			EnvPassthrough: []string{"GITHUB_TOKEN"},
			ResolveCreds: func() *agent.ResolvedCredentials {
				return &agent.ResolvedCredentials{Env: []string{"GH_TOKEN=ghtok"}}
			},
		},
	}
}

func fakeLookup(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func envHas(env []string, key string) bool {
	for _, e := range env {
		if strings.HasPrefix(e, key+"=") {
			return true
		}
	}
	return false
}

func TestBuildCredentialEnv_None(t *testing.T) {
	env := buildCredentialEnv(
		credpolicy.Decide(types.CredentialPolicyNone),
		testProfiles(),
		[]string{"CUSTOM_VAR"},
		fakeLookup(map[string]string{"ANTHROPIC_API_KEY": "k", "GITHUB_TOKEN": "g", "CUSTOM_VAR": "c"}),
		agent.ResolveCredentials,
	)
	if len(env) != 0 {
		t.Errorf("none policy must inject no credentials, got %v", env)
	}
}

func TestBuildCredentialEnv_AgentOnly(t *testing.T) {
	env := buildCredentialEnv(
		credpolicy.Decide(types.CredentialPolicyAgentOnly),
		testProfiles(),
		[]string{"CUSTOM_VAR"},
		fakeLookup(map[string]string{"ANTHROPIC_API_KEY": "k", "GITHUB_TOKEN": "g", "CUSTOM_VAR": "c"}),
		agent.ResolveCredentials,
	)

	if !envHas(env, "ANTHROPIC_API_KEY") {
		t.Error("agentOnly should forward the agent's LLM key")
	}
	if !envHas(env, "CLAUDE_CODE_OAUTH_TOKEN") {
		t.Error("agentOnly should resolve the agent's own token")
	}
	if envHas(env, "GITHUB_TOKEN") || envHas(env, "GH_TOKEN") {
		t.Errorf("agentOnly must strip git/forge credentials, got %v", env)
	}
	if envHas(env, "CUSTOM_VAR") {
		t.Error("agentOnly must not forward project envPassthrough")
	}
}

func TestBuildCredentialEnv_Legacy(t *testing.T) {
	env := buildCredentialEnv(
		credpolicy.Decide(types.CredentialPolicyLegacy),
		testProfiles(),
		[]string{"CUSTOM_VAR"},
		fakeLookup(map[string]string{"ANTHROPIC_API_KEY": "k", "GITHUB_TOKEN": "g", "CUSTOM_VAR": "c"}),
		agent.ResolveCredentials,
	)
	for _, want := range []string{"ANTHROPIC_API_KEY", "GITHUB_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN", "GH_TOKEN", "CUSTOM_VAR"} {
		if !envHas(env, want) {
			t.Errorf("legacy should forward %s, got %v", want, env)
		}
	}
}

func TestBuildCredentialMounts(t *testing.T) {
	profiles := []*agent.Profile{
		{Name: "x", ConfigMounts: []agent.MountSpec{
			{HostPath: ".config/gh", ReadOnly: false}, // host agent config (bind)
			{HostPath: ".claude.json", Copy: true},    // copied, never bind-mounted
		}},
	}
	commonAuth := []agent.MountSpec{
		{HostPath: ".ssh", ReadOnly: true},
		{HostPath: ".gitconfig", ReadOnly: true},
	}
	always := func(string) bool { return true }

	t.Run("none mounts nothing", func(t *testing.T) {
		m := buildCredentialMounts(credpolicy.Decide(types.CredentialPolicyNone), profiles, "/home/h", "/home/vscode", commonAuth, always)
		if len(m) != 0 {
			t.Errorf("none must mount no credentials, got %v", m)
		}
	})

	t.Run("agentOnly withholds ssh and host config", func(t *testing.T) {
		m := buildCredentialMounts(credpolicy.Decide(types.CredentialPolicyAgentOnly), profiles, "/home/h", "/home/vscode", commonAuth, always)
		if len(m) != 0 {
			t.Errorf("agentOnly must not mount ssh/gitconfig/host-agent-config, got %v", m)
		}
	})

	t.Run("legacy mounts ssh, gitconfig and agent config", func(t *testing.T) {
		m := buildCredentialMounts(credpolicy.Decide(types.CredentialPolicyLegacy), profiles, "/home/h", "/home/vscode", commonAuth, always)
		targets := map[string]bool{}
		for _, mm := range m {
			targets[mm.Target] = true
			if mm.Source == "/home/h/.claude.json" {
				t.Error("copy-marked config must not be bind-mounted")
			}
		}
		for _, want := range []string{"/home/vscode/.ssh", "/home/vscode/.gitconfig", "/home/vscode/.config/gh"} {
			if !targets[want] {
				t.Errorf("legacy should mount %s, got targets %v", want, targets)
			}
		}
	})

	t.Run("missing host paths are skipped", func(t *testing.T) {
		none := func(string) bool { return false }
		m := buildCredentialMounts(credpolicy.Decide(types.CredentialPolicyLegacy), profiles, "/home/h", "/home/vscode", commonAuth, none)
		if len(m) != 0 {
			t.Errorf("missing paths should be skipped, got %v", m)
		}
	})
}
