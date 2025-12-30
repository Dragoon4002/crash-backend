package api

import (
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"time"

	"goLangServer/config"
	"goLangServer/db"
)

/* =========================
   REQUEST/RESPONSE TYPES
========================= */

// CandleFlipRegisterRequest represents the candleflip registration request
type CandleFlipRegisterRequest struct {
	Address    string `json:"address"`
	BetPerRoom string `json:"betPerRoom"` // Wei as string
	Rooms      uint64 `json:"rooms"`
	TxHash     string `json:"txHash"`
}

// CandleFlipRegisterResponse represents the candleflip registration response
type CandleFlipRegisterResponse struct {
	Success  bool    `json:"success"`
	Message  string  `json:"message"`
	GameID   string  `json:"gameId,omitempty"`
	Odds     float64 `json:"odds,omitempty"`
	Exposure string  `json:"exposure,omitempty"` // Wei as string
}

// CandleFlipPreviewOddsRequest represents the preview odds request
type CandleFlipPreviewOddsRequest struct {
	BetPerRoom string `json:"betPerRoom"` // Wei as string
	Rooms      uint64 `json:"rooms"`
}

// CandleFlipPreviewOddsResponse represents the preview odds response
type CandleFlipPreviewOddsResponse struct {
	Success  bool    `json:"success"`
	Odds     float64 `json:"odds"`
	Exposure string  `json:"exposure"` // Wei as string
	Message  string  `json:"message,omitempty"`
}

/* =========================
   CANDLEFLIP ENDPOINTS
========================= */

