package ws

import (
	"encoding/json"
	"goLangServer/crypto"
	"goLangServer/game"
	"log"
	"net/http"
)

type VerifyRequest struct {
	ServerSeed     string `json:"serverSeed"`
	ServerSeedHash string `json:"serverSeedHash"`
	GameID         string `json:"gameId"`
}

type VerifyResponse struct {
	Valid          bool    `json:"valid"`
	PeakMultiplier float64 `json:"peakMultiplier,omitempty"`
	Error          string  `json:"error,omitempty"`
}

// HandleVerifyGame verifies a game result by recalculating the peak multiplier
// from the server seed and game ID
func HandleVerifyGame(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		json.NewEncoder(w).Encode(VerifyResponse{
			Valid: false,
			Error: "Method not allowed. Use POST.",
		})
		return
	}

	var req VerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(VerifyResponse{
			Valid: false,
			Error: "Invalid request body",
		})
		return
	}

	// Validate required fields
	if req.ServerSeed == "" || req.ServerSeedHash == "" || req.GameID == "" {
		json.NewEncoder(w).Encode(VerifyResponse{
			Valid: false,
			Error: "Missing required fields: serverSeed, serverSeedHash, gameId",
		})
		return
	}

	// Verify the server seed hash
	if !crypto.VerifySeed(req.ServerSeed, req.ServerSeedHash) {
		json.NewEncoder(w).Encode(VerifyResponse{
			Valid: false,
			Error: "Server seed hash does not match",
		})
		return
	}

	// Calculate the peak multiplier
	peak := game.VerifyGamePeak(req.ServerSeed, req.GameID)

	log.Printf("✅ Game verified - GameID: %s, Peak: %.2fx", req.GameID, peak)

	json.NewEncoder(w).Encode(VerifyResponse{
		Valid:          true,
		PeakMultiplier: peak,
	})
}
