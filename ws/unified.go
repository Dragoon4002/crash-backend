package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"goLangServer/contract"
	"goLangServer/db"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/websocket"
)

// ClientConnection represents a connected client with their subscriptions
type ClientConnection struct {
	ID            string
	Conn          *websocket.Conn
	Subscriptions map[string]bool // crash, chat, rooms, candleflip:<roomId>
	mu            sync.RWMutex
	writeMutex    sync.Mutex // Protects websocket writes
	Send          chan []byte
}

// writeJSON safely writes JSON to the websocket with mutex protection
func (c *ClientConnection) writeJSON(v interface{}) error {
	c.writeMutex.Lock()
	defer c.writeMutex.Unlock()
	return c.Conn.WriteJSON(v)
}

var (
	// All connected clients
	clients      = make(map[*ClientConnection]bool)
	clientsMutex sync.RWMutex

	// Channels for different event types
	crashBroadcast   = make(chan interface{}, 100)
	chatBroadcastCh  = make(chan interface{}, 100)
	roomsBroadcast   = make(chan interface{}, 100)
	clientRegister   = make(chan *ClientConnection)
	clientUnregister = make(chan *ClientConnection)

	// Client ID counter
	clientIDCounter int64

	// Chat ring buffer (FIFO, max 100 messages)
	chatHistory      []interface{}
	chatHistoryMutex sync.RWMutex
	maxChatHistory   = 100

	// Contract client for payPlayer calls
	contractClient      *contract.GameHouseContract
	contractClientMutex sync.RWMutex
)

// SetContractClient sets the global contract client instance
func SetContractClient(client *contract.GameHouseContract) {
	contractClientMutex.Lock()
	defer contractClientMutex.Unlock()
	contractClient = client
	log.Println("✅ Contract client set for crash payouts")
}

// Message types from client
type ClientMessage struct {
	Type string                 `json:"type"`
	Data map[string]interface{} `json:"data,omitempty"`
}

func init() {
	// Load chat history from database
	go loadChatHistoryFromDB()

	// Start the unified event hub
	go runEventHub()

	// Start periodic room broadcaster (for constant updates)
	go runPeriodicRoomBroadcaster()
}

// loadChatHistoryFromDB loads recent chat messages from PostgreSQL on startup
func loadChatHistoryFromDB() {
	// Wait a bit for DB to initialize
	time.Sleep(2 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	messages, err := db.GetRecentChatMessages(ctx, maxChatHistory)
	if err != nil {
		log.Printf("⚠️  Failed to load chat history from DB: %v", err)
		return
	}

	if len(messages) == 0 {
		log.Println("📨 No chat history found in database")
		return
	}

	// Convert to interface{} slice for chatHistory
	chatHistoryMutex.Lock()
	for _, msg := range messages {
		chatHistory = append(chatHistory, map[string]interface{}{
			"type":          "chat_message",
			"playerAddress": msg.PlayerAddress,
			"message":       msg.Message,
			"timestamp":     msg.Timestamp.Format(time.RFC3339),
		})
	}
	chatHistoryMutex.Unlock()

	log.Printf("✅ Loaded %d chat messages from database", len(messages))
}

// runEventHub is the central message dispatcher
func runEventHub() {
	log.Println("🚀 Unified Event Hub started")

	for {
		select {
		case client := <-clientRegister:
			clientsMutex.Lock()
			clients[client] = true
			clientsMutex.Unlock()
			log.Printf("✅ Client registered: %s (Total: %d)", client.ID, len(clients))

		case client := <-clientUnregister:
			clientsMutex.Lock()
			if _, ok := clients[client]; ok {
				delete(clients, client)
				close(client.Send)
			}
			clientsMutex.Unlock()
			log.Printf("👋 Client unregistered: %s (Total: %d)", client.ID, len(clients))

		case message := <-crashBroadcast:
			broadcastToSubscribers("crash", message)

		case message := <-chatBroadcastCh:
			// Add to chat history ring buffer
			chatHistoryMutex.Lock()
			chatHistory = append(chatHistory, message)
			if len(chatHistory) > maxChatHistory {
				// Remove oldest message (FIFO)
				chatHistory = chatHistory[1:]
			}
			chatHistoryMutex.Unlock()

			// Broadcast to all chat subscribers
			broadcastToSubscribers("chat", message)

		case message := <-roomsBroadcast:
			broadcastToSubscribers("rooms", message)
		}
	}
}

// runPeriodicRoomBroadcaster broadcasts room updates every 200ms
// Tested to handle up to 10 rooms in parallel batches efficiently
func runPeriodicRoomBroadcaster() {
	log.Println("📡 Periodic room broadcaster started (200ms interval, max 10 rooms per batch)")
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		// Get current rooms
		globalRoomsMutex.RLock()
		rooms := make([]*RoomInfo, 0, len(globalRooms))
		for _, room := range globalRooms {
			rooms = append(rooms, room)
		}
		globalRoomsMutex.RUnlock()

		// Only broadcast if there are rooms to show
		if len(rooms) > 0 {
			message := map[string]interface{}{
				"type":  "rooms_update",
				"rooms": rooms,
			}

			// Send to unified broadcast channel
			select {
			case roomsBroadcast <- message:
				// Successfully sent
			default:
				// Channel full, skip this broadcast
			}
		}
	}
}

