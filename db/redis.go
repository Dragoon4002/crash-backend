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

// CrashBetData represents a single crash bet
// Stored as [multiplier, amount] in Redis hash
type CrashBetData struct {
	EntryMultiplier float64 `json:"multiplier"`
	BetAmount       float64 `json:"amount"` // In MNT (not wei)
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
   CRASH GAME FUNCTIONS (Hash Map Structure)
   Redis Key: crash:{gameId} -> Hash{playerAddress: [multiplier, amount]}
========================= */

// StoreCrashBet stores an active crash bet in Redis hash map
func StoreCrashBet(ctx context.Context, gameID, playerAddress string, bet *CrashBetData) error {
	hashKey := fmt.Sprintf("crash:%s", gameID)

	// Serialize bet data to JSON
	data, err := json.Marshal(bet)
	if err != nil {
		return fmt.Errorf("failed to marshal crash bet: %w", err)
	}

	// Store in hash map (HSET)
	if err := RedisClient.HSet(ctx, hashKey, playerAddress, data).Err(); err != nil {
		return fmt.Errorf("failed to store crash bet: %w", err)
	}

	// Set TTL on hash (only if new)
	RedisClient.Expire(ctx, hashKey, config.CrashGameTTL)

	log.Printf("✅ Stored crash bet - Game: %s, Player: %s, Mult: %.2fx, Amount: %.4f MNT",
		gameID, playerAddress, bet.EntryMultiplier, bet.BetAmount)
	return nil
}

// GetCrashBet retrieves an active crash bet from Redis hash map
func GetCrashBet(ctx context.Context, gameID, playerAddress string) (*CrashBetData, error) {
	hashKey := fmt.Sprintf("crash:%s", gameID)

	// Get from hash map (HGET)
	data, err := RedisClient.HGet(ctx, hashKey, playerAddress).Result()
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

// GetAllCrashBets retrieves all active bets for a game
func GetAllCrashBets(ctx context.Context, gameID string) (map[string]*CrashBetData, error) {
	hashKey := fmt.Sprintf("crash:%s", gameID)

	// Get all fields from hash (HGETALL)
	data, err := RedisClient.HGetAll(ctx, hashKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get all crash bets: %w", err)
	}

	bets := make(map[string]*CrashBetData)
	for playerAddress, betJSON := range data {
		var bet CrashBetData
		if err := json.Unmarshal([]byte(betJSON), &bet); err != nil {
			log.Printf("⚠️  Failed to unmarshal bet for %s: %v", playerAddress, err)
			continue
		}
		bets[playerAddress] = &bet
	}

	return bets, nil
}

// DeleteCrashBet removes an active crash bet from Redis hash map
func DeleteCrashBet(ctx context.Context, gameID, playerAddress string) error {
	hashKey := fmt.Sprintf("crash:%s", gameID)

	// Remove from hash (HDEL)
	if err := RedisClient.HDel(ctx, hashKey, playerAddress).Err(); err != nil {
		return fmt.Errorf("failed to delete crash bet: %w", err)
	}

	log.Printf("🗑️  Deleted crash bet - Game: %s, Player: %s", gameID, playerAddress)
	return nil
}

// GetActivePlayers returns all active player addresses in a crash game
func GetActivePlayers(ctx context.Context, gameID string) ([]string, error) {
	hashKey := fmt.Sprintf("crash:%s", gameID)

	// Get all keys from hash (HKEYS)
	players, err := RedisClient.HKeys(ctx, hashKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get active players: %w", err)
	}

	return players, nil
}

// CleanupCrashGame removes all active bets for a crashed game
func CleanupCrashGame(ctx context.Context, gameID string) error {
	hashKey := fmt.Sprintf("crash:%s", gameID)

	// Get count before deleting
	count, _ := RedisClient.HLen(ctx, hashKey).Result()

	// Delete entire hash
	if err := RedisClient.Del(ctx, hashKey).Err(); err != nil {
		return fmt.Errorf("failed to cleanup crash game: %w", err)
	}

	log.Printf("🧹 Cleaned up crash game %s (%d players)", gameID, count)
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
