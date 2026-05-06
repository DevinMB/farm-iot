package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/farmsense/api/internal/middleware"
	vaultclient "github.com/farmsense/api/internal/vault"
)

// Hub represents a hub record.
type Hub struct {
	ID            string     `json:"id"`
	FarmID        string     `json:"farm_id"`
	Name          string     `json:"name"`
	VaultRoleID   string     `json:"-"`
	VaultSecretID string     `json:"-"`
	LastSeen      *time.Time `json:"last_seen"`
	Online        bool       `json:"online"`
	CreatedAt     time.Time  `json:"created_at"`
	Nodes         []Node     `json:"nodes,omitempty"`
}

// HubHandler handles hub routes.
type HubHandler struct {
	pool   *pgxpool.Pool
	vault  *vaultclient.Client
	domain string
}

// NewHubHandler creates a new HubHandler.
func NewHubHandler(pool *pgxpool.Pool, vault *vaultclient.Client, domain string) *HubHandler {
	return &HubHandler{pool: pool, vault: vault, domain: domain}
}

// List handles GET /api/farms/:farmId/hubs
func (h *HubHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	farmID := chi.URLParam(r, "farmId")

	if !userCanAccessFarm(r.Context(), h.pool, farmID, userID) {
		writeError(w, http.StatusNotFound, "farm not found")
		return
	}

	rows, err := h.pool.Query(r.Context(),
		`SELECT id, farm_id, name, vault_role_id, vault_secret_id, last_seen, created_at
		 FROM hubs WHERE farm_id = $1 ORDER BY created_at DESC`,
		farmID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query hubs")
		return
	}
	defer rows.Close()

	hubs := []Hub{}
	for rows.Next() {
		var hub Hub
		if err := rows.Scan(&hub.ID, &hub.FarmID, &hub.Name, &hub.VaultRoleID, &hub.VaultSecretID, &hub.LastSeen, &hub.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan hub")
			return
		}
		hub.Online = hub.LastSeen != nil && time.Since(*hub.LastSeen) < 5*time.Minute
		hubs = append(hubs, hub)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"hubs": hubs})
}

// Create handles POST /api/farms/:farmId/hubs
func (h *HubHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	farmID := chi.URLParam(r, "farmId")

	if !userCanAccessFarm(r.Context(), h.pool, farmID, userID) {
		writeError(w, http.StatusNotFound, "farm not found")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// Generate Vault AppRole credentials for this hub
	roleID, secretID, err := h.vault.CreateHubCredentials(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create hub credentials")
		return
	}

	var hub Hub
	err = h.pool.QueryRow(r.Context(),
		`INSERT INTO hubs (farm_id, name, vault_role_id, vault_secret_id) VALUES ($1, $2, $3, $4)
		 RETURNING id, farm_id, name, vault_role_id, vault_secret_id, last_seen, created_at`,
		farmID, req.Name, roleID, secretID,
	).Scan(&hub.ID, &hub.FarmID, &hub.Name, &hub.VaultRoleID, &hub.VaultSecretID, &hub.LastSeen, &hub.CreatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create hub")
		return
	}
	hub.Online = false

	writeJSON(w, http.StatusCreated, hub)
}

// Get handles GET /api/hubs/:hubId
func (h *HubHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	hubID := chi.URLParam(r, "hubId")

	var hub Hub
	err := h.pool.QueryRow(r.Context(),
		`SELECT h.id, h.farm_id, h.name, h.vault_role_id, h.vault_secret_id, h.last_seen, h.created_at
		 FROM hubs h
		 JOIN farms f ON f.id = h.farm_id
		 WHERE h.id = $1 AND (f.user_id = $2 OR EXISTS(SELECT 1 FROM farm_members fm WHERE fm.farm_id = f.id AND fm.user_id = $2))`,
		hubID, userID,
	).Scan(&hub.ID, &hub.FarmID, &hub.Name, &hub.VaultRoleID, &hub.VaultSecretID, &hub.LastSeen, &hub.CreatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "hub not found")
		return
	}
	hub.Online = hub.LastSeen != nil && time.Since(*hub.LastSeen) < 5*time.Minute

	// Load nodes
	rows, err := h.pool.Query(r.Context(),
		`SELECT id, hub_id, name, sensor_type, unit, last_seen, created_at
		 FROM nodes WHERE hub_id = $1 ORDER BY created_at DESC`,
		hubID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query nodes")
		return
	}
	defer rows.Close()

	hub.Nodes = []Node{}
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.ID, &n.HubID, &n.Name, &n.SensorType, &n.Unit, &n.LastSeen, &n.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan node")
			return
		}
		hub.Nodes = append(hub.Nodes, n)
	}

	writeJSON(w, http.StatusOK, hub)
}

