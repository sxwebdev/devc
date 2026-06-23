package container

import (
	"strings"
	"testing"
)

func TestBuildFirewallScript(t *testing.T) {
	script := buildFirewallScript([]string{"api.anthropic.com", "claude.ai"})

	checks := []string{
		"command -v iptables",               // fail-open guard
		"iptables -P OUTPUT DROP",           // default deny
		"--dport 53",                        // DNS allowed
		"ESTABLISHED,RELATED",               // established allowed
		"getent ahostsv4 api.anthropic.com", // allowlisted domain resolved
		"getent ahostsv4 claude.ai",         // allowlisted domain resolved
		"127.0.0.0/8",                       // loopback/private allowed
	}
	for _, c := range checks {
		if !strings.Contains(script, c) {
			t.Errorf("firewall script missing %q\n---\n%s", c, script)
		}
	}
}

func TestBuildFirewallScript_RejectsUnsafeDomains(t *testing.T) {
	script := buildFirewallScript([]string{"good.example.com", "evil; rm -rf /", "$(whoami)", ""})

	if !strings.Contains(script, "good.example.com") {
		t.Error("expected safe domain to be allowed")
	}
	// Distinctive content from the rejected domains must not be interpolated.
	for _, bad := range []string{"rm -rf", "whoami"} {
		if strings.Contains(script, bad) {
			t.Errorf("unsafe token %q leaked into firewall script", bad)
		}
	}
}

func TestBuildFirewallScript_DedupAndSort(t *testing.T) {
	script := buildFirewallScript([]string{"b.com", "a.com", "b.com"})
	ai := strings.Index(script, "getent ahostsv4 a.com")
	bi := strings.Index(script, "getent ahostsv4 b.com")
	if ai == -1 || bi == -1 || ai > bi {
		t.Errorf("expected sorted domains a.com before b.com, got a=%d b=%d", ai, bi)
	}
	if strings.Count(script, "getent ahostsv4 b.com") != 1 {
		t.Error("expected b.com deduplicated")
	}
}

func TestEgressDomains(t *testing.T) {
	got := egressDomains([]string{"api.anthropic.com"}, []string{"internal.example.com"})
	if len(got) != 2 {
		t.Fatalf("expected merged domains, got %v", got)
	}
}
