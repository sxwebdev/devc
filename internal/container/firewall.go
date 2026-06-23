package container

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// hostnameRe matches safe DNS hostnames for interpolation into the firewall
// shell script. Anything outside this set is rejected to prevent injection.
var hostnameRe = regexp.MustCompile(`^[a-zA-Z0-9.-]+$`)

// buildFirewallScript renders a root shell script that turns the OUTPUT chain
// into default-DROP, allowing only loopback, established connections, DNS,
// private networks (for sibling services and the Docker resolver), and the
// resolved IPs of the allowlisted domains.
//
// It fails open with a loud warning when iptables is unavailable, because the
// alternative — a container that cannot reach anything — is worse and the
// control is documented as best-effort/experimental.
func buildFirewallScript(domains []string) string {
	// Deduplicate, validate, and sort for determinism.
	seen := make(map[string]bool)
	var safe []string
	for _, d := range domains {
		d = strings.TrimSpace(d)
		if d == "" || seen[d] || !hostnameRe.MatchString(d) {
			continue
		}
		seen[d] = true
		safe = append(safe, d)
	}
	sort.Strings(safe)

	var b strings.Builder
	b.WriteString(`set -e
if ! command -v iptables >/dev/null 2>&1; then
  echo "devc: iptables not found in image; egress enforcement SKIPPED (outbound traffic is NOT restricted)" >&2
  exit 0
fi
iptables -F OUTPUT
iptables -A OUTPUT -o lo -j ACCEPT
iptables -A OUTPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
iptables -A OUTPUT -p udp --dport 53 -j ACCEPT
iptables -A OUTPUT -p tcp --dport 53 -j ACCEPT
for cidr in 127.0.0.0/8 10.0.0.0/8 172.16.0.0/12 192.168.0.0/16; do
  iptables -A OUTPUT -d "$cidr" -j ACCEPT
done
`)
	for _, d := range safe {
		// Resolve each domain to its IPv4 addresses and allow them.
		fmt.Fprintf(&b, `for ip in $(getent ahostsv4 %s 2>/dev/null | awk '{print $1}' | sort -u); do iptables -A OUTPUT -d "$ip" -j ACCEPT; done
`, d)
	}
	b.WriteString("iptables -P OUTPUT DROP\n")
	return b.String()
}

// egressDomains merges the agent profiles' required domains with the project
// allowlist.
func egressDomains(profileDomains, allowlist []string) []string {
	var all []string
	all = append(all, profileDomains...)
	all = append(all, allowlist...)
	return all
}
