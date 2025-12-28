package ws

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// ClientConnection represents a connected client with their subscriptions
type ClientConnection struct {
	ID            string
	Conn          *websocket.Conn
	Subscriptions map[string]bool // crash, chat, rooms, candleflip:<roomId>
	mu            sync.RWMutex
	Send          chan []byte
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
)

// Message types from client
type ClientMessage struct {
	Type string                 `json:"type"`
	Data map[string]interface{} `json:"data,omitempty"`
}

func init() {
	// Start the unified event hub
	go runEventHub()
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
		if err := c.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
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

	case "join_candleflip_room":
		roomID := msg.Data["roomId"].(string)
		handleJoinCandleflipRoom(c, roomID)

	default:
		log.Printf("⚠️  Unknown message type from client %s: %s", c.ID, msg.Type)
	}
}

// sendInitialData sends current state when client subscribes to a channel
func (c *ClientConnection) sendInitialData(channel string) {
	switch channel {
	case "crash":
		// Send crash game history
		history := getCrashGameHistory()

		data, _ := json.Marshal(map[string]interface{}{
			"type":    "crash_history",
			"history": history,
		})
		c.Send <- data

		// Send current active bettors
		bettors := GetActiveBettors()
		bettorData, _ := json.Marshal(map[string]interface{}{
			"type":    "active_bettors",
			"bettors": bettors,
			"count":   len(bettors),
		})
		c.Send <- bettorData

	case "rooms":
		// Send current room list
		globalRoomsMutex.RLock()
		rooms := make([]*RoomInfo, 0, len(globalRooms))
		for _, room := range globalRooms {
			rooms = append(rooms, room)
		}
		globalRoomsMutex.RUnlock()

		data, _ := json.Marshal(map[string]interface{}{
			"type":  "rooms_update",
			"rooms": rooms,
		})
		c.Send <- data

	case "chat":
		// Send chat history to new client
		chatHistoryMutex.RLock()
		history := make([]interface{}, len(chatHistory))
		copy(history, chatHistory)
		chatHistoryMutex.RUnlock()

		// Send each message individually to maintain order
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

		// Start the game AFTER room is fully configured
		// Use a small delay to ensure clients can connect before game starts
		go func() {
			time.Sleep(500 * time.Millisecond) // Give clients time to connect
			StartCandleflipGame(roomID)
		}()
	}
}

func handleChatMessage(client *ClientConnection, data map[string]interface{}) {
	message := data["message"].(string)

	chatMsg := map[string]interface{}{
		"type":      "chat_message",
		"username":  "User-" + client.ID[len(client.ID)-min(8, len(client.ID)):],
		"message":   message,
		"userId":    client.ID,
		"timestamp": time.Now().Format(time.RFC3339),
	}

	chatBroadcastCh <- chatMsg
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
