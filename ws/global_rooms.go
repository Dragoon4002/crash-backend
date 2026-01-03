package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// RoomInfo represents a game room visible to all clients
type RoomInfo struct {
	RoomID         string    `json:"roomId"`
	GameType       string    `json:"gameType"` // "crash" or "candleflip"
	BetAmount      float64   `json:"betAmount"`
	Trend          string    `json:"trend,omitempty"`      // For candleflip: "bullish" or "bearish" (player's choice)
	Status         string    `json:"status"`               // "active", "running", "finished"
	CreatedAt      time.Time `json:"createdAt"`
	Players        int       `json:"players"`
	CreatorId      string    `json:"creatorId,omitempty"`  // ID of player who created the room
	BotName        string    `json:"botName,omitempty"`    // Bot opponent name for candleflip
	BearSide       string    `json:"bearSide,omitempty"`   // "player" or "bot" - who is on bearish side
	BullSide       string    `json:"bullSide,omitempty"`   // "player" or "bot" - who is on bullish side
	MaxPlayers     int       `json:"maxPlayers"`           // 1 for candleflip (player vs bot), unlimited for crash
	ContractGameID string    `json:"contractGameId,omitempty"` // Contract game ID from placeCandleFlip
	RoomsCount     int       `json:"roomsCount,omitempty"` // Number of rooms for CandleFlip
}

var (
	// Global rooms visible to all clients
	globalRooms      = make(map[string]*RoomInfo)
	globalRoomsMutex sync.RWMutex

	// Clients subscribed to room updates
	globalRoomClients      = make(map[*websocket.Conn]bool)
	globalRoomClientsMutex sync.RWMutex

)

// BroadcastRoomUpdate sends room list to all subscribed clients via unified broadcast
func BroadcastRoomUpdate() {
	globalRoomsMutex.RLock()
	rooms := make([]*RoomInfo, 0, len(globalRooms))
	for _, room := range globalRooms {
		rooms = append(rooms, room)
	}
	globalRoomsMutex.RUnlock()

	message := map[string]interface{}{
		"type":  "rooms_update",
		"rooms": rooms,
	}

	// Send to unified broadcast channel instead of direct writes
	roomsBroadcast <- message
}

// GetBotName always returns "AI" as the bot name
func GetBotName(seed string) string {
	return "AI"
}

// CreateRoom creates a new global room
func CreateRoom(roomID, gameType string, betAmount float64, trend string) *RoomInfo {
	maxPlayers := 0 // unlimited for crash
	if gameType == "candleflip" {
		maxPlayers = 1 // player vs bot for candleflip
	}

	room := &RoomInfo{
		RoomID:     roomID,
		GameType:   gameType,
		BetAmount:  betAmount,
		Trend:      trend,
		Status:     "active",
		CreatedAt:  time.Now(),
		Players:    0,
		MaxPlayers: maxPlayers,
	}

	globalRoomsMutex.Lock()
	globalRooms[roomID] = room
	globalRoomsMutex.Unlock()

	log.Printf("🌍 Created global %s room: %s (max players: %d)", gameType, roomID, maxPlayers)
	BroadcastRoomUpdate()

	return room
}

// UpdateRoomStatus updates a room's status
func UpdateRoomStatus(roomID, status string) {
	globalRoomsMutex.Lock()
	if room, exists := globalRooms[roomID]; exists {
		room.Status = status
	}
	globalRoomsMutex.Unlock()

	BroadcastRoomUpdate()
}

// UpdateRoomPlayers updates player count in a room
func UpdateRoomPlayers(roomID string, players int) {
	globalRoomsMutex.Lock()
	if room, exists := globalRooms[roomID]; exists {
		room.Players = players
	}
	globalRoomsMutex.Unlock()

	BroadcastRoomUpdate()
}

// RemoveRoom removes a room from global list
func RemoveRoom(roomID string) {
	globalRoomsMutex.Lock()
	delete(globalRooms, roomID)
	globalRoomsMutex.Unlock()

	log.Printf("🗑️  Removed global room: %s", roomID)
	BroadcastRoomUpdate()
}

// HandleGlobalRoomsWS handles WebSocket connections for global room list
func HandleGlobalRoomsWS(w http.ResponseWriter, r *http.Request) {
	log.Println("🌍 Global rooms WebSocket connection attempt from:", r.RemoteAddr)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("❌ Global rooms WebSocket upgrade failed:", err)
		return
	}
	defer conn.Close()

	// Register client
	globalRoomClientsMutex.Lock()
	globalRoomClients[conn] = true
	globalRoomClientsMutex.Unlock()

	log.Printf("✅ Global rooms client connected. Total: %d", len(globalRoomClients))

	// Send current room list immediately
	globalRoomsMutex.RLock()
	rooms := make([]*RoomInfo, 0, len(globalRooms))
	for _, room := range globalRooms {
		rooms = append(rooms, room)
	}
	globalRoomsMutex.RUnlock()

	if err := conn.WriteJSON(map[string]interface{}{
		"type":  "rooms_update",
		"rooms": rooms,
	}); err != nil {
		log.Printf("❌ Failed to send initial room list: %v", err)
	}

	// Cleanup on disconnect
	defer func() {
		globalRoomClientsMutex.Lock()
		delete(globalRoomClients, conn)
		globalRoomClientsMutex.Unlock()
		log.Printf("👋 Global rooms client disconnected. Total: %d", len(globalRoomClients))
	}()

	// Listen for messages (room creation requests)
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("❌ Global rooms WebSocket error: %v", err)
			}
			break
		}

		var req map[string]interface{}
		if err := json.Unmarshal(message, &req); err != nil {
			log.Printf("❌ Failed to parse room request: %v", err)
			continue
		}

		// Handle room creation request
		if reqType, ok := req["type"].(string); ok && reqType == "create_room" {
			data := req["data"].(map[string]interface{})
			roomID := data["roomId"].(string)
			gameType := data["gameType"].(string)
			betAmount := data["betAmount"].(float64)
			trend := ""
			if t, ok := data["trend"].(string); ok {
				trend = t
			}

			CreateRoom(roomID, gameType, betAmount, trend)
		}
	}
}
