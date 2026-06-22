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

func TestServiceEnv_WellKnownServices(t *testing.T) {
	custom := &types.DevcCustomization{
		Services: map[string]*types.ServiceConfig{
			"rabbitmq": {Enabled: true, Image: "rabbitmq:3", Env: map[string]string{"RABBITMQ_DEFAULT_USER": "u", "RABBITMQ_DEFAULT_PASS": "p"}},
			"nats":     {Enabled: true, Image: "nats:2"},
			"kafka":    {Enabled: true, Image: "bitnami/kafka"},
			"mongo":    {Enabled: true, Image: "mongo:7", Env: map[string]string{"MONGO_INITDB_ROOT_USERNAME": "root", "MONGO_INITDB_ROOT_PASSWORD": "secret"}},
			"mysql":    {Enabled: true, Image: "mysql:8", Env: map[string]string{"MYSQL_USER": "app", "MYSQL_PASSWORD": "app", "MYSQL_DATABASE": "app"}},
		},
	}
	joined := strings.Join(serviceEnv(custom), "\n")

	wants := []string{
		"AMQP_URL=amqp://u:p@rabbitmq:5672/",
		"NATS_URL=nats://nats:4222",
		"KAFKA_BROKERS=kafka:9092",
		"MONGO_URL=mongodb://root:secret@mongo:27017",
		"DATABASE_URL=mysql://app:app@mysql:3306/app",
	}
	for _, w := range wants {
		if !strings.Contains(joined, w) {
			t.Errorf("expected %q in service env, got:\n%s", w, joined)
		}
	}
}

func TestContainerPortFor_Defaults(t *testing.T) {
	cases := map[string]int{"postgres": 5432, "rabbitmq": 5672, "nats": 4222, "kafka": 9092, "mongo": 27017}
	for name, want := range cases {
		if got := containerPortFor(name, &types.ServiceConfig{}); got != want {
			t.Errorf("default port for %s = %d, want %d", name, got, want)
		}
	}
	// Explicit port wins over the default.
	if got := containerPortFor("postgres", &types.ServiceConfig{ContainerPort: 6000}); got != 6000 {
		t.Errorf("explicit port should win, got %d", got)
	}
	// Unknown service without a port has no default.
	if got := containerPortFor("custombroker", &types.ServiceConfig{}); got != 0 {
		t.Errorf("unknown service should have no default port, got %d", got)
	}
}

func TestServiceNetworkName(t *testing.T) {
	if got := serviceNetworkName("devc-app-abcd1234"); got != "devc-net-devc-app-abcd1234" {
		t.Errorf("unexpected network name %q", got)
	}
}
