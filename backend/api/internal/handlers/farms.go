package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/farmsense/api/internal/middleware"
)

// Farm represents a farm record.
type Farm struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	Location  string    `json:"location"`
	CreatedAt time.Time `json:"created_at"`
}

// FarmHandler handles farm routes.
type FarmHandler struct {
	pool *pgxpool.Pool
}

// NewFarmHandler creates a new FarmHandler.
func NewFarmHandler(pool *pgxpool.Pool) *FarmHandler {
	return &FarmHandler{pool: pool}
}

// List handles GET /api/farms
func (h *FarmHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	rows, err := h.pool.Query(r.Context(),
		`SELECT id, user_id, name, location, created_at FROM farms WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query farms")
		return
	}
	defer rows.Close()

	farms := []Farm{}
	for rows.Next() {
		var f Farm
		if err := rows.Scan(&f.ID, &f.UserID, &f.Name, &f.Location, &f.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan farm")
			return
		}
		farms = append(farms, f)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"farms": farms})
}

// Create handles POST /api/farms
func (h *FarmHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req struct {
		Name     string `json:"name"`
		Location string `json:"location"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	var f Farm
	err := h.pool.QueryRow(r.Context(),
		`INSERT INTO farms (user_id, name, location) VALUES ($1, $2, $3)
		 RETURNING id, user_id, name, location, created_at`,
		userID, req.Name, req.Location,
	).Scan(&f.ID, &f.UserID, &f.Name, &f.Location, &f.CreatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create farm")
		return
	}

	writeJSON(w, http.StatusCreated, f)
}

// Get handles GET /api/farms/:farmId
func (h *FarmHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	farmID := chi.URLParam(r, "farmId")

	var f Farm
	err := h.pool.QueryRow(r.Context(),
		`SELECT id, user_id, name, location, created_at FROM farms WHERE id = $1 AND user_id = $2`,
		farmID, userID,
	).Scan(&f.ID, &f.UserID, &f.Name, &f.Location, &f.CreatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "farm not found")
		return
	}

	writeJSON(w, http.StatusOK, f)
}

// Stats handles GET /api/farms/:farmId/stats
func (h *FarmHandler) Stats(w http.ResponseWriter, r *http.Request) {
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

	var hubCount, nodeCount, onlineHubs int
	err = h.pool.QueryRow(r.Context(),
		`SELECT
			COUNT(DISTINCT h.id) AS hub_count,
			COUNT(DISTINCT n.id) AS node_count,
			COUNT(DISTINCT CASE WHEN h.last_seen > NOW() - INTERVAL '5 minutes' THEN h.id END) AS online_hubs
		 FROM hubs h
		 LEFT JOIN nodes n ON n.hub_id = h.id
		 WHERE h.farm_id = $1`,
		farmID,
	).Scan(&hubCount, &nodeCount, &onlineHubs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query stats")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"hub_count":       hubCount,
		"node_count":      nodeCount,
		"online_hubs":     onlineHubs,
		"latest_readings": map[string]float64{},
	})
}
