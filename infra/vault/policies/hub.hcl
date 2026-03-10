# Policy for Raspberry Pi hubs
# Grants hubs the ability to read dynamic credentials only — nothing else.

# Read dynamic InfluxDB credentials
path "influxdb/creds/hub-role" {
  capabilities = ["read"]
}

# Read dynamic Kafka credentials (if using Vault Kafka secrets engine)
path "kafka/creds/hub-role" {
  capabilities = ["read"]
}

# Allow hubs to renew their own leases
path "sys/leases/renew" {
  capabilities = ["update"]
}

# Allow hubs to revoke their own token on clean shutdown
path "auth/token/revoke-self" {
  capabilities = ["update"]
}
