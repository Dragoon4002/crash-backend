package ws

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type ChatClient struct {
	ID       string
	Conn     *websocket.Conn
	Username string
	Send     chan []byte
}

type ChatMessage struct {
	Type      string    `json:"type"`
	Username  string    `json:"username"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	UserId    string    `json:"userId"`
}

var (
	chatClients   = make(map[*ChatClient]bool)
	chatBroadcast = make(chan ChatMessage)
	chatMutex     sync.Mutex
)

func init() {
	go handleChatMessages()
}

func handleChatMessages() {
	for {
		msg := <-chatBroadcast

		chatMutex.Lock()
		for client := range chatClients {
			err := client.Conn.WriteJSON(msg)
			if err != nil {
				log.Printf("❌ Error sending chat message to client %s: %v", client.ID, err)
				client.Conn.Close()
				delete(chatClients, client)
			}
		}
		chatMutex.Unlock()
	}
}

func HandleChatWS(w http.ResponseWriter, r *http.Request) {
	log.Println("💬 Chat WebSocket connection attempt from:", r.RemoteAddr)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("❌ Chat WebSocket upgrade failed:", err)
		return
	}

	// Create new client
	clientID := time.Now().Format("20060102-150405.000")
	client := &ChatClient{
		ID:       clientID,
		Conn:     conn,
		Username: "User-" + clientID[len(clientID)-8:],
		Send:     make(chan []byte, 256),
	}

	// Register client
	chatMutex.Lock()
	chatClients[client] = true
	chatMutex.Unlock()

	log.Printf("✅ Chat client connected! ID: %s, Total chat clients: %d", client.ID, len(chatClients))

	// Send welcome message
	welcomeMsg := ChatMessage{
		Type:      "system",
		Username:  "System",
		Message:   client.Username + " joined the chat",
		Timestamp: time.Now(),
		UserId:    "system",
	}
	chatBroadcast <- welcomeMsg

	// Cleanup on disconnect
	defer func() {
		chatMutex.Lock()
		delete(chatClients, client)
		chatMutex.Unlock()

		leaveMsg := ChatMessage{
			Type:      "system",
			Username:  "System",
			Message:   client.Username + " left the chat",
			Timestamp: time.Now(),
			UserId:    "system",
		}
		chatBroadcast <- leaveMsg

		conn.Close()
		log.Printf("👋 Chat client disconnected. ID: %s, Total chat clients: %d", client.ID, len(chatClients))
	}()

	// Listen for messages
	for {
		var msg ChatMessage
		err := conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("❌ Chat WebSocket error: %v", err)
			}
			break
		}

		// Add metadata
		msg.Timestamp = time.Now()
		msg.Username = client.Username
		msg.UserId = client.ID
		msg.Type = "message"

		log.Printf("💬 Chat message from %s: %s", client.Username, msg.Message)

		// Broadcast to all clients
		chatBroadcast <- msg
	}
}
