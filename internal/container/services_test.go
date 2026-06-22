package container

import (
	"strings"
	"testing"

	"github.com/sxwebdev/devc/pkg/types"
)

func secureServices() *types.DevcCustomization {
	return &types.DevcCustomization{
		Services: map[string]*types.ServiceConfig{
			"postgres": {
				Enabled:  true,
				Image:    "postgres:16",
				HostPort: 54321,
				HostIP:   "127.0.0.1",
				Env:      map[string]string{"POSTGRES_USER": "app", "POSTGRES_PASSWORD": "app", "POSTGRES_DB": "app"},
				Volumes:  []types.ServiceVolume{{Name: "pgdata", Target: "/var/lib/postgresql/data"}},
			},
			"redis": {
				Enabled:  true,
				Image:    "redis:7",
				HostPort: 63791,
				HostIP:   "127.0.0.1",
			},
			"disabled": {Enabled: false, Image: "nginx"},
		},
	}
}

func TestServicesEnabled(t *testing.T) {
	if servicesEnabled(&types.DevcCustomization{}) {
		t.Error("no services should be disabled")
	}
	if !servicesEnabled(secureServices()) {
		t.Error("expected services enabled")
	}
}

func TestBuildServiceSpecs(t *testing.T) {
	custom := secureServices()
	specs := buildServiceSpecs(custom, "devc-app-abcd1234", "devc-net-devc-app-abcd1234")

	if len(specs) != 2 {
		t.Fatalf("expected 2 enabled services, got %d", len(specs))
	}

	byAlias := map[string]int{}
	for i, s := range specs {
		byAlias[s.Alias] = i
		if s.Parent != "devc-app-abcd1234" {
			t.Errorf("expected parent label, got %q", s.Parent)
		}
		if s.NetworkName != "devc-net-devc-app-abcd1234" {
			t.Errorf("unexpected network %q", s.NetworkName)
		}
		if s.ContainerName != "devc-app-abcd1234-"+s.Alias {
			t.Errorf("unexpected container name %q", s.ContainerName)
		}
	}

	pg := specs[byAlias["postgres"]]
	if pg.ContainerPort != 5432 {
		t.Errorf("expected default postgres port 5432, got %d", pg.ContainerPort)
	}
	if pg.HostPort != 54321 {
		t.Errorf("expected host port 54321, got %d", pg.HostPort)
	}
	if len(pg.Volumes) != 1 || pg.Volumes[0].VolumeName != "devc-app-abcd1234-pgdata" {
		t.Errorf("expected namespaced volume, got %+v", pg.Volumes)
	}

	redis := specs[byAlias["redis"]]
	if redis.ContainerPort != 6379 {
		t.Errorf("expected default redis port 6379, got %d", redis.ContainerPort)
	}
}

func TestServiceEnv(t *testing.T) {
	env := serviceEnv(secureServices())
	joined := strings.Join(env, "\n")

	if !strings.Contains(joined, "DATABASE_URL=postgresql://app:app@postgres:5432/app") {
		t.Errorf("unexpected DATABASE_URL in %v", env)
	}
	if !strings.Contains(joined, "REDIS_URL=redis://redis:6379") {
		t.Errorf("unexpected REDIS_URL in %v", env)
	}
}

func TestServiceEnv_AgentEnvOverride(t *testing.T) {
	custom := &types.DevcCustomization{
		Services: map[string]*types.ServiceConfig{
			"postgres": {
				Enabled:  true,
				Image:    "postgres:16",
				AgentEnv: map[string]string{"PG_DSN": "postgres://app@postgres:5432/app?sslmode=disable"},
			},
		},
	}
	env := serviceEnv(custom)
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "PG_DSN=postgres://app@postgres:5432/app?sslmode=disable") {
		t.Errorf("expected agentEnv override, got %v", env)
	}
	if strings.Contains(joined, "DATABASE_URL=") {
		t.Errorf("agentEnv override should replace the default DATABASE_URL, got %v", env)
	}
}

func TestServiceNetworkName(t *testing.T) {
	if got := serviceNetworkName("devc-app-abcd1234"); got != "devc-net-devc-app-abcd1234" {
		t.Errorf("unexpected network name %q", got)
	}
}
