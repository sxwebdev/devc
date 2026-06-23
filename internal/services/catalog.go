// Package services holds a catalog of ready-to-use sibling service definitions
// (Postgres, Redis, etc.) that `devc init --services` and `devc service add`
// scaffold into a devcontainer.json. It is the single source of truth for those
// templates so the two entry points never drift.
//
// This is scaffolding-time data only. Runtime port/connection-string resolution
// lives separately in internal/container/services_known.go.
package services

import (
	"sort"

	"github.com/sxwebdev/devc/pkg/types"
)

// catalog maps a canonical service key to a constructor that returns a fresh,
// independently-owned ServiceConfig. Each call allocates new Env maps and Volume
// slices so callers can mutate the result without corrupting the template.
//
// Image tags track the latest stable major of each project. Host ports are the
// native port shifted into a high, unlikely-to-collide range (so a service
// running natively on the host does not clash with the published port), and are
// hardcoded per entry because a naive "append a digit" scheme overflows for
// high native ports.
//
// kafka, elasticsearch, and opensearch are heavier and carry the extra env they
// need to boot single-node; users may still need to tune memory.
var catalog = map[string]func() *types.ServiceConfig{
	"postgres": func() *types.ServiceConfig {
		return &types.ServiceConfig{
			Enabled:       true,
			Image:         "postgres:18",
			ContainerPort: 5432,
			HostPort:      54321,
			HostIP:        "127.0.0.1",
			Env: map[string]string{
				"POSTGRES_USER":     "app",
				"POSTGRES_PASSWORD": "app",
				"POSTGRES_DB":       "app",
			},
			Volumes: []types.ServiceVolume{
				{Name: "postgres-data", Target: "/var/lib/postgresql/data"},
			},
		}
	},
	"redis": func() *types.ServiceConfig {
		return &types.ServiceConfig{
			Enabled:       true,
			Image:         "redis:8",
			ContainerPort: 6379,
			HostPort:      63791,
			HostIP:        "127.0.0.1",
			Volumes: []types.ServiceVolume{
				{Name: "redis-data", Target: "/data"},
			},
		}
	},
	"valkey": func() *types.ServiceConfig {
		return &types.ServiceConfig{
			Enabled:       true,
			Image:         "valkey/valkey:9",
			ContainerPort: 6379,
			HostPort:      63792,
			HostIP:        "127.0.0.1",
			Volumes: []types.ServiceVolume{
				{Name: "valkey-data", Target: "/data"},
			},
		}
	},
	"mysql": func() *types.ServiceConfig {
		return &types.ServiceConfig{
			Enabled:       true,
			Image:         "mysql:9",
			ContainerPort: 3306,
			HostPort:      33061,
			HostIP:        "127.0.0.1",
			Env: map[string]string{
				"MYSQL_ROOT_PASSWORD": "app",
				"MYSQL_DATABASE":      "app",
				"MYSQL_USER":          "app",
				"MYSQL_PASSWORD":      "app",
			},
			Volumes: []types.ServiceVolume{
				{Name: "mysql-data", Target: "/var/lib/mysql"},
			},
		}
	},
	"mariadb": func() *types.ServiceConfig {
		return &types.ServiceConfig{
			Enabled:       true,
			Image:         "mariadb:12",
			ContainerPort: 3306,
			HostPort:      33062,
			HostIP:        "127.0.0.1",
			Env: map[string]string{
				"MARIADB_ROOT_PASSWORD": "app",
				"MARIADB_DATABASE":      "app",
				"MARIADB_USER":          "app",
				"MARIADB_PASSWORD":      "app",
			},
			Volumes: []types.ServiceVolume{
				{Name: "mariadb-data", Target: "/var/lib/mysql"},
			},
		}
	},
	"mongo": func() *types.ServiceConfig {
		return &types.ServiceConfig{
			Enabled:       true,
			Image:         "mongo:8",
			ContainerPort: 27017,
			HostPort:      27018,
			HostIP:        "127.0.0.1",
			Env: map[string]string{
				"MONGO_INITDB_ROOT_USERNAME": "app",
				"MONGO_INITDB_ROOT_PASSWORD": "app",
			},
			Volumes: []types.ServiceVolume{
				{Name: "mongo-data", Target: "/data/db"},
			},
		}
	},
	"rabbitmq": func() *types.ServiceConfig {
		return &types.ServiceConfig{
			Enabled:       true,
			Image:         "rabbitmq:4-management",
			ContainerPort: 5672,
			HostPort:      56721,
			HostIP:        "127.0.0.1",
			Env: map[string]string{
				"RABBITMQ_DEFAULT_USER": "app",
				"RABBITMQ_DEFAULT_PASS": "app",
			},
			Volumes: []types.ServiceVolume{
				{Name: "rabbitmq-data", Target: "/var/lib/rabbitmq"},
			},
		}
	},
	"nats": func() *types.ServiceConfig {
		return &types.ServiceConfig{
			Enabled:       true,
			Image:         "nats:2",
			ContainerPort: 4222,
			HostPort:      42221,
			HostIP:        "127.0.0.1",
		}
	},
	"memcached": func() *types.ServiceConfig {
		return &types.ServiceConfig{
			Enabled:       true,
			Image:         "memcached:1.6",
			ContainerPort: 11211,
			HostPort:      11212,
			HostIP:        "127.0.0.1",
		}
	},
	"clickhouse": func() *types.ServiceConfig {
		return &types.ServiceConfig{
			Enabled:       true,
			Image:         "clickhouse/clickhouse-server:26",
			ContainerPort: 9000,
			HostPort:      59000,
			HostIP:        "127.0.0.1",
			Volumes: []types.ServiceVolume{
				{Name: "clickhouse-data", Target: "/var/lib/clickhouse"},
			},
		}
	},
	"kafka": func() *types.ServiceConfig {
		return &types.ServiceConfig{
			Enabled:       true,
			Image:         "apache/kafka:4.3.0",
			ContainerPort: 9092,
			HostPort:      59092,
			HostIP:        "127.0.0.1",
			Volumes: []types.ServiceVolume{
				{Name: "kafka-data", Target: "/var/lib/kafka/data"},
			},
		}
	},
	"elasticsearch": func() *types.ServiceConfig {
		return &types.ServiceConfig{
			Enabled:       true,
			Image:         "docker.elastic.co/elasticsearch/elasticsearch:9",
			ContainerPort: 9200,
			HostPort:      59200,
			HostIP:        "127.0.0.1",
			Env: map[string]string{
				"discovery.type":         "single-node",
				"xpack.security.enabled": "false",
				"ES_JAVA_OPTS":           "-Xms512m -Xmx512m",
			},
			Volumes: []types.ServiceVolume{
				{Name: "elasticsearch-data", Target: "/usr/share/elasticsearch/data"},
			},
		}
	},
	"opensearch": func() *types.ServiceConfig {
		return &types.ServiceConfig{
			Enabled:       true,
			Image:         "opensearchproject/opensearch:3",
			ContainerPort: 9200,
			HostPort:      59201,
			HostIP:        "127.0.0.1",
			Env: map[string]string{
				"discovery.type":          "single-node",
				"DISABLE_SECURITY_PLUGIN": "true",
			},
			Volumes: []types.ServiceVolume{
				{Name: "opensearch-data", Target: "/usr/share/opensearch/data"},
			},
		}
	},
}

// Template returns a fresh ServiceConfig (Enabled=true) for a known service key.
// The returned value is independently owned; mutating it never affects the
// catalog or other callers. ok is false for unknown names.
func Template(name string) (*types.ServiceConfig, bool) {
	ctor, ok := catalog[name]
	if !ok {
		return nil, false
	}
	return ctor(), true
}

// Names returns the catalog's service keys in sorted order.
func Names() []string {
	names := make([]string, 0, len(catalog))
	for name := range catalog {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Has reports whether name is a known catalog service.
func Has(name string) bool {
	_, ok := catalog[name]
	return ok
}
