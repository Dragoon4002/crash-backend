package api

import (
	"encoding/json"
	"log"
	"net/http"
	// "time"

	"goLangServer/ws"
)

// AddBettorRequest represents a request to add an active bettor
type AddBettorRequest struct {
	Address         string  `json:"address"`
	BetAmount       float64 `json:"betAmount"`
	EntryMultiplier float64 `json:"entryMultiplier"`
}

// RemoveBettorRequest represents a request to remove an active bettor
type RemoveBettorRequest struct {
	Address string `json:"address"`
}

// HandleAddBettor handles POST /api/bettor/add
func HandleAddBettor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AddBettorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Address == "" {
		http.Error(w, "Address is required", http.StatusBadRequest)
		return
	}

	if req.BetAmount <= 0 {
		http.Error(w, "Bet amount must be positive", http.StatusBadRequest)
		return
	}

	if req.EntryMultiplier <= 0 {
		http.Error(w, "Entry multiplier must be positive", http.StatusBadRequest)
		return
	}

	ws.AddActiveBettor(req.Address, req.BetAmount, req.EntryMultiplier)

	log.Printf("✅ Added active bettor: %s (%.4f MNT @ %.2fx)", req.Address, req.BetAmount, req.EntryMultiplier)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Bettor added successfully",
	})
}

// HandleRemoveBettor handles POST /api/bettor/remove
func HandleRemoveBettor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RemoveBettorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Address == "" {
		http.Error(w, "Address is required", http.StatusBadRequest)
		return
	}

	ws.RemoveActiveBettor(req.Address)

	log.Printf("✅ Removed active bettor: %s", req.Address)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Bettor removed successfully",
	})
}

// HandleGetActiveBettors handles GET /api/bettor/list
func HandleGetActiveBettors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bettors := ws.GetActiveBettors()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"bettors": bettors,
		"count":   len(bettors),
	})
}
