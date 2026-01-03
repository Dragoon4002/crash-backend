package api

import (
	"encoding/json"
	"net/http"

	"goLangServer/db"
)

/* =========================
   HEALTH CHECK ENDPOINT
========================= */

// HandleHealthCheck handles health check requests
// GET /api/health
func HandleHealthCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx := r.Context()

	// Check Redis
	redisHealth := "ok"
	if err := db.HealthCheck(ctx); err != nil {
		redisHealth = "error: " + err.Error()
	}

	// Check PostgreSQL
	postgresHealth := "ok"
	if err := db.HealthCheckPostgres(ctx); err != nil {
		postgresHealth = "error: " + err.Error()
	}

	response := map[string]interface{}{
		"success":  true,
		"redis":    redisHealth,
		"postgres": postgresHealth,
		"message":  "Health check completed",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
