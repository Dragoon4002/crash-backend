package db

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"goLangServer/config"

	"github.com/redis/go-redis/v9"
)

var (
	// RedisClient is the global Redis client instance
	RedisClient *redis.Client
)

// CrashBetData represents the Redis structure for a crash game bet
type CrashBetData struct {
	PlayerAddress   string    `json:"playerAddress"`
	GameID          string    `json:"gameId"`
	BetAmount       string    `json:"betAmount"` // Wei as string
	EntryMultiplier float64   `json:"entryMultiplier"`
	Timestamp       time.Time `json:"timestamp"`
	TxHash          string    `json:"txHash"` // Transaction hash for verification
}

// CrashCashedOutData represents the Redis structure for a cashed out bet
type CrashCashedOutData struct {
	PlayerAddress    string    `json:"playerAddress"`
	GameID           string    `json:"gameId"`
	BetAmount        string    `json:"betAmount"` // Wei as string
	EntryMultiplier  float64   `json:"entryMultiplier"`
	CashoutMultiplier float64   `json:"cashoutMultiplier"`
	Payout           string    `json:"payout"` // Wei as string
	CashoutTimestamp time.Time `json:"cashoutTimestamp"`
	BuybackEligible  bool      `json:"buybackEligible"`
}

// CandleFlipGameData represents the Redis structure for a candleflip game
type CandleFlipGameData struct {
	PlayerAddress string    `json:"playerAddress"`
	GameID        string    `json:"gameId"`
	BetPerRoom    string    `json:"betPerRoom"` // Wei as string
	Rooms         uint64    `json:"rooms"`
	Odds          float64   `json:"odds"`
	Exposure      string    `json:"exposure"` // Wei as string
	Timestamp     time.Time `json:"timestamp"`
	TxHash        string    `json:"txHash"`
}

// InitRedis initializes the Redis client connection
func InitRedis() error {
	log.Println("🔌 Connecting to Redis...")

	// Get Redis configuration from environment
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "localhost:6379"
	}

	redisPassword := os.Getenv("REDIS_PASSWORD")
	redisDB := 0
	if dbStr := os.Getenv("REDIS_DB"); dbStr != "" {
		if db, err := strconv.Atoi(dbStr); err == nil {
			redisDB = db
		}
	}

	// Create Redis client
	RedisClient = redis.NewClient(&redis.Options{
		Addr:         redisURL,
		Password:     redisPassword,
		DB:           redisDB,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
		MinIdleConns: 5,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := RedisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	log.Printf("✅ Redis connected successfully - URL: %s", redisURL)
	return nil
}

// CloseRedis closes the Redis connection
func CloseRedis() error {
	if RedisClient != nil {
		log.Println("🔌 Closing Redis connection...")
		return RedisClient.Close()
	}
	return nil
}

/* =========================
   CRASH GAME FUNCTIONS
========================= */

// StoreCrashBet stores an active crash bet in Redis
func StoreCrashBet(ctx context.Context, gameID, playerAddress string, bet *CrashBetData) error {
	key := fmt.Sprintf(config.RedisCrashBetKey, gameID, playerAddress)

	// Serialize to JSON
	data, err := json.Marshal(bet)
	if err != nil {
		return fmt.Errorf("failed to marshal crash bet: %w", err)
	}

	// Store with TTL
	if err := RedisClient.Set(ctx, key, data, config.CrashGameTTL).Err(); err != nil {
		return fmt.Errorf("failed to store crash bet: %w", err)
	}

	// Add player to active players set
	playersKey := fmt.Sprintf(config.RedisCrashPlayersKey, gameID)
	if err := RedisClient.SAdd(ctx, playersKey, playerAddress).Err(); err != nil {
		return fmt.Errorf("failed to add player to set: %w", err)
	}

	// Set TTL on players set
	RedisClient.Expire(ctx, playersKey, config.ActivePlayersTTL)

	log.Printf("✅ Stored crash bet - Game: %s, Player: %s", gameID, playerAddress)
	return nil
}

// GetCrashBet retrieves an active crash bet from Redis
func GetCrashBet(ctx context.Context, gameID, playerAddress string) (*CrashBetData, error) {
	key := fmt.Sprintf(config.RedisCrashBetKey, gameID, playerAddress)

	data, err := RedisClient.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil // Bet doesn't exist
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get crash bet: %w", err)
	}

	var bet CrashBetData
	if err := json.Unmarshal([]byte(data), &bet); err != nil {
		return nil, fmt.Errorf("failed to unmarshal crash bet: %w", err)
	}

	return &bet, nil
}

