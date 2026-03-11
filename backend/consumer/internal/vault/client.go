package vault

import (
	"context"
	"fmt"
	"log/slog"

	vault "github.com/hashicorp/vault/api"
)

// Client wraps the Vault API client with AppRole auth and token renewal.
type Client struct {
	v *vault.Client
}

// New authenticates with Vault using AppRole and returns a ready Client.
func New(ctx context.Context, addr, roleID, secretID string) (*Client, error) {
	cfg := vault.DefaultConfig()
	cfg.Address = addr

	v, err := vault.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("vault client: %w", err)
	}

	secret, err := v.Logical().WriteWithContext(ctx, "auth/approle/login", map[string]interface{}{
		"role_id":   roleID,
		"secret_id": secretID,
	})
	if err != nil {
		return nil, fmt.Errorf("vault approle login: %w", err)
	}
	if secret.Auth == nil {
		return nil, fmt.Errorf("vault approle login: no auth info returned")
	}

	v.SetToken(secret.Auth.ClientToken)
	c := &Client{v: v}

	go c.renewToken(ctx, secret)

	return c, nil
}

// GetKV reads a KV v2 secret and returns the data map.
func (c *Client) GetKV(ctx context.Context, path string) (map[string]interface{}, error) {
	s, err := c.v.KVv2("secret").Get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("vault kv get %s: %w", path, err)
	}
	return s.Data, nil
}

func (c *Client) renewToken(ctx context.Context, secret *vault.Secret) {
	watcher, err := c.v.NewLifetimeWatcher(&vault.LifetimeWatcherInput{Secret: secret})
	if err != nil {
		slog.Error("vault: failed to create token renewer", "err", err)
		return
	}
	go watcher.Start()
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case err := <-watcher.DoneCh():
			if err != nil {
				slog.Error("vault: token renewal failed", "err", err)
			}
			return
		case renewal := <-watcher.RenewCh():
			slog.Debug("vault: token renewed", "ttl", renewal.Secret.Auth.LeaseDuration)
		}
	}
}
