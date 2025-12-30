package api

import (
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"time"

	"goLangServer/config"
	"goLangServer/contract"
	"goLangServer/db"
	"goLangServer/ws"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
)

/* =========================
   REQUEST/RESPONSE TYPES
========================= */

// CrashRegisterRequest represents the crash game registration request
type CrashRegisterRequest struct {
	Address         string  `json:"address"`
	BetAmount       string  `json:"betAmount"`       // Wei as string
	EntryMultiplier float64 `json:"entryMultiplier"` // e.g., 1.5
	TxHash          string  `json:"txHash"`          // Transaction hash for verification
}

// CrashRegisterResponse represents the crash game registration response
type CrashRegisterResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	GameID  string `json:"gameId,omitempty"`
}

// CrashCashoutRequest represents the crash game cashout request
type CrashCashoutRequest struct {
	Address          string  `json:"address"`
	GameID           string  `json:"gameId"`
	CurrentMultiplier float64 `json:"currentMultiplier"` // Server-provided current multiplier
}

// CrashCashoutResponse represents the crash game cashout response
type CrashCashoutResponse struct {
	Success           bool    `json:"success"`
	Message           string  `json:"message"`
	Payout            string  `json:"payout,omitempty"`            // Wei as string
	CashoutMultiplier float64 `json:"cashoutMultiplier,omitempty"` // Actual cashout multiplier
	TxHash            string  `json:"txHash,omitempty"`            // Transaction hash
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

/* =========================
   CRASH GAME ENDPOINTS
========================= */

// HandleCrashRegister handles crash game registration
// POST /api/crash/register
func HandleCrashRegister(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request
	var req CrashRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate request
	if req.Address == "" {
		sendError(w, http.StatusBadRequest, "Address is required")
		return
	}
	if req.BetAmount == "" {
		sendError(w, http.StatusBadRequest, "Bet amount is required")
		return
	}
	if req.EntryMultiplier <= 0 {
		sendError(w, http.StatusBadRequest, "Entry multiplier must be positive")
		return
	}
	if req.TxHash == "" {
		sendError(w, http.StatusBadRequest, "Transaction hash is required")
		return
	}

	// Get current game ID from the crash game state
	// TODO: This should be pulled from the current running game
	// For now, we'll use a placeholder
	gameID := getCurrentCrashGameID()
	if gameID == "" {
		sendError(w, http.StatusServiceUnavailable, "No active crash game")
		return
	}

	// TODO: Verify the transaction on-chain
	// For now, we trust the client provided txHash
	// In production, verify:
	// 1. Transaction exists and succeeded
	// 2. From address matches req.Address
	// 3. msg.value matches req.BetAmount
	// 4. Transaction called buyIn() on correct contract

	// Check if player already has an active bet in this game
	existingBet, err := db.GetCrashBet(ctx, gameID, req.Address)
	if err != nil {
		log.Printf("❌ Failed to check existing bet: %v", err)
		sendError(w, http.StatusInternalServerError, "Failed to check existing bet")
		return
	}
	if existingBet != nil {
		sendError(w, http.StatusConflict, "Player already has an active bet in this game")
		return
	}

	// Store bet in Redis
	bet := &db.CrashBetData{
		PlayerAddress:   req.Address,
		GameID:          gameID,
		BetAmount:       req.BetAmount,
		EntryMultiplier: req.EntryMultiplier,
		Timestamp:       time.Now(),
		TxHash:          req.TxHash,
	}

	if err := db.StoreCrashBet(ctx, gameID, req.Address, bet); err != nil {
		log.Printf("❌ Failed to store crash bet: %v", err)
		sendError(w, http.StatusInternalServerError, "Failed to register bet")
		return
	}

	// Send success response
	response := CrashRegisterResponse{
		Success: true,
		Message: "Bet registered successfully",
		GameID:  gameID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	log.Printf("✅ Crash bet registered - Game: %s, Player: %s, Amount: %s, Entry: %.2fx",
		gameID, req.Address, req.BetAmount, req.EntryMultiplier)
}

// HandleCrashCashout handles crash game cashout
// POST /api/crash/cashout
func HandleCrashCashout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request
	var req CrashCashoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate request
	if req.Address == "" {
		sendError(w, http.StatusBadRequest, "Address is required")
		return
	}
	if req.GameID == "" {
		sendError(w, http.StatusBadRequest, "Game ID is required")
		return
	}
	if req.CurrentMultiplier <= 0 {
		sendError(w, http.StatusBadRequest, "Current multiplier must be positive")
		return
	}

	// Get active bet from Redis
	bet, err := db.GetCrashBet(ctx, req.GameID, req.Address)
	if err != nil {
		log.Printf("❌ Failed to get crash bet: %v", err)
		sendError(w, http.StatusInternalServerError, "Failed to get bet")
		return
	}
	if bet == nil {
		sendError(w, http.StatusNotFound, "No active bet found")
		return
	}

	// Calculate payout: payout = (betAmount * currentMultiplier) / entryMultiplier
	betAmountBig, ok := new(big.Int).SetString(bet.BetAmount, 10)
	if !ok {
		log.Printf("❌ Failed to parse bet amount: %s", bet.BetAmount)
		sendError(w, http.StatusInternalServerError, "Invalid bet amount")
		return
	}

	// Convert multipliers to wei format (18 decimals)
	currentMultiplierWei := config.MultiplierToWei(req.CurrentMultiplier)
	entryMultiplierWei := config.MultiplierToWei(bet.EntryMultiplier)

	// Calculate payout: (betAmount * currentMultiplier) / entryMultiplier
	payout := new(big.Int).Mul(betAmountBig, currentMultiplierWei)
	payout = payout.Div(payout, entryMultiplierWei)

	// Call contract to execute cashout (gasless - server pays gas)
	contractClient, err := contract.NewGameHouseContract()
	if err != nil {
		log.Printf("❌ Failed to initialize contract client: %v", err)
		sendError(w, http.StatusInternalServerError, "Failed to execute cashout")
		return
	}
	defer contractClient.Close()

	// Convert gameID string to big.Int
	gameIDBig, ok := new(big.Int).SetString(req.GameID, 10)
	if !ok {
		log.Printf("❌ Failed to parse game ID: %s", req.GameID)
		sendError(w, http.StatusInternalServerError, "Invalid game ID")
		return
	}

	// Create transactor (server pays gas)
	chainIDBig := big.NewInt(5003) // Mantle Sepolia
	auth, err := bind.NewKeyedTransactorWithChainID(contractClient.PrivateKey, chainIDBig)
	if err != nil {
		log.Printf("❌ Failed to create transactor: %v", err)
		sendError(w, http.StatusInternalServerError, "Failed to create transaction")
		return
	}

	// Get gas parameters
	nonce, err := contractClient.Client.PendingNonceAt(ctx, contractClient.FromAddress)
	if err != nil {
		log.Printf("❌ Failed to get nonce: %v", err)
		sendError(w, http.StatusInternalServerError, "Failed to prepare transaction")
		return
	}
	auth.Nonce = big.NewInt(int64(nonce))

	gasPrice, err := contractClient.Client.SuggestGasPrice(ctx)
	if err != nil {
		log.Printf("❌ Failed to get gas price: %v", err)
		sendError(w, http.StatusInternalServerError, "Failed to prepare transaction")
		return
	}
	auth.GasPrice = gasPrice
	auth.GasLimit = uint64(config.RelayerGasLimit)

	// Execute cashOutFor on contract (server pays gas)
	playerAddress := common.HexToAddress(req.Address)
	tx, err := contractClient.CashOutFor(auth, playerAddress, gameIDBig, currentMultiplierWei)
	if err != nil {
		log.Printf("❌ Failed to execute cashout: %v", err)
		sendError(w, http.StatusInternalServerError, fmt.Sprintf("Cashout failed: %v", err))
		return
	}

	txHash := tx.Hash().Hex()

	// Delete active bet from Redis
	if err := db.DeleteCrashBet(ctx, req.GameID, req.Address); err != nil {
		log.Printf("⚠️  Failed to delete crash bet: %v", err)
		// Don't fail the request, cashout was successful
	}

	// Send success response
	response := CrashCashoutResponse{
		Success:           true,
		Message:           "Cashout successful",
		Payout:            payout.String(),
		CashoutMultiplier: req.CurrentMultiplier,
		TxHash:            txHash,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	log.Printf("✅ Crash cashout - Game: %s, Player: %s, Payout: %s, Multiplier: %.2fx, TX: %s",
		req.GameID, req.Address, payout.String(), req.CurrentMultiplier, txHash)
}

/* =========================
   HELPER FUNCTIONS
========================= */

// sendError sends an error response
func sendError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(ErrorResponse{
		Success: false,
		Error:   message,
	})
}

// getCurrentCrashGameID returns the current crash game ID from the running game
func getCurrentCrashGameID() string {
	return ws.GetCurrentGameID()
}
