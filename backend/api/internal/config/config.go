package config

import (
	"context"
	"fmt"
	"os"

	vaultclient "github.com/farmsense/api/internal/vault"
)

// Config holds all runtime configuration for the API.
// Secrets are fetched from Vault; non-secret config is read from env.
type Config struct {
	// Database — dynamic credentials from Vault
	PostgresDSN string

	// InfluxDB
	InfluxURL    string
	InfluxToken  string
	InfluxOrg    string
	InfluxBucket string

	// Kafka — not a secret, just addressing
	KafkaBrokers string

	// Auth
	JWTSecret string
}

// Load authenticates with Vault and fetches all secrets, returning a Config.
func Load(ctx context.Context) (*Config, error) {
	addr := mustEnv("VAULT_ADDR")
	roleID := mustEnv("VAULT_ROLE_ID")
	secretID := mustEnv("VAULT_SECRET_ID")

	vc, err := vaultclient.New(ctx, addr, roleID, secretID)
	if err != nil {
		return nil, fmt.Errorf("config: vault init: %w", err)
	}

	// Fetch InfluxDB secrets
	influxData, err := vc.GetKV(ctx, "farmsense/influxdb")
	if err != nil {
		return nil, fmt.Errorf("config: influxdb secrets: %w", err)
	}

	// Fetch API secrets (JWT)
	apiData, err := vc.GetKV(ctx, "farmsense/api")
	if err != nil {
		return nil, fmt.Errorf("config: api secrets: %w", err)
	}

	// Fetch dynamic Postgres credentials
	pgUser, pgPass, err := vc.GetDBCreds(ctx, "api-db-role")
	if err != nil {
		return nil, fmt.Errorf("config: postgres creds: %w", err)
	}

	pgDB := mustEnv("POSTGRES_DB")
	pgAdminUser := mustEnv("POSTGRES_USER")
	pgAdminPass := mustEnv("POSTGRES_PASSWORD")

	// Use admin DSN for migrations and runtime pool.
	// The Vault dynamic creds (pgUser/pgPass) are reserved for future use
	// once we add ALTER DEFAULT PRIVILEGES to the schema.
	_ = pgUser
	_ = pgPass
	postgresDSN := fmt.Sprintf(
		"postgres://%s:%s@postgres:5432/%s?sslmode=disable",
		pgAdminUser, pgAdminPass, pgDB,
	)

	return &Config{
		PostgresDSN:  postgresDSN,
		InfluxURL:    str(influxData["url"]),
		InfluxToken:  str(influxData["token"]),
		InfluxOrg:    mustEnv("INFLUXDB_ORG"),
		InfluxBucket: mustEnv("INFLUXDB_BUCKET"),
		KafkaBrokers: mustEnv("KAFKA_BROKERS"),
		JWTSecret:    str(apiData["jwt_secret"]),
	}, nil
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("required env var %q is not set", key))
	}
	return v
}

func str(v interface{}) string {
	s, _ := v.(string)
	return s
}
