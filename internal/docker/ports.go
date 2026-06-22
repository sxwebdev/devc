package docker

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/moby/moby/api/types/network"
)

// defaultForwardHostIP binds forwarded ports to loopback only, matching the
// secure-local philosophy (services and apps reachable from the host, not the
// LAN). Use an explicit "0.0.0.0:..." form to override.
const defaultForwardHostIP = "127.0.0.1"

// parseForwardPorts converts devcontainer.json "forwardPorts" entries into
// Docker port bindings and the matching exposed-port set.
//
// Accepted entry forms (numbers or strings):
//
//	3000                      -> 127.0.0.1:3000 -> container 3000/tcp
//	"3000"                    -> 127.0.0.1:3000 -> container 3000/tcp
//	"8080:3000"               -> 127.0.0.1:8080 -> container 3000/tcp
//	"127.0.0.1:8080:3000"     -> 127.0.0.1:8080 -> container 3000/tcp
//	"3000/tcp", "5353/udp"    -> protocol suffix honored on the container port
func parseForwardPorts(entries []any) (network.PortMap, network.PortSet, error) {
	if len(entries) == 0 {
		return nil, nil, nil
	}

	bindings := network.PortMap{}
	exposed := network.PortSet{}

	for _, e := range entries {
		hostIP, hostPort, containerPort, proto, err := parseForwardEntry(e)
		if err != nil {
			return nil, nil, err
		}

		port, ok := network.PortFrom(containerPort, proto)
		if !ok {
			return nil, nil, fmt.Errorf("invalid forwardPorts entry %v: bad container port", e)
		}
		addr, addrErr := netip.ParseAddr(hostIP)
		if addrErr != nil {
			return nil, nil, fmt.Errorf("invalid forwardPorts host IP %q: %w", hostIP, addrErr)
		}

		exposed[port] = struct{}{}
		bindings[port] = append(bindings[port], network.PortBinding{
			HostIP:   addr,
			HostPort: strconv.Itoa(int(hostPort)),
		})
	}

	return bindings, exposed, nil
}

// parseForwardEntry normalizes a single forwardPorts entry.
func parseForwardEntry(e any) (hostIP string, hostPort, containerPort uint16, proto network.IPProtocol, err error) {
	hostIP = defaultForwardHostIP
	proto = network.TCP

	switch v := e.(type) {
	case float64:
		p := uint16(v)
		return hostIP, p, p, proto, nil
	case int:
		p := uint16(v)
		return hostIP, p, p, proto, nil
	case string:
		s := strings.TrimSpace(v)
		// Protocol suffix on the container port (e.g. "5353/udp").
		if idx := strings.LastIndex(s, "/"); idx != -1 {
			switch strings.ToLower(s[idx+1:]) {
			case "udp":
				proto = network.UDP
			case "tcp":
				proto = network.TCP
			default:
				return "", 0, 0, proto, fmt.Errorf("invalid forwardPorts protocol in %q", v)
			}
			s = s[:idx]
		}

		parts := strings.Split(s, ":")
		switch len(parts) {
		case 1: // "3000"
			p, perr := parsePort(parts[0])
			if perr != nil {
				return "", 0, 0, proto, fmt.Errorf("invalid forwardPorts entry %q: %w", v, perr)
			}
			return hostIP, p, p, proto, nil
		case 2: // "host:container"
			hp, herr := parsePort(parts[0])
			cp, cerr := parsePort(parts[1])
			if herr != nil || cerr != nil {
				return "", 0, 0, proto, fmt.Errorf("invalid forwardPorts entry %q", v)
			}
			return hostIP, hp, cp, proto, nil
		case 3: // "ip:host:container"
			hp, herr := parsePort(parts[1])
			cp, cerr := parsePort(parts[2])
			if herr != nil || cerr != nil {
				return "", 0, 0, proto, fmt.Errorf("invalid forwardPorts entry %q", v)
			}
			return parts[0], hp, cp, proto, nil
		default:
			return "", 0, 0, proto, fmt.Errorf("invalid forwardPorts entry %q", v)
		}
	default:
		return "", 0, 0, proto, fmt.Errorf("unsupported forwardPorts entry type %T", e)
	}
}

func parsePort(s string) (uint16, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, err
	}
	if n < 1 || n > 65535 {
		return 0, fmt.Errorf("port %d out of range", n)
	}
	return uint16(n), nil
}
