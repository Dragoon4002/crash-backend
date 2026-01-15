// api/leaderboard.go
package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"goLangServer/db"
)

/* =========================
   RESPONSE TYPES
========================= */

// LeaderboardEntryResponse represents a single leaderboard entry
type LeaderboardEntryResponse struct {
	Rank          int     `json:"rank"`
	WalletAddress string  `json:"walletAddress"`
	Pnl           float64 `json:"pnl"`
}

// LeaderboardResponse represents the leaderboard API response
type LeaderboardResponse struct {
	Success      bool                       `json:"success"`
	Leaderboard  []LeaderboardEntryResponse `json:"leaderboard"`
	UserPosition *LeaderboardEntryResponse  `json:"userPosition,omitempty"`
}

/* =========================
   HTTP ENDPOINTS
========================= */

// HandleGetLeaderboard handles GET /api/leaderboard
// Query params: wallet (optional) - get user's position
func HandleGetLeaderboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx := context.Background()

	// Get top 20 leaderboard
	records, err := db.GetWalletPnLLeaderboard(ctx, 20)
	if err != nil {
		log.Printf("❌ Failed to get leaderboard: %v", err)
		sendError(w, http.StatusInternalServerError, "Failed to retrieve leaderboard")
		return
	}

	// Build response
	response := LeaderboardResponse{
		Success:     true,
		Leaderboard: make([]LeaderboardEntryResponse, 0, len(records)),
	}

	for _, record := range records {
		response.Leaderboard = append(response.Leaderboard, LeaderboardEntryResponse{
			Rank:          record.Rank,
			WalletAddress: record.WalletAddress,
			Pnl:           record.Amount,
		})
	}

	// Check for user wallet query param
	walletParam := r.URL.Query().Get("wallet")
	if walletParam != "" {
		// Check if user is already in top 20
		userInTop := false
		for _, entry := range response.Leaderboard {
			if entry.WalletAddress == walletParam {
				userInTop = true
				break
			}
		}

		// If not in top 20, fetch their position
		if !userInTop {
			userRecord, err := db.GetWalletPnLRank(ctx, walletParam)
			if err != nil {
				log.Printf("⚠️  Failed to get user rank: %v", err)
			} else if userRecord != nil {
				response.UserPosition = &LeaderboardEntryResponse{
					Rank:          userRecord.Rank,
					WalletAddress: userRecord.WalletAddress,
					Pnl:           userRecord.Amount,
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(response)

	log.Printf("📋 Retrieved leaderboard with %d entries", len(records))
}