// broadcastToSubscribers sends message to all clients subscribed to a channel
func broadcastToSubscribers(channel string, message interface{}) {
	data, err := json.Marshal(message)
	if err != nil {
		log.Printf("❌ Failed to marshal message for %s: %v", channel, err)
		return
	}

	clientsMutex.RLock()
	defer clientsMutex.RUnlock()

	for client := range clients {
		client.mu.RLock()
		subscribed := client.Subscriptions[channel]
		client.mu.RUnlock()

		if subscribed {
			select {
			case client.Send <- data:
			default:
				// Client's send channel is full, skip
				log.Printf("⚠️  Client %s send buffer full, skipping message", client.ID)
			}
		}
	}
}

// HandleUnifiedWS is the single WebSocket endpoint
func HandleUnifiedWS(w http.ResponseWriter, r *http.Request) {
	log.Println("📥 Unified WebSocket connection from:", r.RemoteAddr)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("❌ WebSocket upgrade failed:", err)
		return
	}

	// Create client
	client := &ClientConnection{
		ID:            generateClientID(),
		Conn:          conn,
		Subscriptions: make(map[string]bool),
		Send:          make(chan []byte, 256),
	}

	// Register client
	clientRegister <- client

	// Start goroutines for this client
	go client.writePump()
	go client.readPump()
}

// writePump sends messages from the Send channel to the WebSocket
func (c *ClientConnection) writePump() {
	defer func() {
		c.Conn.Close()
	}()

	for message := range c.Send {
		c.writeMutex.Lock()
		err := c.Conn.WriteMessage(websocket.TextMessage, message)
		c.writeMutex.Unlock()

		if err != nil {
			log.Printf("❌ Write error for client %s: %v", c.ID, err)
			return
		}
	}
}

// readPump reads messages from the WebSocket and handles subscriptions/requests
func (c *ClientConnection) readPump() {
	defer func() {
		clientUnregister <- c
		c.Conn.Close()
	}()

	for {
		_, messageBytes, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("❌ Read error for client %s: %v", c.ID, err)
			}
			break
		}

		var msg ClientMessage
		if err := json.Unmarshal(messageBytes, &msg); err != nil {
			log.Printf("❌ Failed to parse message from client %s: %v", c.ID, err)
			continue
		}

		c.handleMessage(msg)
	}
}

