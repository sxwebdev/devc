package docker

import (
	"testing"

	"github.com/moby/moby/api/types/network"
)

func TestParseForwardPorts(t *testing.T) {
	entries := []any{
		float64(3000),         // number -> 127.0.0.1:3000 -> 3000/tcp
		"8080:3000",           // host:container
		"127.0.0.1:5433:5432", // ip:host:container
		"5353/udp",            // protocol suffix
	}

	bindings, exposed, err := parseForwardPorts(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Distinct container ports: 3000/tcp, 5432/tcp, 5353/udp (the 3000 and
	// 8080:3000 entries share the 3000/tcp key with two host bindings).
	if len(bindings) != 3 {
		t.Fatalf("expected 3 container-port keys, got %d", len(bindings))
	}

	p3000, _ := network.PortFrom(3000, network.TCP)
	// 3000 (direct) and 8080 (mapped) both target container 3000/tcp.
	if len(bindings[p3000]) != 2 {
		t.Errorf("expected container 3000 to have 2 host bindings (3000 and 8080), got %d", len(bindings[p3000]))
	}
	hostPorts := map[string]bool{}
	for _, b := range bindings[p3000] {
		hostPorts[b.HostPort] = true
		if b.HostIP.String() != "127.0.0.1" {
			t.Errorf("expected loopback bind, got %s", b.HostIP)
		}
	}
	if !hostPorts["3000"] || !hostPorts["8080"] {
		t.Errorf("expected host ports 3000 and 8080, got %v", hostPorts)
	}

	p5432, _ := network.PortFrom(5432, network.TCP)
	if b := bindings[p5432]; len(b) != 1 || b[0].HostPort != "5433" {
		t.Errorf("unexpected 5432 binding: %+v", b)
	}

	p5353udp, _ := network.PortFrom(5353, network.UDP)
	if _, ok := exposed[p5353udp]; !ok {
		t.Error("expected udp 5353 to be exposed")
	}
}

func TestParseForwardPorts_Empty(t *testing.T) {
	bindings, exposed, err := parseForwardPorts(nil)
	if err != nil || bindings != nil || exposed != nil {
		t.Errorf("expected empty result, got bindings=%v exposed=%v err=%v", bindings, exposed, err)
	}
}

func TestParseForwardPorts_Invalid(t *testing.T) {
	for _, bad := range []any{"notaport", "70000", "1:2:3:4", "8080:bad", true} {
		if _, _, err := parseForwardPorts([]any{bad}); err == nil {
			t.Errorf("expected error for %v", bad)
		}
	}
}