// HandleCandleFlipRegister handles candleflip game registration
// POST /api/candle/register
func HandleCandleFlipRegister(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request
	var req CandleFlipRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate request
	if req.Address == "" {
		sendError(w, http.StatusBadRequest, "Address is required")
		return
	}
	if req.BetPerRoom == "" {
		sendError(w, http.StatusBadRequest, "Bet per room is required")
		return
	}
	if req.Rooms < config.MinRooms || req.Rooms > config.MaxRooms {
		sendError(w, http.StatusBadRequest, fmt.Sprintf("Rooms must be between %d and %d", config.MinRooms, config.MaxRooms))
		return
	}
	if req.TxHash == "" {
		sendError(w, http.StatusBadRequest, "Transaction hash is required")
		return
	}

	// Parse bet per room
	betPerRoomBig, ok := new(big.Int).SetString(req.BetPerRoom, 10)
	if !ok {
		sendError(w, http.StatusBadRequest, "Invalid bet per room")
		return
	}

	// Calculate exposure: betPerRoom * rooms * 2
	roomsBig := big.NewInt(int64(req.Rooms))
	twoBig := big.NewInt(2)
	exposure := new(big.Int).Mul(betPerRoomBig, roomsBig)
	exposure = exposure.Mul(exposure, twoBig)

	// TODO: Verify the transaction on-chain
	// For now, we trust the client provided txHash

	// Calculate odds (same logic as contract)
	odds := calculateOdds(exposure)

	// Generate unique game ID (timestamp + address suffix)
	gameID := fmt.Sprintf("%d-%s", time.Now().UnixNano(), req.Address[len(req.Address)-6:])

	// Store game in Redis
	game := &db.CandleFlipGameData{
		PlayerAddress: req.Address,
		GameID:        gameID,
		BetPerRoom:    req.BetPerRoom,
		Rooms:         req.Rooms,
		Odds:          odds,
		Exposure:      exposure.String(),
		Timestamp:     time.Now(),
		TxHash:        req.TxHash,
	}

	if err := db.StoreCandleFlipGame(ctx, gameID, req.Address, game); err != nil {
		log.Printf("❌ Failed to store candleflip game: %v", err)
		sendError(w, http.StatusInternalServerError, "Failed to register game")
		return
	}

	// Send success response
	response := CandleFlipRegisterResponse{
		Success:  true,
		Message:  "CandleFlip game registered successfully",
		GameID:   gameID,
		Odds:     odds,
		Exposure: exposure.String(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	log.Printf("✅ CandleFlip registered - Game: %s, Player: %s, Rooms: %d, Odds: %.2fx",
		gameID, req.Address, req.Rooms, odds)

	// TODO: Start game simulation for this player
	// This should trigger the server to run the candleflip rooms and broadcast results via WebSocket
}

// HandleCandleFlipPreviewOdds handles preview odds calculation
// POST /api/candle/preview-odds
func HandleCandleFlipPreviewOdds(w http.ResponseWriter, r *http.Request) {
	// Parse request
	var req CandleFlipPreviewOddsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate request
	if req.BetPerRoom == "" {
		sendError(w, http.StatusBadRequest, "Bet per room is required")
		return
	}
	if req.Rooms < config.MinRooms || req.Rooms > config.MaxRooms {
		sendError(w, http.StatusBadRequest, fmt.Sprintf("Rooms must be between %d and %d", config.MinRooms, config.MaxRooms))
		return
	}

	// Parse bet per room
	betPerRoomBig, ok := new(big.Int).SetString(req.BetPerRoom, 10)
	if !ok {
		sendError(w, http.StatusBadRequest, "Invalid bet per room")
		return
	}

	// Calculate exposure: betPerRoom * rooms * 2
	roomsBig := big.NewInt(int64(req.Rooms))
	twoBig := big.NewInt(2)
	exposure := new(big.Int).Mul(betPerRoomBig, roomsBig)
	exposure = exposure.Mul(exposure, twoBig)

	// Calculate odds
	odds := calculateOdds(exposure)

	// Send response
	response := CandleFlipPreviewOddsResponse{
		Success:  true,
		Odds:     odds,
		Exposure: exposure.String(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	log.Printf("📊 Odds preview - BetPerRoom: %s, Rooms: %d, Odds: %.2fx", req.BetPerRoom, req.Rooms, odds)
}

/* =========================
   ODDS CALCULATION
========================= */

// calculateOdds calculates dynamic odds based on house liquidity
// This mirrors the contract logic in GameHouseV2.sol
func calculateOdds(singleGameExposure *big.Int) float64 {
	// TODO: Get actual house balance and active exposure from contract
	// For now, using placeholder values
	houseBalance := big.NewInt(0).Mul(big.NewInt(100), big.NewInt(1e18))   // 100 MNT
	activeExposure := big.NewInt(0).Mul(big.NewInt(10), big.NewInt(1e18)) // 10 MNT

	// Calculate required reserve: exposure * RESERVE_GAMES
	reserveGamesBig := big.NewInt(int64(config.ReserveGames))
	requiredReserve := new(big.Int).Mul(singleGameExposure, reserveGamesBig)

	// If house balance <= active exposure, return min odds
	if houseBalance.Cmp(activeExposure) <= 0 {
		return config.GetMinOddsFloat()
	}

	// Calculate available balance
	availableBalance := new(big.Int).Sub(houseBalance, activeExposure)

	// Calculate risk factor
	var riskFactor *big.Int
	if availableBalance.Cmp(requiredReserve) >= 0 {
		// Full odds
		riskFactor = big.NewInt(1e18)
	} else {
		// Scaled odds: (availableBalance * 1e18) / requiredReserve
		riskFactor = new(big.Int).Mul(availableBalance, big.NewInt(1e18))
		riskFactor = riskFactor.Div(riskFactor, requiredReserve)
	}

	// Calculate odds: (BASE_ODDS * riskFactor) / 1e18
	odds := new(big.Int).Mul(config.BaseOdds, riskFactor)
	odds = odds.Div(odds, big.NewInt(1e18))

	// Convert to float
	oddsFloat := config.WeiToMultiplier(odds)

	// Enforce minimum odds
	minOdds := config.GetMinOddsFloat()
	if oddsFloat < minOdds {
		return minOdds
	}

	return oddsFloat
}
