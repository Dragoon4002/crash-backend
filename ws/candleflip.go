// ws/candleflip.go
package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"sync"
	"time"

	"goLangServer/config"
	"goLangServer/contract"
	"goLangServer/crypto"
	"goLangServer/game"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/websocket"
)

// Room represents a single CandleFlip room in a batch
type Room struct {
	RoomNumber  int       `json:"roomNumber"`
	Status      string    `json:"status"` // "waiting", "running", "completed"
	FinalPrice  float64   `json:"finalPrice,omitempty"`
	Winner      string    `json:"winner,omitempty"` // "bull" or "bear"
	PlayerWon   bool      `json:"playerWon"`
	StartTime   time.Time `json:"startTime,omitempty"`
	EndTime     time.Time `json:"endTime,omitempty"`
}

// CandleflipBatch represents a batch of rooms for a single player
type CandleflipBatch struct {
	BatchID        string
	PlayerAddress  common.Address
	AmountPerRoom  *big.Int
	TotalRooms     int
	PlayerSide     string // "bull" or "bear"
	Rooms          []*Room
	ServerSeed     string
	ServerSeedHash string
	Status         string // "waiting", "running", "completed", "paid"
	WonRooms       int
	PayoutAmount   *big.Int
	PayoutTxHash   string
	PayoutError    string
	CreatedAt      time.Time
	CompletedAt    time.Time
	mu             sync.RWMutex
}

func (b *CandleflipBatch) RLock() {
	b.mu.RLock()
}

func (b *CandleflipBatch) RUnlock() {
	b.mu.RUnlock()
}

// CreateBatchMessage - Client creates a new batch
type CreateBatchMessage struct {
	Type          string `json:"type"` // "create_batch"
	Address       string `json:"address"`
	RoomCount     int    `json:"roomCount"`
	AmountPerRoom string `json:"amountPerRoom"` // wei
	Side          string `json:"side"` // "bull" or "bear"
}

var (
	// Active batches by batchID
	candleflipBatches      = make(map[string]*CandleflipBatch)
	candleflipBatchesMutex sync.RWMutex

	// Connected clients
	candleflipClients      = make(map[*websocket.Conn]bool)
	candleflipClientsMutex sync.RWMutex
)

// GetBatch retrieves a batch by ID (thread-safe)
func GetBatch(batchID string) *CandleflipBatch {
	candleflipBatchesMutex.RLock()
	defer candleflipBatchesMutex.RUnlock()
	return candleflipBatches[batchID]
}

// GetAllBatches returns all active batches (for HTTP endpoint)
func GetAllBatches() []*CandleflipBatch {
	candleflipBatchesMutex.RLock()
	defer candleflipBatchesMutex.RUnlock()
	
	batches := make([]*CandleflipBatch, 0, len(candleflipBatches))
	for _, batch := range candleflipBatches {
		batches = append(batches, batch)
	}
	return batches
}

// Broadcast to all connected clients
func broadcastToAllCandleflipClients(message map[string]interface{}) {
	candleflipClientsMutex.RLock()
	defer candleflipClientsMutex.RUnlock()

	for conn := range candleflipClients {
		if err := conn.WriteJSON(message); err != nil {
			log.Printf("❌ Failed to broadcast to candleflip client: %v", err)
		}
	}
}

// HandleCandleflipWS - WebSocket endpoint
func HandleCandleflipWS(w http.ResponseWriter, r *http.Request) {
	log.Printf("🔥 CandleFlip WebSocket connection from: %s", r.RemoteAddr)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("❌ WebSocket upgrade failed:", err)
		return
	}

	// Register client
	candleflipClientsMutex.Lock()
	candleflipClients[conn] = true
	clientCount := len(candleflipClients)
	candleflipClientsMutex.Unlock()

	log.Printf("✅ CandleFlip client connected (Total: %d)", clientCount)

	// Send welcome message
	conn.WriteJSON(map[string]interface{}{
		"type":    "connected",
		"message": "Connected to CandleFlip server",
	})

	// Listen for messages
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("❌ WebSocket error: %v", err)
			}
			break
		}

		handleCandleflipMessage(conn, message)
	}

	// Cleanup on disconnect
	candleflipClientsMutex.Lock()
	delete(candleflipClients, conn)
	candleflipClientsMutex.Unlock()
	
	conn.Close()
	log.Printf("👋 CandleFlip client disconnected")
}

