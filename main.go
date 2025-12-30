package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"goLangServer/api"
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

	// Initialize Redis
	if err := db.InitRedis(); err != nil {
		log.Printf("⚠️  Warning: Failed to connect to Redis: %v", err)
		log.Println("   Server will continue but API endpoints will not work")
	}

	// Initialize PostgreSQL
	if err := db.InitPostgres(); err != nil {
		log.Printf("⚠️  Warning: Failed to connect to PostgreSQL: %v", err)
		log.Println("   Server will continue but verification endpoint will not work")
	}

	// Setup graceful shutdown
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-shutdown
		log.Println("\n🛑 Shutting down server...")

		// Close database connections
		db.CloseRedis()
		db.ClosePostgres()

		log.Println("✅ Cleanup complete")
		os.Exit(0)
	}()

	// WebSocket endpoints (with CORS)
	http.HandleFunc("/ws", corsMiddleware(ws.HandleUnifiedWS))
	http.HandleFunc("/candleflip", corsMiddleware(ws.HandleCandleflipWS))

	// Crash game API endpoints
	http.HandleFunc("/api/crash/register", corsMiddleware(api.HandleCrashRegister))
	http.HandleFunc("/api/crash/cashout", corsMiddleware(api.HandleCrashCashout))

	// CandleFlip API endpoints
	http.HandleFunc("/api/candle/register", corsMiddleware(api.HandleCandleFlipRegister))
	http.HandleFunc("/api/candle/preview-odds", corsMiddleware(api.HandleCandleFlipPreviewOdds))

	// Verification and health endpoints
	http.HandleFunc("/api/verify/", corsMiddleware(api.HandleVerifyGame))
	http.HandleFunc("/api/health", corsMiddleware(api.HandleHealthCheck))

	// Legacy endpoints (with CORS)
	http.HandleFunc("/api/bettor/add", corsMiddleware(ws.HandleAddBettor))
	http.HandleFunc("/api/bettor/remove", corsMiddleware(ws.HandleRemoveBettor))
	http.HandleFunc("/api/verify-game", corsMiddleware(ws.HandleVerifyGame))

	addr := "0.0.0.0:8080"
	log.Printf("🚀 Server starting on %s", addr)
	log.Println("")
	log.Println("📡 WebSocket Endpoints:")
	log.Println("   ws://localhost:8080/ws")
	log.Println("   - Subscribe to 'crash' for crash game + history")
	log.Println("   - Subscribe to 'chat' for server chat")
	log.Println("   - Subscribe to 'rooms' for global rooms")
	log.Println("   - Subscribe to 'candleflip:<roomId>' for specific room")
	log.Println("")
	log.Println("🎮 Crash Game API:")
	log.Println("   POST /api/crash/register - Register a crash bet")
	log.Println("   POST /api/crash/cashout - Cash out (gasless)")
	log.Println("")
	log.Println("🎲 CandleFlip API:")
	log.Println("   POST /api/candle/register - Register a candleflip game")
	log.Println("   POST /api/candle/preview-odds - Preview odds")
	log.Println("")
	log.Println("🔍 Verification:")
	log.Println("   GET /api/verify/:gameId - Verify crash game")
	log.Println("   GET /api/health - Health check")
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
