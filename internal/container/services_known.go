package container

import (
	"fmt"
	"net/url"

	"github.com/sxwebdev/devc/pkg/types"
)

// Any image works as a service. The registries below only provide convenience
// defaults for well-known service keys: a default container port (so you can
// omit containerPort) and a derived connection-string env var for the agent.
// For anything else, set containerPort explicitly and use agentEnv for the
// connection string.

// defaultServicePorts maps a conventional service key to its default port.
var defaultServicePorts = map[string]int{
	"postgres":      5432,
	"postgresql":    5432,
	"redis":         6379,
	"valkey":        6379,
	"mysql":         3306,
	"mariadb":       3306,
	"mongo":         27017,
	"mongodb":       27017,
	"rabbitmq":      5672,
	"nats":          4222,
	"kafka":         9092,
	"clickhouse":    9000,
	"elasticsearch": 9200,
	"opensearch":    9200,
	"memcached":     11211,
}

// connStringBuilder derives a "KEY=value" connection env var for the agent from
// a service config, its DNS alias, and resolved port. Returns "" for none.
type connStringBuilder func(svc *types.ServiceConfig, alias string, port int) string

// connStringBuilders maps a conventional service key to its connection builder.
// Variable names are distinct per service to avoid collisions.
var connStringBuilders = map[string]connStringBuilder{
	"postgres":   postgresConn,
	"postgresql": postgresConn,
	"redis":      redisConn,
	"valkey":     redisConn,
	"mysql":      mysqlConn,
	"mariadb":    mysqlConn,
	"mongo":      mongoConn,
	"mongodb":    mongoConn,
	"rabbitmq":   rabbitConn,
	"nats":       natsConn,
	"kafka":      kafkaConn,
}

func postgresConn(svc *types.ServiceConfig, alias string, port int) string {
	user := valueOr(svc.Env["POSTGRES_USER"], "app")
	pass := valueOr(svc.Env["POSTGRES_PASSWORD"], "app")
	db := valueOr(svc.Env["POSTGRES_DB"], "app")
	return fmt.Sprintf("DATABASE_URL=postgresql://%s:%s@%s:%d/%s",
		url.QueryEscape(user), url.QueryEscape(pass), alias, port, db)
}

func redisConn(_ *types.ServiceConfig, alias string, port int) string {
	return fmt.Sprintf("REDIS_URL=redis://%s:%d", alias, port)
}

func mysqlConn(svc *types.ServiceConfig, alias string, port int) string {
	user := valueOr(svc.Env["MYSQL_USER"], "root")
	pass := valueOr(svc.Env["MYSQL_PASSWORD"], svc.Env["MYSQL_ROOT_PASSWORD"])
	db := valueOr(svc.Env["MYSQL_DATABASE"], "app")
	return fmt.Sprintf("DATABASE_URL=mysql://%s:%s@%s:%d/%s",
		url.QueryEscape(user), url.QueryEscape(pass), alias, port, db)
}

func mongoConn(svc *types.ServiceConfig, alias string, port int) string {
	user := svc.Env["MONGO_INITDB_ROOT_USERNAME"]
	pass := svc.Env["MONGO_INITDB_ROOT_PASSWORD"]
	if user == "" {
		return fmt.Sprintf("MONGO_URL=mongodb://%s:%d", alias, port)
	}
	return fmt.Sprintf("MONGO_URL=mongodb://%s:%s@%s:%d",
		url.QueryEscape(user), url.QueryEscape(pass), alias, port)
}

func rabbitConn(svc *types.ServiceConfig, alias string, port int) string {
	user := valueOr(svc.Env["RABBITMQ_DEFAULT_USER"], "guest")
	pass := valueOr(svc.Env["RABBITMQ_DEFAULT_PASS"], "guest")
	return fmt.Sprintf("AMQP_URL=amqp://%s:%s@%s:%d/",
		url.QueryEscape(user), url.QueryEscape(pass), alias, port)
}

func natsConn(_ *types.ServiceConfig, alias string, port int) string {
	return fmt.Sprintf("NATS_URL=nats://%s:%d", alias, port)
}

func kafkaConn(_ *types.ServiceConfig, alias string, port int) string {
	return fmt.Sprintf("KAFKA_BROKERS=%s:%d", alias, port)
}