// Handle incoming messages
func handleCandleflipMessage(conn *websocket.Conn, message []byte) {
	var msg CreateBatchMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		log.Printf("❌ Failed to parse candleflip message: %v", err)
		conn.WriteJSON(map[string]interface{}{
			"type":  "error",
			"error": "Invalid message format",
		})
		return
	}

	if msg.Type != "create_batch" {
		log.Printf("⚠️ Unknown message type: %s", msg.Type)
		return
	}

	// Validate inputs
	if msg.Address == "" {
		conn.WriteJSON(map[string]interface{}{
			"type":  "error",
			"error": "Address is required",
		})
		return
	}

	if msg.RoomCount < 1 || msg.RoomCount > 100 {
		conn.WriteJSON(map[string]interface{}{
			"type":  "error",
			"error": "Room count must be between 1 and 100",
		})
		return
	}

	if msg.AmountPerRoom == "" {
		conn.WriteJSON(map[string]interface{}{
			"type":  "error",
			"error": "Amount per room is required",
		})
		return
	}

	if msg.Side != "bull" && msg.Side != "bear" {
		conn.WriteJSON(map[string]interface{}{
			"type":  "error",
			"error": "Side must be 'bull' or 'bear'",
		})
		return
	}

	// Parse amount
	playerAddr := common.HexToAddress(msg.Address)
	amountWei, ok := new(big.Int).SetString(msg.AmountPerRoom, 10)
	if !ok {
		conn.WriteJSON(map[string]interface{}{
			"type":  "error",
			"error": "Invalid amount format",
		})
		return
	}

	// Create batch
	batchID := fmt.Sprintf("batch-%s-%d", playerAddr.Hex()[:8], time.Now().UnixNano())
	serverSeed, seedHash := crypto.GenerateServerSeed()

	batch := &CandleflipBatch{
		BatchID:        batchID,
		PlayerAddress:  playerAddr,
		AmountPerRoom:  amountWei,
		TotalRooms:     msg.RoomCount,
		PlayerSide:     msg.Side,
		Rooms:          make([]*Room, msg.RoomCount),
		ServerSeed:     serverSeed,
		ServerSeedHash: seedHash,
		Status:         "waiting",
		CreatedAt:      time.Now(),
	}

	// Initialize rooms
	for i := 0; i < msg.RoomCount; i++ {
		batch.Rooms[i] = &Room{
			RoomNumber: i + 1,
			Status:     "waiting",
		}
	}

	// Store batch
	candleflipBatchesMutex.Lock()
	if _, exists := candleflipBatches[batchID]; exists {
			candleflipBatchesMutex.Unlock()
			conn.WriteJSON(map[string]interface{}{
					"type":  "error",
					"error": "Batch ID collision, retry",
			})
			return
	}
	candleflipBatches[batchID] = batch
	candleflipBatchesMutex.Unlock()

	log.Printf("🎮 CandleFlip batch created - Batch: %s, Player: %s, Rooms: %d, Amount: %s, Side: %s",
		batchID, msg.Address, msg.RoomCount, msg.AmountPerRoom, msg.Side)

	// Send batch_created response to requester
	conn.WriteJSON(map[string]interface{}{
		"type":    "batch_created",
		"batchId": batchID,
	})

	// Broadcast batch start to all clients
	broadcastToAllCandleflipClients(map[string]interface{}{
		"type": "batch_start",
		"data": map[string]interface{}{
			"batchId":        batchID,
			"playerAddress":  playerAddr.Hex(),
			"totalRooms":     msg.RoomCount,
			"amountPerRoom":  msg.AmountPerRoom,
			"playerSide":     msg.Side,
			"aiSide":         getOppositeSide(msg.Side),
			"serverSeedHash": seedHash,
		},
	})

	// Start game in background
	go runCandleflipBatch(batch)
}

