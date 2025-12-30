package api

import (
	"encoding/json"
	"log"
	"net/http"

	"goLangServer/db"
)

/* =========================
   REQUEST/RESPONSE TYPES
========================= */

// VerifyGameResponse represents the game verification response
type VerifyGameResponse struct {
	Success            bool                   `json:"success"`
	GameID             string                 `json:"gameId"`
	ServerSeed         string                 `json:"serverSeed"`
	ServerSeedHash     string                 `json:"serverSeedHash"`
	Peak               float64                `json:"peak"`
	Rugged             bool                   `json:"rugged"`
	CandlestickHistory interface{}            `json:"candlestickHistory"`
	Message            string                 `json:"message,omitempty"`
}

/* =========================
   VERIFICATION ENDPOINT
========================= */

// HandleVerifyGame handles game verification requests
// GET /api/verify/:gameId
func HandleVerifyGame(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract game ID from URL path
	// Expected format: /api/verify/{gameId}
	gameID := r.URL.Path[len("/api/verify/"):]
	if gameID == "" {
		sendError(w, http.StatusBadRequest, "Game ID is required")
		return
	}

	// Get game history from PostgreSQL
	history, err := db.GetCrashHistory(ctx, gameID)
	if err != nil {
		log.Printf("❌ Failed to get crash history: %v", err)
		sendError(w, http.StatusInternalServerError, "Failed to retrieve game history")
		return
	}

	if history == nil {
		sendError(w, http.StatusNotFound, "Game not found")
		return
	}

	// Send response with provably fair data
	response := VerifyGameResponse{
		Success:            true,
		GameID:             history.GameID,
		ServerSeed:         history.ServerSeed,
		ServerSeedHash:     history.ServerSeedHash,
		Peak:               history.Peak,
		Rugged:             history.Rugged,
		CandlestickHistory: history.CandlestickHistory,
		Message:            "Game data retrieved successfully. Verify by hashing the serverSeed and comparing with serverSeedHash.",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	log.Printf("🔍 Game verification - Game: %s", gameID)
}

// HandleHealthCheck handles health check requests
// GET /api/health
func HandleHealthCheck(w http.ResponseWriter, r *http.Request) {
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
