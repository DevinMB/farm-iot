package vault

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	vault "github.com/hashicorp/vault/api"
)

// Client wraps the Vault API client with AppRole auth and lease renewal.
type Client struct {
	v *vault.Client
}

// New authenticates with Vault using AppRole and returns a ready Client.
// It also starts a background goroutine to renew the token before it expires.
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

// GetDBCreds fetches dynamic Postgres credentials from the database secrets engine.
func (c *Client) GetDBCreds(ctx context.Context, role string) (username, password string, err error) {
	s, err := c.v.Logical().ReadWithContext(ctx, "database/creds/"+role)
	if err != nil {
		return "", "", fmt.Errorf("vault db creds %s: %w", role, err)
	}
	username, _ = s.Data["username"].(string)
	password, _ = s.Data["password"].(string)

	// Renew DB lease in background so credentials don't expire under us
	go c.renewLease(ctx, s.LeaseID, s.LeaseDuration)

	return username, password, nil
}

// renewToken renews the Vault token before it expires.
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

// renewLease renews a secret lease (e.g. dynamic DB creds) before expiry.
func (c *Client) renewLease(ctx context.Context, leaseID string, leaseDuration int) {
	if leaseID == "" {
		return
	}
	// Renew at 75% of TTL
	renewIn := time.Duration(float64(leaseDuration)*0.75) * time.Second
	timer := time.NewTimer(renewIn)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			_, err := c.v.Logical().WriteWithContext(ctx, "sys/leases/renew", map[string]interface{}{
				"lease_id":  leaseID,
				"increment": leaseDuration,
			})
			if err != nil {
				slog.Error("vault: lease renewal failed", "lease", leaseID, "err", err)
				return
			}
			slog.Debug("vault: lease renewed", "lease", leaseID)
			timer.Reset(renewIn)
		}
	}
}
