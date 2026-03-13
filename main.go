package main

import (
	"log"
	"net/http"

	"goLangServer/api"
	"goLangServer/contract"
	"goLangServer/db"
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

	// Initialize database connections
	if err := db.InitPostgres(); err != nil {
		log.Printf("⚠️  Warning: PostgreSQL initialization failed: %v", err)
		log.Println("   Chat history and crash history features will be disabled")
	} else {
		// Load crash history from DB into memory
		ws.LoadCrashHistoryFromDB()
	}
	defer db.ClosePostgres()

	// Start crash game loop after DB is ready
	ws.StartCrashGameLoop()

	if err := db.InitRedis(); err != nil {
		log.Printf("⚠️  Warning: Redis initialization failed: %v", err)
		log.Println("   Some features may not work correctly")
	}
	defer db.CloseRedis()

	// Initialize contract client
	contractClient, err := contract.NewGameHouseContract()
	if err != nil {
		log.Printf("⚠️  Warning: Contract client initialization failed: %v", err)
		log.Println("   Cashout payments will not work")
	} else {
		ws.SetContractClient(contractClient)
		defer contractClient.Close()
	}

	// WebSocket endpoints
	http.HandleFunc("/ws", ws.HandleUnifiedWS)
	http.HandleFunc("/candleflip", ws.HandleCandleflipWS)

	// API endpoints (wrapped with CORS middleware)
	http.HandleFunc("/api/crash", corsMiddleware(api.HandleGetCrashHistory))
	http.HandleFunc("/api/crash/", corsMiddleware(api.HandleGetCrashGameDetail)) // Trailing slash for :gameId
	http.HandleFunc("/api/health", corsMiddleware(api.HandleHealthCheck))
	http.HandleFunc("/api/bettor/add", corsMiddleware(api.HandleAddBettor))
	http.HandleFunc("/api/bettor/remove", corsMiddleware(api.HandleRemoveBettor))
	http.HandleFunc("/api/bettor/list", corsMiddleware(api.HandleGetActiveBettors))
	http.HandleFunc("/api/leaderboard", corsMiddleware(api.HandleGetLeaderboard))

	addr := "0.0.0.0:8080"
	log.Printf("🚀 Server starting on %s", addr)
	log.Println("")
	log.Println("📡 WebSocket Endpoints:")
	log.Println("   ws://localhost:8080/ws - Unified WebSocket")
	log.Println("   - Subscribe to 'crash' for crash game + history")
	log.Println("   - Subscribe to 'chat' for server chat")
	log.Println("   - Subscribe to 'rooms' for global rooms")
	log.Println("   - Subscribe to 'candleflip:<roomId>' for specific room")
	log.Println("")
	log.Println("🔌 API Endpoints:")
	log.Println("   GET  /api/crash - Get crash game history (last 50)")
	log.Println("   GET  /api/crash/:gameId - Get specific crash game details")
	log.Println("   GET  /api/health - Health check (Redis + PostgreSQL)")
	log.Println("   POST /api/bettor/add - Add active bettor")
	log.Println("   POST /api/bettor/remove - Remove active bettor")
	log.Println("   GET  /api/bettor/list - Get active bettors")
	log.Println("")

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal("❌ Server error:", err)
	}
}

// corsMiddleware adds CORS headers to allow frontend requests
func corsMiddleware(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		// Handle preflight OPTIONS request
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		handler(w, r)
	}
}