// handleMessage processes incoming client messages
func (c *ClientConnection) handleMessage(msg ClientMessage) {
	switch msg.Type {
	case "subscribe":
		channel := msg.Data["channel"].(string)
		c.mu.Lock()
		c.Subscriptions[channel] = true
		c.mu.Unlock()
		log.Printf("📡 Client %s subscribed to: %s", c.ID, channel)

		// Send initial data for the channel
		c.sendInitialData(channel)

	case "unsubscribe":
		channel := msg.Data["channel"].(string)
		c.mu.Lock()
		delete(c.Subscriptions, channel)
		c.mu.Unlock()
		log.Printf("📴 Client %s unsubscribed from: %s", c.ID, channel)

	case "create_room":
		handleCreateRoom(msg.Data)

	case "chat_message":
		handleChatMessage(c, msg.Data)

	case "crash_bet_placed":
		handleCrashBetPlaced(c, msg.Data)

	case "crash_cashout":
		handleCrashCashout(c, msg.Data)

	case "join_candleflip_room":
		roomID := msg.Data["roomId"].(string)
		handleJoinCandleflipRoom(c, roomID)

	case "create_batch":
		// Forward to candleflip batch creation (fallback handler)
		address, _ := msg.Data["address"].(string)
		roomCountFloat, _ := msg.Data["roomCount"].(float64)
		roomCount := int(roomCountFloat)
		amountPerRoom, _ := msg.Data["amountPerRoom"].(string)
		side, _ := msg.Data["side"].(string)

		batchID, err := CreateBatchFromData(address, roomCount, amountPerRoom, side)
		if err != nil {
			c.writeJSON(map[string]interface{}{
				"type":  "error",
				"error": err.Error(),
			})
			return
		}
		c.writeJSON(map[string]interface{}{
			"type":    "batch_created",
			"batchId": batchID,
		})

	default:
		log.Printf("⚠️  Unknown message type from client %s: %s", c.ID, msg.Type)
	}
}

// sendInitialData sends initial state when client subscribes to a channel
// For crash and rooms, periodic broadcasts handle syncing (no initial send needed)
// For chat, send history immediately since it's a one-time operation
func (c *ClientConnection) sendInitialData(channel string) {
	switch channel {
	case "crash":
		// Send crash history immediately
		history := GetCrashHistory()
		historyMsg := map[string]interface{}{
			"type":    "crash_history",
			"history": history,
		}
		if err := c.writeJSON(historyMsg); err != nil {
			log.Printf("⚠️  Failed to send crash history to client %s: %v", c.ID, err)
		} else {
			log.Printf("📨 Client %s subscribed to crash - sent %d history items", c.ID, len(history))
		}

		// Send active bettors immediately
		bettors := GetActiveBettors()
		bettorsMsg := map[string]interface{}{
			"type":    "active_bettors",
			"bettors": bettors,
			"count":   len(bettors),
		}
		if err := c.writeJSON(bettorsMsg); err != nil {
			log.Printf("⚠️  Failed to send active bettors to client %s: %v", c.ID, err)
		} else {
			log.Printf("📨 Client %s - sent %d active bettors", c.ID, len(bettors))
		}

	case "rooms":
		// No initial sync needed - periodic broadcasts every 200ms
		log.Printf("📨 Client %s subscribed to rooms (will receive next broadcast within 200ms)", c.ID)

	case "chat":
		// Send chat history from in-memory buffer
		chatHistoryMutex.RLock()
		history := make([]interface{}, len(chatHistory))
		copy(history, chatHistory)
		chatHistoryMutex.RUnlock()

		// Send each message
		for _, msg := range history {
			data, _ := json.Marshal(msg)
			c.Send <- data
		}

		log.Printf("📨 Client %s joined chat (sent %d history messages)", c.ID, len(history))
	}
}

// Helper functions
func handleCreateRoom(data map[string]interface{}) {
	roomID := data["roomId"].(string)
	gameType := data["gameType"].(string)
	betAmount := data["betAmount"].(float64)
	creatorId := ""
	if id, ok := data["creatorId"].(string); ok {
		creatorId = id
	}
	trend := ""
	if t, ok := data["trend"].(string); ok {
		trend = t
	}
	botNameSeed := ""
	if seed, ok := data["botNameSeed"].(string); ok {
		botNameSeed = seed
	}
	contractGameId := ""
	if gameId, ok := data["contractGameId"].(string); ok {
		contractGameId = gameId
	}
	roomsCount := 0
	if count, ok := data["roomsCount"].(float64); ok {
		roomsCount = int(count)
	}

	CreateRoom(roomID, gameType, betAmount, trend)

	// For candleflip, assign player vs bot and start game
	if gameType == "candleflip" && creatorId != "" {
		globalRoomsMutex.Lock()
		if globalRoom, exists := globalRooms[roomID]; exists {
			globalRoom.CreatorId = creatorId
			globalRoom.Players = 1
			globalRoom.ContractGameID = contractGameId
			globalRoom.RoomsCount = roomsCount

			// Get consistent bot name for all rooms in this batch
			globalRoom.BotName = GetBotName(botNameSeed)

			// Assign player to their chosen side, bot gets opposite
			if trend == "bullish" {
				globalRoom.BullSide = "player"
				globalRoom.BearSide = "bot"
			} else if trend == "bearish" {
				globalRoom.BearSide = "player"
				globalRoom.BullSide = "bot"
			}

			// Mark room as ready to start
			globalRoom.Status = "active"
		}
		globalRoomsMutex.Unlock()
		BroadcastRoomUpdate()
		log.Printf("🎮 Candleflip room %s created by %s vs Bot '%s' (player side: %s, contractGameId: %s)",
			roomID, creatorId, GetBotName(botNameSeed), trend, contractGameId)

		// Game will auto-start when client connects to /candleflip WebSocket
		// No need to start here - prevents race condition with multiple rooms
	}
}

