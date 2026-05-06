package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
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
	Owned     bool      `json:"owned"`
}

// FarmMember represents a user who has been granted access to a farm.
type FarmMember struct {
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	InvitedBy string    `json:"invited_by"`
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

// userCanAccessFarm returns true if the user owns or is a member of the farm.
// Used by hub and node handlers to enforce consistent access control.
func userCanAccessFarm(ctx context.Context, pool *pgxpool.Pool, farmID, userID string) bool {
	var ok bool
	pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM farms f
			WHERE f.id = $1 AND (
				f.user_id = $2
				OR EXISTS(SELECT 1 FROM farm_members fm WHERE fm.farm_id = f.id AND fm.user_id = $2)
			)
		)`, farmID, userID).Scan(&ok)
	return ok
}

// List handles GET /api/farms
func (h *FarmHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	rows, err := h.pool.Query(r.Context(), `
		SELECT f.id, f.user_id, f.name, f.location, f.created_at, (f.user_id = $1) AS owned
		FROM farms f
		WHERE f.user_id = $1
		   OR EXISTS(SELECT 1 FROM farm_members fm WHERE fm.farm_id = f.id AND fm.user_id = $1)
		ORDER BY f.created_at DESC`,
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
		if err := rows.Scan(&f.ID, &f.UserID, &f.Name, &f.Location, &f.CreatedAt, &f.Owned); err != nil {
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
	f.Owned = true

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
	err := h.pool.QueryRow(r.Context(), `
		SELECT f.id, f.user_id, f.name, f.location, f.created_at, (f.user_id = $2) AS owned
		FROM farms f
		WHERE f.id = $1 AND (
			f.user_id = $2
			OR EXISTS(SELECT 1 FROM farm_members fm WHERE fm.farm_id = f.id AND fm.user_id = $2)
		)`,
		farmID, userID,
	).Scan(&f.ID, &f.UserID, &f.Name, &f.Location, &f.CreatedAt, &f.Owned)
	if err != nil {
		writeError(w, http.StatusNotFound, "farm not found")
		return
	}

	writeJSON(w, http.StatusOK, f)
}

// Delete handles DELETE /api/farms/:farmId — owner only
func (h *FarmHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	farmID := chi.URLParam(r, "farmId")

	result, err := h.pool.Exec(r.Context(),
		`DELETE FROM farms WHERE id = $1 AND user_id = $2`,
		farmID, userID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete farm")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "farm not found or not owner")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Stats handles GET /api/farms/:farmId/stats
func (h *FarmHandler) Stats(w http.ResponseWriter, r *http.Request) {
	userID, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	farmID := chi.URLParam(r, "farmId")

	var farmExists bool
	err := h.pool.QueryRow(r.Context(), `
		SELECT EXISTS(
			SELECT 1 FROM farms f
			WHERE f.id = $1 AND (
				f.user_id = $2
				OR EXISTS(SELECT 1 FROM farm_members fm WHERE fm.farm_id = f.id AND fm.user_id = $2)
			)
		)`, farmID, userID,
	).Scan(&farmExists)
	if err != nil || !farmExists {
		writeError(w, http.StatusNotFound, "farm not found")
		return
	}

	var hubCount, nodeCount, onlineHubs int
	err = h.pool.QueryRow(r.Context(), `
		SELECT
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

// ListMembers handles GET /api/farms/:farmId/members
func (h *FarmHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	userID, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	farmID := chi.URLParam(r, "farmId")

	var canAccess bool
	h.pool.QueryRow(r.Context(), `
		SELECT EXISTS(
			SELECT 1 FROM farms f
			WHERE f.id = $1 AND (
				f.user_id = $2
				OR EXISTS(SELECT 1 FROM farm_members fm WHERE fm.farm_id = f.id AND fm.user_id = $2)
			)
		)`, farmID, userID,
	).Scan(&canAccess)
	if !canAccess {
		writeError(w, http.StatusNotFound, "farm not found")
		return
	}

	rows, err := h.pool.Query(r.Context(), `
		SELECT u.id, u.email, u.name, fm.invited_by, fm.created_at
		FROM farm_members fm
		JOIN users u ON u.id = fm.user_id
		WHERE fm.farm_id = $1
		ORDER BY fm.created_at ASC`,
		farmID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query members")
		return
	}
	defer rows.Close()

	members := []FarmMember{}
	for rows.Next() {
		var m FarmMember
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.InvitedBy, &m.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan member")
			return
		}
		members = append(members, m)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"members": members})
}

// AddMember handles POST /api/farms/:farmId/members — owner only
func (h *FarmHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	userID, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	farmID := chi.URLParam(r, "farmId")

	// Only farm owner can invite
	var isOwner bool
	h.pool.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM farms WHERE id = $1 AND user_id = $2)`,
		farmID, userID,
	).Scan(&isOwner)
	if !isOwner {
		writeError(w, http.StatusForbidden, "only the farm owner can invite members")
		return
	}

	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	// Look up target user by email
	var targetID, targetEmail, targetName string
	err := h.pool.QueryRow(r.Context(),
		`SELECT id, email, name FROM users WHERE email = $1`,
		req.Email,
	).Scan(&targetID, &targetEmail, &targetName)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no account found with that email address")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up user")
		return
	}

	if targetID == userID {
		writeError(w, http.StatusBadRequest, "you already own this farm")
		return
	}

	// Upsert — ignore if already a member
	_, err = h.pool.Exec(r.Context(),
		`INSERT INTO farm_members (farm_id, user_id, invited_by)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (farm_id, user_id) DO NOTHING`,
		farmID, targetID, userID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add member")
		return
	}

	writeJSON(w, http.StatusCreated, FarmMember{
		UserID:    targetID,
		Email:     targetEmail,
		Name:      targetName,
		InvitedBy: userID,
	})
}

// RemoveMember handles DELETE /api/farms/:farmId/members/:userId — owner only
func (h *FarmHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	userID, ok := authmw.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	farmID := chi.URLParam(r, "farmId")
	targetID := chi.URLParam(r, "userId")

	var isOwner bool
	h.pool.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM farms WHERE id = $1 AND user_id = $2)`,
		farmID, userID,
	).Scan(&isOwner)
	if !isOwner {
		writeError(w, http.StatusForbidden, "only the farm owner can remove members")
		return
	}

	result, err := h.pool.Exec(r.Context(),
		`DELETE FROM farm_members WHERE farm_id = $1 AND user_id = $2`,
		farmID, targetID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove member")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