// Run the batch game
func runCandleflipBatch(batch *CandleflipBatch) {
	batch.mu.Lock()
	batch.Status = "running"
	batch.mu.Unlock()

	wonRooms := 0

	// Run each room
	for i := 0; i < batch.TotalRooms; i++ {
		room := batch.Rooms[i]
		
		room.Status = "running"
		room.StartTime = time.Now()

		// Broadcast room start
		broadcastToAllCandleflipClients(map[string]interface{}{
			"type": "room_start",
			"data": map[string]interface{}{
				"batchId":    batch.BatchID,
				"roomNumber": room.RoomNumber,
			},
		})

		// Generate price movement for this room
		roomSeed := fmt.Sprintf("%s-room-%d", batch.ServerSeed, i)
		rng := game.NewSeededRNG(roomSeed)

		currentPrice := game.CandleflipStartingPrice
		priceHistory := []float64{currentPrice}

		// Simulate ticks
		for tick := 0; tick < game.CandleflipTotalTicks; tick++ {
			currentPrice = game.GenerateCandleflipPrice(rng, currentPrice)
			if currentPrice < 0 {
				currentPrice = 0
			}
			priceHistory = append(priceHistory, currentPrice)

			// Broadcast price update
			broadcastToAllCandleflipClients(map[string]interface{}{
				"type": "price_update",
				"data": map[string]interface{}{
					"batchId":    batch.BatchID,
					"roomNumber": room.RoomNumber,
					"tick":       tick + 1,
					"price":      game.RoundToDecimal(currentPrice, 3),
					"totalTicks": game.CandleflipTotalTicks,
				},
			})

			time.Sleep(100 * time.Millisecond)
		}

		// Determine winner
		finalPrice := currentPrice
		var winner string
		if finalPrice >= game.CandleflipStartingPrice {
			winner = "bull"
		} else {
			winner = "bear"
		}

		playerWon := (winner == batch.PlayerSide)
		if playerWon {
			wonRooms++
		}

		// Update room
		room.Status = "completed"
		room.FinalPrice = finalPrice
		room.Winner = winner
		room.PlayerWon = playerWon
		room.EndTime = time.Now()

		// Broadcast room end
		broadcastToAllCandleflipClients(map[string]interface{}{
			"type": "room_end",
			"data": map[string]interface{}{
				"batchId":    batch.BatchID,
				"roomNumber": room.RoomNumber,
				"finalPrice": game.RoundToDecimal(finalPrice, 3),
				"winner":     winner,
				"playerWon":  playerWon,
			},
		})

		log.Printf("🎲 Room %d/%d - Final: %.3f, Winner: %s, Player Won: %v",
			i+1, batch.TotalRooms, finalPrice, winner, playerWon)

		time.Sleep(500 * time.Millisecond)
	}

	// Update batch
	batch.mu.Lock()
	batch.Status = "completed"
	batch.WonRooms = wonRooms
	batch.CompletedAt = time.Now()
	batch.mu.Unlock()

	// Broadcast batch end
	broadcastToAllCandleflipClients(map[string]interface{}{
		"type": "batch_end",
		"data": map[string]interface{}{
			"batchId":    batch.BatchID,
			"totalRooms": batch.TotalRooms,
			"wonRooms":   wonRooms,
			"serverSeed": batch.ServerSeed,
		},
	})

	log.Printf("🎯 CandleFlip batch complete - Player won %d/%d rooms", wonRooms, batch.TotalRooms)

	// Attempt payout (non-blocking)
	payoutCandleflipWinnings(batch)

	// Wait exactly 5 seconds after payout attempt finishes
	time.Sleep(5 * time.Second)

	// Delete batch
	candleflipBatchesMutex.Lock()
	delete(candleflipBatches, batch.BatchID)
	candleflipBatchesMutex.Unlock()

	log.Printf("🗑️ Removed batch %s from memory", batch.BatchID)
}

// Payout winnings
func payoutCandleflipWinnings(batch *CandleflipBatch) {
	if batch.WonRooms == 0 {
		log.Printf("❌ Player won 0 rooms, no payout for %s", batch.PlayerAddress.Hex())
		
		batch.mu.Lock()
		batch.Status = "paid"
		batch.PayoutAmount = big.NewInt(0)
		batch.mu.Unlock()
		
		return
	}

	// Calculate payout: wonRooms * amountPerRoom * 2
	wonRoomsBig := big.NewInt(int64(batch.WonRooms))
	multiplier := big.NewInt(2)

	payout := new(big.Int).Mul(batch.AmountPerRoom, wonRoomsBig)
	payout.Mul(payout, multiplier)

	batch.mu.Lock()
	batch.PayoutAmount = payout
	batch.mu.Unlock()

	log.Printf("💰 Calculating payout: %d rooms × %s wei/room × 2 = %s wei",
		batch.WonRooms, batch.AmountPerRoom.String(), payout.String())

	// Initialize contract
	contractClient, err := contract.NewGameHouseContract()
	if err != nil {
		log.Printf("❌ Failed to initialize contract: %v", err)
		
		batch.mu.Lock()
		batch.Status = "paid"
		batch.PayoutError = err.Error()
		batch.mu.Unlock()
		
		broadcastToAllCandleflipClients(map[string]interface{}{
			"type": "payout_failed",
			"data": map[string]interface{}{
				"batchId": batch.BatchID,
				"error":   err.Error(),
			},
		})
		return
	}
	defer contractClient.Close()

	// Call payPlayer
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	err = contractClient.PayPlayer(ctx, batch.PlayerAddress, payout)
	if err != nil {
		log.Printf("No balance in contract to pay players")
		
		batch.mu.Lock()
		batch.Status = "paid"
		batch.PayoutError = err.Error()
		batch.mu.Unlock()
		
		broadcastToAllCandleflipClients(map[string]interface{}{
			"type": "payout_failed",
			"data": map[string]interface{}{
				"batchId": batch.BatchID,
				"error":   "No balance in contract to pay players",
			},
		})
		return
	}

	// Success
	batch.mu.Lock()
	batch.Status = "paid"
	batch.mu.Unlock()

	payoutMNT := config.WeiToMNT(payout)
	log.Printf("✅ Paid %s: %.4f MNT", batch.PlayerAddress.Hex(), payoutMNT)
}

// Helper functions
func getOppositeSide(side string) string {
	if side == "bull" {
		return "bear"
	}
	return "bull"
}