// DeleteCrashBet removes an active crash bet from Redis
func DeleteCrashBet(ctx context.Context, gameID, playerAddress string) error {
	key := fmt.Sprintf(config.RedisCrashBetKey, gameID, playerAddress)

	if err := RedisClient.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete crash bet: %w", err)
	}

	// Remove player from active players set
	playersKey := fmt.Sprintf(config.RedisCrashPlayersKey, gameID)
	RedisClient.SRem(ctx, playersKey, playerAddress)

	return nil
}

// StoreCashedOut stores a cashed out bet in Redis
func StoreCashedOut(ctx context.Context, gameID, playerAddress string, data *CrashCashedOutData) error {
	key := fmt.Sprintf(config.RedisCrashCashedOutKey, gameID, playerAddress)

	// Serialize to JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal cashed out data: %w", err)
	}

	// Store with TTL
	ttl := config.CrashCashedOutTTL
	if data.BuybackEligible {
		ttl = config.BuybackTTL
	}

	if err := RedisClient.Set(ctx, key, jsonData, ttl).Err(); err != nil {
		return fmt.Errorf("failed to store cashed out data: %w", err)
	}

	log.Printf("✅ Stored cashed out data - Game: %s, Player: %s", gameID, playerAddress)
	return nil
}

// GetCashedOut retrieves a cashed out bet from Redis
func GetCashedOut(ctx context.Context, gameID, playerAddress string) (*CrashCashedOutData, error) {
	key := fmt.Sprintf(config.RedisCrashCashedOutKey, gameID, playerAddress)

	data, err := RedisClient.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil // Doesn't exist
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get cashed out data: %w", err)
	}

	var cashedOut CrashCashedOutData
	if err := json.Unmarshal([]byte(data), &cashedOut); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cashed out data: %w", err)
	}

	return &cashedOut, nil
}

// GetActivePlayers returns all active players in a crash game
func GetActivePlayers(ctx context.Context, gameID string) ([]string, error) {
	playersKey := fmt.Sprintf(config.RedisCrashPlayersKey, gameID)

	players, err := RedisClient.SMembers(ctx, playersKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get active players: %w", err)
	}

	return players, nil
}

// CleanupCrashGame removes all active bets for a crashed/rugged game
func CleanupCrashGame(ctx context.Context, gameID string) error {
	// Get all active players
	players, err := GetActivePlayers(ctx, gameID)
	if err != nil {
		return err
	}

	// Delete each player's bet
	for _, player := range players {
		if err := DeleteCrashBet(ctx, gameID, player); err != nil {
			log.Printf("⚠️  Failed to delete bet for %s: %v", player, err)
		}
	}

	// Delete the players set
	playersKey := fmt.Sprintf(config.RedisCrashPlayersKey, gameID)
	RedisClient.Del(ctx, playersKey)

	log.Printf("🧹 Cleaned up crash game %s (%d players)", gameID, len(players))
	return nil
}

/* =========================
   CANDLEFLIP FUNCTIONS
========================= */

// StoreCandleFlipGame stores a candleflip game in Redis
func StoreCandleFlipGame(ctx context.Context, gameID, playerAddress string, game *CandleFlipGameData) error {
	key := fmt.Sprintf(config.RedisCandleGameKey, gameID, playerAddress)

	// Serialize to JSON
	data, err := json.Marshal(game)
	if err != nil {
		return fmt.Errorf("failed to marshal candle game: %w", err)
	}

	// Store with TTL
	if err := RedisClient.Set(ctx, key, data, config.CandleFlipTTL).Err(); err != nil {
		return fmt.Errorf("failed to store candle game: %w", err)
	}

	log.Printf("✅ Stored candle game - ID: %s, Player: %s", gameID, playerAddress)
	return nil
}

// GetCandleFlipGame retrieves a candleflip game from Redis
func GetCandleFlipGame(ctx context.Context, gameID, playerAddress string) (*CandleFlipGameData, error) {
	key := fmt.Sprintf(config.RedisCandleGameKey, gameID, playerAddress)

	data, err := RedisClient.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil // Game doesn't exist
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get candle game: %w", err)
	}

	var game CandleFlipGameData
	if err := json.Unmarshal([]byte(data), &game); err != nil {
		return nil, fmt.Errorf("failed to unmarshal candle game: %w", err)
	}

	return &game, nil
}

// DeleteCandleFlipGame removes a candleflip game from Redis
func DeleteCandleFlipGame(ctx context.Context, gameID, playerAddress string) error {
	key := fmt.Sprintf(config.RedisCandleGameKey, gameID, playerAddress)

	if err := RedisClient.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete candle game: %w", err)
	}

	return nil
}

/* =========================
   HEALTH CHECK
========================= */

// HealthCheck performs a Redis health check
func HealthCheck(ctx context.Context) error {
	return RedisClient.Ping(ctx).Err()
}
