// api/crash.go
package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"goLangServer/db"
)

/* =========================
   RESPONSE TYPES
========================= */

// CrashGameHistoryResponse represents a single crash game
type CrashGameHistoryResponse struct {
	GameID    string      `json:"gameId"`
	Peak      float64     `json:"peak"`
	Candles   interface{} `json:"candles"`
	Timestamp string      `json:"timestamp"`
}

// CrashHistoryResponse represents the list of crash games
type CrashHistoryResponse struct {
	Success bool                       `json:"success"`
	Games   []CrashGameHistoryResponse `json:"games"`
	Count   int                        `json:"count"`
}

// CrashGameDetailResponse represents detailed game info for verification
type CrashGameDetailResponse struct {
	Success    bool        `json:"success"`
	GameID     string      `json:"gameId"`
	ServerSeed string      `json:"serverSeed"`
	Peak       float64     `json:"peak"`
	Candles    interface{} `json:"candles"`
	Timestamp  string      `json:"timestamp"`
	Message    string      `json:"message,omitempty"`
}

/* =========================
   HTTP ENDPOINTS
========================= */

// HandleGetCrashHistory handles GET /api/crash
// Returns recent crash game history (last 50 games)
func HandleGetCrashHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx := context.Background()

	// Get recent crash history from PostgreSQL (last 50 games)
	history, err := db.GetRecentCrashHistory(ctx, 50)
	if err != nil {
		log.Printf("❌ Failed to get crash history: %v", err)
		sendError(w, http.StatusInternalServerError, "Failed to retrieve crash history")
		return
	}

	// Build response
	response := CrashHistoryResponse{
		Success: true,
		Games:   make([]CrashGameHistoryResponse, 0, len(history)),
		Count:   len(history),
	}

	for _, game := range history {
		response.Games = append(response.Games, CrashGameHistoryResponse{
			GameID:    game.GameID,
			Peak:      game.Peak,
			Candles:   game.CandlestickHistory,
			Timestamp: game.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	log.Printf("📋 Retrieved %d crash games", len(history))
}

// HandleGetCrashGameDetail handles GET /api/crash/:gameId
// Returns detailed game data for verification
func HandleGetCrashGameDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Extract game ID from URL path
	// Expected format: /api/crash/{gameId}
	path := r.URL.Path
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		sendError(w, http.StatusBadRequest, "Game ID is required")
		return
	}
	gameID := parts[3]

	if gameID == "" {
		sendError(w, http.StatusBadRequest, "Game ID is required")
		return
	}

	ctx := context.Background()

	// Get game history from PostgreSQL
	game, err := db.GetCrashHistory(ctx, gameID)
	if err != nil {
		log.Printf("❌ Failed to get crash game: %v", err)
		sendError(w, http.StatusInternalServerError, "Failed to retrieve game data")
		return
	}

	if game == nil {
		sendError(w, http.StatusNotFound, "Game not found")
		return
	}

	// Build response with server seed for provably fair verification
	response := CrashGameDetailResponse{
		Success:    true,
		GameID:     game.GameID,
		ServerSeed: game.ServerSeed,
		Peak:       game.Peak,
		Candles:    game.CandlestickHistory,
		Timestamp:  game.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		Message:    "Verify by using serverSeed to recalculate game outcome",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	log.Printf("🔍 Retrieved crash game: %s", gameID)
}
