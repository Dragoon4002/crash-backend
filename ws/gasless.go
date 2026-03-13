package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// GaslessCashOutRequest represents a request for gasless cashout
type GaslessCashOutRequest struct {
	PlayerAddress     string `json:"playerAddress"`
	GameID            string `json:"gameId"`
	CurrentMultiplier string `json:"currentMultiplier"`
	Signature         string `json:"signature"` // Player's signature authorizing the cashout
}

// GaslessCashOutResponse represents the response
type GaslessCashOutResponse struct {
	Success         bool   `json:"success"`
	TransactionHash string `json:"transactionHash,omitempty"`
	Error           string `json:"error,omitempty"`
	Payout          string `json:"payout,omitempty"`
}

// HandleGaslessCashOut handles gasless cashout requests
func HandleGaslessCashOut(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req GaslessCashOutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate inputs
	if req.PlayerAddress == "" {
		sendJSONError(w, "Invalid player address", http.StatusBadRequest)
		return
	}

	log.Printf("🎮 Gasless cashout request from %s for game %s at %sx",
		req.PlayerAddress, req.GameID, req.CurrentMultiplier)

	// Execute gasless cashout via relayer
	ctx := context.Background()
	txHash, payout, err := executeGaslessCashOut(ctx, req.PlayerAddress, req.GameID, req.CurrentMultiplier, req.Signature)

	if err != nil {
		log.Printf("❌ Gasless cashout failed: %v", err)
		sendJSONResponse(w, GaslessCashOutResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	log.Printf("✅ Gasless cashout successful! TX: %s, Payout: %s MNT", txHash, payout)

	sendJSONResponse(w, GaslessCashOutResponse{
		Success:         true,
		TransactionHash: txHash,
		Payout:          payout,
	})
}

// executeGaslessCashOut executes the cashout via the relayer (stub — not implemented for Stellar)
func executeGaslessCashOut(_ context.Context, player, gameID, multiplier, _ string) (string, string, error) {
	return "", "0.000", fmt.Errorf("gasless cashout not implemented for Stellar (player=%s game=%s mult=%s)", player, gameID, multiplier)
}

// Helper function to send JSON responses
func sendJSONResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// Helper function to send JSON errors
func sendJSONError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}

// HandleGaslessBuyIn handles gasless buy-in requests
func HandleGaslessBuyIn(w http.ResponseWriter, r *http.Request) {
	// Similar to cashout but for buy-in
	sendJSONError(w, "Not implemented yet", http.StatusNotImplemented)
}

// Request types for bettor notifications
type AddBettorRequest struct {
	Address    string  `json:"address"`
	BetAmount  float64 `json:"betAmount"`
	Multiplier float64 `json:"multiplier"`
}

type RemoveBettorRequest struct {
	Address string `json:"address"`
}

// HandleAddBettor processes notifications when a player places a bet
func HandleAddBettor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AddBettorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("❌ Failed to parse add bettor request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate request
	if req.Address == "" {
		http.Error(w, "Missing address", http.StatusBadRequest)
		return
	}
	if req.BetAmount <= 0 {
		http.Error(w, "Invalid bet amount", http.StatusBadRequest)
		return
	}
	if req.Multiplier <= 0 {
		http.Error(w, "Invalid multiplier", http.StatusBadRequest)
		return
	}

	// Add bettor to active list
	AddActiveBettor(req.Address, req.BetAmount, req.Multiplier)

	// Send success response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Bettor added",
	})
}

// HandleRemoveBettor processes notifications when a player cashes out
func HandleRemoveBettor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RemoveBettorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("❌ Failed to parse remove bettor request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate request
	if req.Address == "" {
		http.Error(w, "Missing address", http.StatusBadRequest)
		return
	}

	// Remove bettor from active list
	RemoveActiveBettor(req.Address)

	// Send success response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Bettor removed",
	})
}
