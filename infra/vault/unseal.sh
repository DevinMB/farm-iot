#!/bin/sh
# vault-unseal sidecar
# Polls Vault every 10 seconds. If sealed, unseals using the key stored
# in the vault_keys volume written by init.sh.
# Runs forever — Docker restart policy handles any crashes.
#
# Uses vault status exit code (not JSON parsing) to detect sealed state:
#   0 = unsealed, 1 = error, 2 = sealed

KEYS_FILE="/vault/keys/keys.json"

echo "==> vault-unseal: waiting for keys file at $KEYS_FILE..."
while [ ! -f "$KEYS_FILE" ]; do
  echo "    keys file not found yet — run init.sh first"
  sleep 15
done

echo "==> vault-unseal: keys found, entering watch loop"

while true; do
  vault status > /dev/null 2>&1
  STATUS_CODE=$?

  if [ "$STATUS_CODE" = "2" ]; then
    echo "==> vault-unseal: Vault is sealed — unsealing..."
    UNSEAL_KEY=$(jq -r '.unseal_keys_hex[0]' "$KEYS_FILE")
    vault operator unseal "$UNSEAL_KEY" \
      && echo "==> vault-unseal: unsealed successfully" \
      || echo "==> vault-unseal: unseal failed, will retry"
  elif [ "$STATUS_CODE" = "1" ]; then
    echo "==> vault-unseal: Vault unreachable, will retry..."
  fi

  sleep 10
done