// Delete handles DELETE /api/hubs/:hubId
func (h *HubHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	hubID := chi.URLParam(r, "hubId")

	result, err := h.pool.Exec(r.Context(),
		`DELETE FROM hubs h USING farms f
		 WHERE h.id = $1 AND h.farm_id = f.id AND f.user_id = $2`,
		hubID, userID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete hub")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "hub not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Provision handles GET /api/hubs/:hubId/provision
func (h *HubHandler) Provision(w http.ResponseWriter, r *http.Request) {
	userID, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	hubID := chi.URLParam(r, "hubId")

	arch := r.URL.Query().Get("arch")
	switch arch {
	case "armv7", "arm64":
		// valid
	default:
		arch = "armv6"
	}

	var hub Hub
	err := h.pool.QueryRow(r.Context(),
		`SELECT h.id, h.farm_id, h.name, h.vault_role_id, h.vault_secret_id, h.last_seen, h.created_at
		 FROM hubs h
		 JOIN farms f ON f.id = h.farm_id
		 WHERE h.id = $1 AND (f.user_id = $2 OR EXISTS(SELECT 1 FROM farm_members fm WHERE fm.farm_id = f.id AND fm.user_id = $2))`,
		hubID, userID,
	).Scan(&hub.ID, &hub.FarmID, &hub.Name, &hub.VaultRoleID, &hub.VaultSecretID, &hub.LastSeen, &hub.CreatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "hub not found")
		return
	}

	script := fmt.Sprintf(`#!/bin/bash
# FarmSense Hub Setup Script
# Hub: %s (%s)
# Generated: %s
set -euo pipefail

API_URL="http://api.%s"
REBOOT_NEEDED=false

echo "==> FarmSense Hub Setup"
echo "    Hub ID: %s"
echo "    API:    $API_URL"

# Install dependencies
apt-get update -qq
apt-get install -y -qq curl

# Enable SPI (required for LoRa HAT)
CONFIG_FILE=""
if   [ -f /boot/firmware/config.txt ]; then CONFIG_FILE=/boot/firmware/config.txt  # Pi OS Bookworm+
elif [ -f /boot/config.txt ];          then CONFIG_FILE=/boot/config.txt            # Pi OS Bullseye
fi
if [ -n "$CONFIG_FILE" ]; then
    if ! grep -q "^dtparam=spi=on" "$CONFIG_FILE"; then
        echo "dtparam=spi=on" >> "$CONFIG_FILE"
        echo "==> SPI enabled in $CONFIG_FILE (reboot required)"
        REBOOT_NEEDED=true
    else
        echo "==> SPI already enabled"
    fi
fi

# Write config (values embedded at generation time, not shell variables)
mkdir -p /etc/farmsense
cat > /etc/farmsense/config.env << 'ENVEOF'
HUB_ID=%s
VAULT_ADDR=http://vault.%s
VAULT_ROLE_ID=%s
VAULT_SECRET_ID=%s
API_URL=http://api.%s

# ── LoRa HAT (SX1262) ──────────────────────────────────────────────────────
LORA_ENABLED=true
LORA_SPI_DEV=/dev/spidev0.0
# GPIO pin numbers (BCM). Adjust if your HAT uses different pins.
LORA_RESET_PIN=22
LORA_BUSY_PIN=23
LORA_DIO1_PIN=6
# Radio parameters — must match your ESP32 firmware
LORA_FREQ_HZ=915000000
LORA_SF=7
LORA_BW_CODE=4
LORA_CR=1
ENVEOF
chmod 600 /etc/farmsense/config.env
echo "==> Config written to /etc/farmsense/config.env"

# Download hub daemon binary (%s)
echo "==> Downloading hub daemon..."
curl -fsSL "$API_URL/api/hub/daemon?arch=%s" -o /usr/local/bin/farmsense-hub
chmod +x /usr/local/bin/farmsense-hub
echo "==> Daemon installed at /usr/local/bin/farmsense-hub"

# Install systemd service
cat > /etc/systemd/system/farmsense-hub.service << 'SVCEOF'
[Unit]
Description=FarmSense Hub Daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/farmsense-hub
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
SVCEOF

systemctl daemon-reload
systemctl enable farmsense-hub

if [ "$REBOOT_NEEDED" = "true" ]; then
    echo ""
    echo "==> Setup complete! SPI was just enabled — a reboot is required."
    echo "    Run: sudo reboot"
    echo "    After reboot the FarmSense daemon will start automatically."
else
    systemctl start farmsense-hub
    echo ""
    echo "==> FarmSense Hub is running!"
    echo "    View logs: journalctl -u farmsense-hub -f"
    echo "    Status:    systemctl status farmsense-hub"
fi
`,
		hub.Name,
		hub.ID,
		time.Now().UTC().Format(time.RFC3339),
		h.domain,
		hub.ID,
		// config.env literal values
		hub.ID,
		h.domain,
		hub.VaultRoleID,
		hub.VaultSecretID,
		h.domain,
		// daemon download
		arch,
		arch,
	)

	w.Header().Set("Content-Type", "text/x-shellscript")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="farmsense-hub-setup.sh"`))
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(script))
}
