package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/farmsense/api/internal/middleware"
)

// Node represents a node record.
type Node struct {
	ID         string     `json:"id"`
	HubID      string     `json:"hub_id"`
	Name       string     `json:"name"`
	SensorType string     `json:"sensor_type"`
	Unit       string     `json:"unit"`
	LastSeen   *time.Time `json:"last_seen"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Reading represents a single sensor reading.
type Reading struct {
	Timestamp string  `json:"timestamp"`
	Value     float64 `json:"value"`
}

// NodeHandler handles node routes.
type NodeHandler struct {
	pool         *pgxpool.Pool
	influxURL    string
	influxToken  string
	influxOrg    string
	influxBucket string
}

// NewNodeHandler creates a new NodeHandler.
func NewNodeHandler(pool *pgxpool.Pool, influxURL, influxToken, influxOrg, influxBucket string) *NodeHandler {
	return &NodeHandler{
		pool:         pool,
		influxURL:    influxURL,
		influxToken:  influxToken,
		influxOrg:    influxOrg,
		influxBucket: influxBucket,
	}
}

// List handles GET /api/hubs/:hubId/nodes
func (h *NodeHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	hubID := chi.URLParam(r, "hubId")

	// Verify hub ownership via farm
	var hubExists bool
	err := h.pool.QueryRow(r.Context(),
		`SELECT EXISTS(
			SELECT 1 FROM hubs h
			JOIN farms f ON f.id = h.farm_id
			WHERE h.id = $1 AND f.user_id = $2
		)`,
		hubID, userID,
	).Scan(&hubExists)
	if err != nil || !hubExists {
		writeError(w, http.StatusNotFound, "hub not found")
		return
	}

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

// Create handles POST /api/hubs/:hubId/nodes
func (h *NodeHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	hubID := chi.URLParam(r, "hubId")

	// Verify hub ownership via farm
	var hubExists bool
	err := h.pool.QueryRow(r.Context(),
		`SELECT EXISTS(
			SELECT 1 FROM hubs h
			JOIN farms f ON f.id = h.farm_id
			WHERE h.id = $1 AND f.user_id = $2
		)`,
		hubID, userID,
	).Scan(&hubExists)
	if err != nil || !hubExists {
		writeError(w, http.StatusNotFound, "hub not found")
		return
	}

	var req struct {
		Name       string `json:"name"`
		SensorType string `json:"sensor_type"`
		Unit       string `json:"unit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.SensorType == "" {
		writeError(w, http.StatusBadRequest, "name and sensor_type are required")
		return
	}

	var n Node
	err = h.pool.QueryRow(r.Context(),
		`INSERT INTO nodes (hub_id, name, sensor_type, unit) VALUES ($1, $2, $3, $4)
		 RETURNING id, hub_id, name, sensor_type, unit, last_seen, created_at`,
		hubID, req.Name, req.SensorType, req.Unit,
	).Scan(&n.ID, &n.HubID, &n.Name, &n.SensorType, &n.Unit, &n.LastSeen, &n.CreatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create node")
		return
	}

	writeJSON(w, http.StatusCreated, n)
}

// Readings handles GET /api/nodes/:nodeId/readings?range=24h
func (h *NodeHandler) Readings(w http.ResponseWriter, r *http.Request) {
	userID, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	nodeID := chi.URLParam(r, "nodeId")

	// Verify node ownership via hub -> farm -> user
	var nodeExists bool
	err := h.pool.QueryRow(r.Context(),
		`SELECT EXISTS(
			SELECT 1 FROM nodes n
			JOIN hubs h ON h.id = n.hub_id
			JOIN farms f ON f.id = h.farm_id
			WHERE n.id = $1 AND f.user_id = $2
		)`,
		nodeID, userID,
	).Scan(&nodeExists)
	if err != nil || !nodeExists {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}

	rangeParam := r.URL.Query().Get("range")
	fluxRange := "-24h"
	switch rangeParam {
	case "1h":
		fluxRange = "-1h"
	case "24h", "":
		fluxRange = "-24h"
	case "7d":
		fluxRange = "-7d"
	case "30d":
		fluxRange = "-30d"
	}

	readings := h.queryInflux(nodeID, fluxRange)

	writeJSON(w, http.StatusOK, map[string]interface{}{"readings": readings})
}

// queryInflux queries InfluxDB for readings for a given node. Returns empty
// slice on any error to avoid surfacing InfluxDB failures to the client.
func (h *NodeHandler) queryInflux(nodeID, fluxRange string) []Reading {
	if h.influxURL == "" {
		return []Reading{}
	}

	fluxQuery := fmt.Sprintf(`
from(bucket: "%s")
  |> range(start: %s)
  |> filter(fn: (r) => r["_measurement"] == "sensor_reading")
  |> filter(fn: (r) => r["node_id"] == "%s")
  |> filter(fn: (r) => r["_field"] == "value")
  |> sort(columns: ["_time"])
`, h.influxBucket, fluxRange, nodeID)

	reqBody := map[string]string{
		"query": fluxQuery,
		"type":  "flux",
		"org":   h.influxOrg,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return []Reading{}
	}

	url := strings.TrimRight(h.influxURL, "/") + "/api/v2/query?org=" + h.influxOrg
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return []Reading{}
	}
	req.Header.Set("Authorization", "Token "+h.influxToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/csv")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return []Reading{}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return []Reading{}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return []Reading{}
	}

	return parseInfluxCSV(body)
}

// parseInfluxCSV parses the CSV response from InfluxDB Flux query API.
func parseInfluxCSV(data []byte) []Reading {
	readings := []Reading{}
	lines := strings.Split(string(data), "\n")

	// Find header line
	timeIdx := -1
	valueIdx := -1
	headerFound := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Split(line, ",")
		if !headerFound {
			// This is the header line
			for i, f := range fields {
				switch strings.TrimSpace(f) {
				case "_time":
					timeIdx = i
				case "_value":
					valueIdx = i
				}
			}
			headerFound = true
			continue
		}

		if timeIdx < 0 || valueIdx < 0 || len(fields) <= timeIdx || len(fields) <= valueIdx {
			continue
		}

		ts := strings.TrimSpace(fields[timeIdx])
		valStr := strings.TrimSpace(fields[valueIdx])
		if ts == "" || valStr == "" {
			continue
		}

		// Parse timestamp
		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			continue
		}

		// Parse value
		var val float64
		if _, err := fmt.Sscanf(valStr, "%f", &val); err != nil {
			continue
		}

		readings = append(readings, Reading{
			Timestamp: t.UTC().Format(time.RFC3339),
			Value:     val,
		})
	}

	return readings
}
