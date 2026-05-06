package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/farmsense/api/internal/middleware"

	"github.com/golang-jwt/jwt/v5"
)

// HubAPIHandler handles hub-device-facing routes.
type HubAPIHandler struct {
	pool         *pgxpool.Pool
	jwtSecret    string
	influxURL    string
	influxToken  string
	influxOrg    string
	influxBucket string
}

// NewHubAPIHandler creates a new HubAPIHandler.
func NewHubAPIHandler(
	pool *pgxpool.Pool,
	jwtSecret string,
	influxURL, influxToken, influxOrg, influxBucket string,
) *HubAPIHandler {
	return &HubAPIHandler{
		pool:         pool,
		jwtSecret:    jwtSecret,
		influxURL:    influxURL,
		influxToken:  influxToken,
		influxOrg:    influxOrg,
		influxBucket: influxBucket,
	}
}

// Auth handles POST /api/hub/auth
// Hub authenticates with role_id+secret_id via Vault AppRole, gets hub JWT.
func (h *HubAPIHandler) Auth(w http.ResponseWriter, r *http.Request) {
	var req struct {
		HubID    string `json:"hub_id"`
		RoleID   string `json:"role_id"`
		SecretID string `json:"secret_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.HubID == "" || req.RoleID == "" || req.SecretID == "" {
		writeError(w, http.StatusBadRequest, "hub_id, role_id, and secret_id are required")
		return
	}

	// Verify hub exists and credentials match what was stored at provisioning time.
	// We compare directly against DB values — calling Vault AppRole login would
	// consume the secret_id use count or fail after its TTL, so we avoid it.
	var storedRoleID, storedSecretID string
	err := h.pool.QueryRow(r.Context(),
		`SELECT vault_role_id, vault_secret_id FROM hubs WHERE id = $1`,
		req.HubID,
	).Scan(&storedRoleID, &storedSecretID)
	if err != nil || storedRoleID != req.RoleID || storedSecretID != req.SecretID {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Issue hub JWT (2h expiry)
	claims := jwt.MapClaims{
		"hub_id": req.HubID,
		"type":   "hub",
		"exp":    time.Now().Add(2 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(h.jwtSecret))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"token": signed})
}

// Heartbeat handles POST /api/hub/heartbeat
// Updates hub last_seen to now.
func (h *HubAPIHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	hubID, ok := authmw.HubIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	_, err := h.pool.Exec(r.Context(),
		`UPDATE hubs SET last_seen = NOW() WHERE id = $1`,
		hubID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update heartbeat")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListNodes handles GET /api/hub/nodes
// Returns nodes belonging to this hub.
func (h *HubAPIHandler) ListNodes(w http.ResponseWriter, r *http.Request) {
	hubID, ok := authmw.HubIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	rows, err := h.pool.Query(r.Context(),
		`SELECT id, hub_id, name, sensor_type, unit, last_seen, created_at
		 FROM nodes WHERE hub_id = $1 ORDER BY created_at ASC`,
		hubID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query nodes")
		return
	}
	defer rows.Close()

	nodes := []Node{}
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.ID, &n.HubID, &n.Name, &n.SensorType, &n.Unit, &n.LastSeen, &n.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan node")
			return
		}
		nodes = append(nodes, n)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"nodes": nodes})
}

// hubReading is a single sensor reading from the hub daemon.
type hubReading struct {
	NodeID     string  `json:"node_id"`
	SensorType string  `json:"sensor_type"`
	Value      float64 `json:"value"`
	Unit       string  `json:"unit"`
	Timestamp  int64   `json:"timestamp"`
}

// Readings handles POST /api/hub/readings
// Accepts batch sensor readings and writes to InfluxDB.
func (h *HubAPIHandler) Readings(w http.ResponseWriter, r *http.Request) {
	hubID, ok := authmw.HubIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req struct {
		Readings []hubReading `json:"readings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.Readings) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Look up farm_id for this hub (needed as InfluxDB tag)
	var farmID string
	err := h.pool.QueryRow(r.Context(),
		`SELECT farm_id FROM hubs WHERE id = $1`, hubID,
	).Scan(&farmID)
	if err != nil {
		writeError(w, http.StatusNotFound, "hub not found")
		return
	}

	// Update node last_seen for all nodes in this batch
	nodeIDs := make([]string, 0, len(req.Readings))
	seen := map[string]bool{}
	for _, rd := range req.Readings {
		if !seen[rd.NodeID] {
			nodeIDs = append(nodeIDs, rd.NodeID)
			seen[rd.NodeID] = true
		}
	}
	for _, nid := range nodeIDs {
		h.pool.Exec(r.Context(), `UPDATE nodes SET last_seen = NOW() WHERE id = $1 AND hub_id = $2`, nid, hubID) //nolint:errcheck
	}

	// Build InfluxDB line protocol body
	var buf bytes.Buffer
	for _, rd := range req.Readings {
		// Escape tag values (spaces → "\ ", commas → "\,", equals → "\=")
		escapedUnit := escapeTagValue(rd.Unit)
		escapedType := escapeTagValue(rd.SensorType)
		fmt.Fprintf(&buf, "sensor_reading,node_id=%s,farm_id=%s,sensor_type=%s,unit=%s value=%f %d\n",
			rd.NodeID, farmID, escapedType, escapedUnit, rd.Value, rd.Timestamp)
	}

	if err := h.writeToInflux(buf.Bytes()); err != nil {
		// Log but don't fail — readings are best-effort
		writeError(w, http.StatusInternalServerError, "failed to write readings")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *HubAPIHandler) writeToInflux(body []byte) error {
	if h.influxURL == "" {
		return nil
	}

	url := strings.TrimRight(h.influxURL, "/") +
		"/api/v2/write?org=" + h.influxOrg +
		"&bucket=" + h.influxBucket +
		"&precision=s"

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+h.influxToken)
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("influx write returned %d", resp.StatusCode)
	}
	return nil
}

// escapeTagValue escapes special characters in InfluxDB line protocol tag values.
func escapeTagValue(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, " ", "\\ ")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "=", "\\=")
	return s
}