func handleChatMessage(client *ClientConnection, data map[string]interface{}) {
	message := data["message"].(string)

	// Player address (use client ID as fallback if no address provided)
	playerAddress := client.ID
	if addr, ok := data["playerAddress"].(string); ok && addr != "" {
		playerAddress = addr
	}

	now := time.Now()
	chatMsg := map[string]interface{}{
		"type":          "chat_message",
		"playerAddress": playerAddress,
		"message":       message,
		"timestamp":     now.Format(time.RFC3339),
	}

	// Store in PostgreSQL (asynchronous, don't block on errors)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := db.StoreChatMessage(ctx, &db.ChatHistoryRecord{
			PlayerAddress: playerAddress,
			Message:       message,
			Timestamp:     now,
		})
		if err != nil {
			log.Printf("⚠️  Failed to store chat message in PostgreSQL: %v", err)
		}
	}()

	// Broadcast to all chat subscribers
	chatBroadcastCh <- chatMsg
}

func handleCrashBetPlaced(client *ClientConnection, data map[string]interface{}) {
	playerAddress := data["playerAddress"].(string)
	userId := data["userId"].(string)
	gameId := data["gameId"].(string)
	betAmount := data["betAmount"].(float64)
	entryMultiplier := data["entryMultiplier"].(float64)
	transactionHash := data["transactionHash"].(string)

	log.Println("🎯 handleCrashBetPlaced called - Processing bet placement")
	log.Printf("🎲 Crash bet placed - Player: %s, Amount: %.4f, Entry: %.2fx, GameID: %s, TxHash: %s",
		playerAddress, betAmount, entryMultiplier, gameId, transactionHash)

	// Store in database
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := db.StoreCrashBetPostgres(ctx, &db.CrashBetRecord{
			GameID:            gameId,
			PlayerAddress:     playerAddress,
			UserID:            userId,
			BetAmount:         betAmount,
			EntryMultiplier:   entryMultiplier,
			TransactionHash:   transactionHash,
			Status:            "active",
			CreatedAt:         time.Now(),
		})
		if err != nil {
			log.Printf("⚠️  Failed to store crash bet in PostgreSQL: %v", err)
		}

		// Subtract bet amount from wallet PnL
		if err := db.SubtractWalletPnL(ctx, playerAddress, betAmount); err != nil {
			log.Printf("⚠️  Failed to update wallet PnL: %v", err)
		}
	}()

	// Add to active bettors list
	AddActiveBettor(playerAddress, betAmount, entryMultiplier)
}

