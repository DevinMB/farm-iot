#!/usr/bin/env bash
# FarmSense Vault Bootstrap — run ONCE after first `docker compose up vault`
# After this script completes, everything is fully automated forever.
#
# Usage (from project root, with vault_keys volume mounted):
#   docker compose exec -e POSTGRES_USER=$POSTGRES_USER \
#                       -e POSTGRES_PASSWORD=$POSTGRES_PASSWORD \
#                       -e POSTGRES_DB=$POSTGRES_DB \
#                       -e INFLUXDB_ADMIN_TOKEN=$INFLUXDB_ADMIN_TOKEN \
#                       -e JWT_SECRET=$(openssl rand -hex 64) \
#                       vault sh /vault/policies/../init.sh
#
# Or run locally pointing at the exposed port:
#   VAULT_ADDR=http://localhost:8200 \
#   POSTGRES_USER=... POSTGRES_PASSWORD=... POSTGRES_DB=... \
#   INFLUXDB_ADMIN_TOKEN=... JWT_SECRET=$(openssl rand -hex 64) \
#   ./infra/vault/init.sh

# Note: no strict mode — busybox ash compatibility, errors handled explicitly

KEYS_FILE="/vault/keys/keys.json"

# ─── 1. Initialize Vault ──────────────────────────────────────────────────────
echo "==> Checking Vault init status..."
if [ -f "$KEYS_FILE" ]; then
  echo "    Keys file already exists — Vault already initialized, skipping init."
else
  echo "==> Initializing Vault (1 key share, threshold 1)..."
  vault operator init -key-shares=1 -key-threshold=1 -format=json > "$KEYS_FILE"
  chmod 644 "$KEYS_FILE"
  echo "    Keys saved to $KEYS_FILE (inside vault_keys Docker volume)"
fi

# ─── 2. Unseal ────────────────────────────────────────────────────────────────
echo "==> Unsealing Vault..."
# Parse pretty-printed JSON without jq (not available in vault image)
# Use hex key (easier to extract from multiline output)
UNSEAL_KEY=$(grep '"unseal_keys_hex"' -A1 "$KEYS_FILE" | grep -o '"[a-f0-9]*"' | tr -d '"')
ROOT_TOKEN=$(grep '"root_token"' "$KEYS_FILE" | sed 's/.*"root_token": *"\([^"]*\)".*/\1/')
# Attempt unseal — safe to run even if already unsealed
vault operator unseal "$UNSEAL_KEY" || echo "    Already unsealed or unseal failed (continuing)"
export VAULT_TOKEN="$ROOT_TOKEN"

# ─── 3. Enable engines ────────────────────────────────────────────────────────
echo "==> Enabling engines..."
vault auth enable approle       2>/dev/null || echo "    approle already enabled"
vault secrets enable -path=secret kv-v2    2>/dev/null || echo "    kv-v2 already enabled"
vault secrets enable database   2>/dev/null || echo "    database already enabled"

# ─── 4. Write policies ────────────────────────────────────────────────────────
echo "==> Writing policies..."
vault policy write hub      /vault/policies/hub.hcl
vault policy write api      /vault/policies/api.hcl
vault policy write consumer /vault/policies/consumer.hcl

# ─── 5. Postgres dynamic credentials ─────────────────────────────────────────
echo "==> Configuring Postgres dynamic credentials..."
vault write database/config/farmsense-postgres \
  plugin_name=postgresql-database-plugin \
  allowed_roles="api-db-role" \
  connection_url="postgresql://{{username}}:{{password}}@postgres:5432/${POSTGRES_DB}?sslmode=disable" \
  username="${POSTGRES_USER}" \
  password="${POSTGRES_PASSWORD}"

vault write database/roles/api-db-role \
  db_name=farmsense-postgres \
  creation_statements="CREATE ROLE \"{{name}}\" WITH LOGIN PASSWORD '{{password}}' VALID UNTIL '{{expiration}}'; \
    GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO \"{{name}}\"; \
    GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO \"{{name}}\";" \
  default_ttl="1h" \
  max_ttl="24h"

# ─── 6. Store secrets in KV ───────────────────────────────────────────────────
echo "==> Storing application secrets..."
vault kv put secret/farmsense/influxdb \
  token="${INFLUXDB_ADMIN_TOKEN}" \
  url="http://influxdb:8086"

vault kv put secret/farmsense/api \
  jwt_secret="${JWT_SECRET}"

# ─── 7. AppRole: api ──────────────────────────────────────────────────────────
echo "==> Creating AppRole: api..."
vault write auth/approle/role/api-role \
  token_policies="api" \
  token_ttl=1h \
  token_max_ttl=4h \
  secret_id_ttl=0

API_ROLE_ID=$(vault read -field=role_id auth/approle/role/api-role/role-id)
API_SECRET_ID=$(vault write -field=secret_id -f auth/approle/role/api-role/secret-id)

# ─── 8. AppRole: consumer ─────────────────────────────────────────────────────
echo "==> Creating AppRole: consumer..."
vault write auth/approle/role/consumer-role \
  token_policies="consumer" \
  token_ttl=1h \
  token_max_ttl=4h \
  secret_id_ttl=0

CONSUMER_ROLE_ID=$(vault read -field=role_id auth/approle/role/consumer-role/role-id)
CONSUMER_SECRET_ID=$(vault write -field=secret_id -f auth/approle/role/consumer-role/secret-id)

# ─── 9. AppRole: hub (used when registering physical devices) ─────────────────
echo "==> Creating AppRole: hub..."
vault write auth/approle/role/hub-role \
  token_policies="hub" \
  token_ttl=1h \
  token_max_ttl=4h \
  secret_id_ttl=24h \
  bind_secret_id=true

# ─── Done ─────────────────────────────────────────────────────────────────────
echo ""
echo "================================================================"
echo " Bootstrap complete! Add these to your .env:"
echo ""
echo " VAULT_API_ROLE_ID=${API_ROLE_ID}"
echo " VAULT_API_SECRET_ID=${API_SECRET_ID}"
echo " VAULT_CONSUMER_ROLE_ID=${CONSUMER_ROLE_ID}"
echo " VAULT_CONSUMER_SECRET_ID=${CONSUMER_SECRET_ID}"
echo "================================================================"
echo ""
echo " vault-unseal will handle unseal on every restart automatically."
echo " DO NOT re-run this script unless rebuilding from scratch."
