# Policy for the Go Kafka consumer service
# Only needs to read the InfluxDB token — nothing else.

path "secret/data/farmsense/influxdb" {
  capabilities = ["read"]
}

path "auth/token/renew-self" {
  capabilities = ["update"]
}
