package config

import (
	"context"
	"fmt"
	"os"

	vaultclient "github.com/farmsense/consumer/internal/vault"
)

// Config holds all runtime configuration for the consumer.
type Config struct {
	InfluxURL    string
	InfluxToken  string
	InfluxOrg    string
	InfluxBucket string
	KafkaBrokers string
	KafkaTopic   string
	KafkaGroupID string
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

	influxData, err := vc.GetKV(ctx, "farmsense/influxdb")
	if err != nil {
		return nil, fmt.Errorf("config: influxdb secrets: %w", err)
	}

	return &Config{
		InfluxURL:    str(influxData["url"]),
		InfluxToken:  str(influxData["token"]),
		InfluxOrg:    mustEnv("INFLUXDB_ORG"),
		InfluxBucket: mustEnv("INFLUXDB_BUCKET"),
		KafkaBrokers: mustEnv("KAFKA_BROKERS"),
		KafkaTopic:   mustEnv("KAFKA_TOPIC"),
		KafkaGroupID: mustEnv("KAFKA_GROUP_ID"),
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
