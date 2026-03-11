package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/farmsense/api/internal/middleware"
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
	pool *pgxpool.Pool
}

// NewHubHandler creates a new HubHandler.
func NewHubHandler(pool *pgxpool.Pool) *HubHandler {
	return &HubHandler{pool: pool}
}

// List handles GET /api/farms/:farmId/hubs
func (h *HubHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	farmID := chi.URLParam(r, "farmId")

	// Verify farm ownership
	var farmExists bool
	err := h.pool.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM farms WHERE id = $1 AND user_id = $2)`,
		farmID, userID,
	).Scan(&farmExists)
	if err != nil || !farmExists {
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

	// Verify farm ownership
	var farmExists bool
	err := h.pool.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM farms WHERE id = $1 AND user_id = $2)`,
		farmID, userID,
	).Scan(&farmExists)
	if err != nil || !farmExists {
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

	var hub Hub
	err = h.pool.QueryRow(r.Context(),
		`INSERT INTO hubs (farm_id, name) VALUES ($1, $2)
		 RETURNING id, farm_id, name, vault_role_id, vault_secret_id, last_seen, created_at`,
		farmID, req.Name,
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
		 WHERE h.id = $1 AND f.user_id = $2`,
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

// Provision handles GET /api/hubs/:hubId/provision
func (h *HubHandler) Provision(w http.ResponseWriter, r *http.Request) {
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
		 WHERE h.id = $1 AND f.user_id = $2`,
		hubID, userID,
	).Scan(&hub.ID, &hub.FarmID, &hub.Name, &hub.VaultRoleID, &hub.VaultSecretID, &hub.LastSeen, &hub.CreatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "hub not found")
		return
	}

	// Extract server host from request, strip port
	serverHost := r.Host
	if idx := strings.LastIndex(serverHost, ":"); idx != -1 {
		serverHost = serverHost[:idx]
	}
	if serverHost == "" {
		serverHost = "farmsense.local"
	}

	script := fmt.Sprintf(`#!/bin/bash
# FarmSense Hub Setup Script
# Hub: %s (%s)
# Generated: %s
set -euo pipefail

HUB_ID="%s"
VAULT_ADDR="http://%s:8200"
VAULT_ROLE_ID="%s"
VAULT_SECRET_ID="%s"
KAFKA_BROKERS="%s:9092"

echo "==> FarmSense Hub Setup"
echo "    Hub ID: $HUB_ID"

# Install dependencies
apt-get update -qq
apt-get install -y -qq curl jq

# Write config
mkdir -p /etc/farmsense
cat > /etc/farmsense/config.env << EOF
HUB_ID=$HUB_ID
VAULT_ADDR=$VAULT_ADDR
VAULT_ROLE_ID=$VAULT_ROLE_ID
VAULT_SECRET_ID=$VAULT_SECRET_ID
KAFKA_BROKERS=$KAFKA_BROKERS
EOF
chmod 600 /etc/farmsense/config.env

echo "==> Hub configured successfully!"
echo "    Config written to /etc/farmsense/config.env"
echo "    Hub software installation coming soon."
`,
		hub.Name,
		hub.ID,
		time.Now().UTC().Format(time.RFC3339),
		hub.ID,
		serverHost,
		hub.VaultRoleID,
		hub.VaultSecretID,
		serverHost,
	)

	w.Header().Set("Content-Type", "text/x-shellscript")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="farmsense-hub-setup.sh"`))
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(script))
}
