# Policy for the Go API service
# Grants the API read access to secrets it needs at startup.

# Read static secrets (JWT secret, DB credentials, etc.)
path "secret/data/farmsense/*" {
  capabilities = ["read"]
}

# Generate dynamic Postgres credentials
path "database/creds/api-db-role" {
  capabilities = ["read"]
}

# Allow the API to issue AppRole secret_ids for new hubs during device registration
path "auth/approle/role/hub-role/secret-id" {
  capabilities = ["create", "update"]
}

# Allow the API to look up AppRole role_id for hub-role
path "auth/approle/role/hub-role/role-id" {
  capabilities = ["read"]
}

# Allow the API to renew its own token
path "auth/token/renew-self" {
  capabilities = ["update"]
}