func handleCrashCashout(client *ClientConnection, data map[string]interface{}) {
	log.Println("🎯 handleCrashCashout called - Data:", data)

	playerAddress := data["playerAddress"].(string)
	userId := data["userId"].(string)
	gameId := data["gameId"].(string)
	cashoutMultiplier := data["cashoutMultiplier"].(float64)
	betAmount := data["betAmount"].(float64)
	entryMultiplier := data["entryMultiplier"].(float64)

	log.Printf("💰 Crash cashout request - Player: %s, GameID: %s, UserID: %s, Cashout: %.2fx, BetAmount: %.4f, Entry: %.2fx",
		playerAddress, gameId, userId, cashoutMultiplier, betAmount, entryMultiplier)

	// Calculate payout
	profit := betAmount * (cashoutMultiplier - 1)
	payoutAmount := betAmount + profit

	log.Printf("💸 Payout calculated - Player: %s, Payout: %.4f MNT (Profit: %.4f)",
		playerAddress, payoutAmount, profit)

	// Call payPlayer contract function
	var payoutTxHash string
	contractClientMutex.RLock()
	hasContract := contractClient != nil
	contractClientMutex.RUnlock()

	log.Printf("💳 Contract client available: %v", hasContract)

	if hasContract {
		// Convert payout amount to wei (18 decimals)
		// payoutAmount is in MNT (ether), convert to wei
		payoutWei := new(big.Float).Mul(big.NewFloat(payoutAmount), big.NewFloat(1e18))
		payoutBigInt := new(big.Int)
		payoutWei.Int(payoutBigInt)

		playerAddr := common.HexToAddress(playerAddress)

		log.Printf("💸 Calling payPlayer contract - Player: %s, PayoutWei: %s", playerAddr.Hex(), payoutBigInt.String())

		// Call contract (async, don't block game flow)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			contractClientMutex.RLock()
			client := contractClient
			contractClientMutex.RUnlock()

			log.Printf("🔄 Executing contract.PayPlayer...")
			err := client.PayPlayer(ctx, playerAddr, payoutBigInt)
			if err != nil {
				log.Printf("❌ Failed to call payPlayer contract: %v", err)
				// Don't block user experience - they still get credited in DB
			} else {
				log.Printf("✅ payPlayer contract call successful for %s", playerAddress)
			}
		}()
	} else {
		log.Printf("⚠️  Contract client not available - skipping on-chain payout")
	}

	// Update database (with empty tx hash for now, contract call is async)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := db.UpdateCrashBetCashout(ctx, gameId, playerAddress, cashoutMultiplier, payoutAmount, payoutTxHash)
		if err != nil {
			log.Printf("⚠️  Failed to update crash bet: %v", err)
		}

		// Add payout amount to wallet PnL
		if err := db.AddWalletPnL(ctx, playerAddress, payoutAmount); err != nil {
			log.Printf("⚠️  Failed to update wallet PnL: %v", err)
		}
	}()

	// Remove from active bettors list
	RemoveActiveBettor(playerAddress)

	// Send private message to user with profit/loss
	sendPrivateMessage(client, userId, map[string]interface{}{
		"type":              "crash_cashout_result",
		"success":           true,
		"cashoutMultiplier": cashoutMultiplier,
		"entryMultiplier":   entryMultiplier,
		"betAmount":         betAmount,
		"payoutAmount":      payoutAmount,
		"profit":            profit,
		"message":           fmt.Sprintf("Cashed out at %.2fx! Profit: %.4f MNT", cashoutMultiplier, profit),
	})
}

func sendPrivateMessage(client *ClientConnection, userId string, message map[string]interface{}) {
	// Find client by userId and send message only to them
	clientsMutex.RLock()
	defer clientsMutex.RUnlock()

	for c := range clients {
		if c.ID == userId {
			data, err := json.Marshal(message)
			if err != nil {
				log.Printf("⚠️  Failed to marshal private message: %v", err)
				return
			}
			c.Send <- data
			log.Printf("✉️  Sent private message to user %s", userId)
			return
		}
	}

	log.Printf("⚠️  User %s not found for private message", userId)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func handleJoinCandleflipRoom(client *ClientConnection, roomID string) {
	// Subscribe client to specific candleflip room updates (for spectating)
	channel := "candleflip:" + roomID
	client.mu.Lock()
	client.Subscriptions[channel] = true
	client.mu.Unlock()

	log.Printf("🎮 Client %s subscribed to Candleflip room: %s (spectator/player)", client.ID, roomID)
}

// generateClientID creates a unique client ID
func generateClientID() string {
	id := atomic.AddInt64(&clientIDCounter, 1)
	return fmt.Sprintf("%d-%d", time.Now().Unix(), id)
}

// getCrashGameHistory returns copy of crash game history
func getCrashGameHistory() []CrashGameHistory {
	gameHistoryMutex.RLock()
	defer gameHistoryMutex.RUnlock()

	history := make([]CrashGameHistory, len(crashGameHistory))
	copy(history, crashGameHistory)
	return history
}
