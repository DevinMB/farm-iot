#!/bin/sh
# vault-unseal sidecar
# Polls Vault every 30 seconds. If sealed, unseals using the key stored
# in the vault_keys volume written by init.sh.
# Runs forever — Docker restart policy handles any crashes.

KEYS_FILE="/vault/keys/keys.json"

echo "==> vault-unseal: waiting for keys file at $KEYS_FILE..."
while [ ! -f "$KEYS_FILE" ]; do
  echo "    keys file not found yet — run init.sh first"
  sleep 15
done

echo "==> vault-unseal: keys found, entering watch loop"

while true; do
  # Check if sealed
  STATUS=$(vault status -format=json 2>/dev/null || echo '{"sealed":true}')
  SEALED=$(echo "$STATUS" | grep -o '"sealed":[a-z]*' | cut -d: -f2)

  if [ "$SEALED" = "true" ]; then
    echo "==> vault-unseal: Vault is sealed — unsealing..."
    UNSEAL_KEY=$(jq -r '.unseal_keys_hex[0]' "$KEYS_FILE")
    vault operator unseal "$UNSEAL_KEY" && echo "==> vault-unseal: unsealed successfully" || echo "==> vault-unseal: unseal failed, will retry"
  fi

  sleep 30
done
