package main

import (
	"log"
	"net/http"

	"goLangServer/ws"
	"github.com/joho/godotenv"
)

func main() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  Warning: .env file not found, using environment variables")
	} else {
		log.Println("✅ Loaded environment variables from .env")
	}

	// Unified WebSocket endpoint
	http.HandleFunc("/ws", ws.HandleUnifiedWS)

	// Legacy endpoints (deprecated, will redirect to unified)
	http.HandleFunc("/candleflip", ws.HandleCandleflipWS)

	// Bettor notification endpoints
	http.HandleFunc("/api/bettor/add", ws.HandleAddBettor)
	http.HandleFunc("/api/bettor/remove", ws.HandleRemoveBettor)

	addr := "0.0.0.0:8080"
	log.Printf("🚀 WebSocket server starting on %s", addr)
	log.Println("📡 Unified WebSocket: ws://localhost:8080/ws")
	log.Println("   - Subscribe to 'crash' for crash game + history")
	log.Println("   - Subscribe to 'chat' for server chat")
	log.Println("   - Subscribe to 'rooms' for global rooms")
	log.Println("   - Subscribe to 'candleflip:<roomId>' for specific room")

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal("❌ Server error:", err)
	}
}
