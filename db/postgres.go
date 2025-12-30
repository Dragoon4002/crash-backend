package db

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"goLangServer/game"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// PostgresPool is the global PostgreSQL connection pool
	PostgresPool *pgxpool.Pool
)

// CrashHistoryRecord represents a crash game history record
type CrashHistoryRecord struct {
	GameID             string             `json:"gameId"`
	ServerSeed         string             `json:"serverSeed"`
	ServerSeedHash     string             `json:"serverSeedHash"`
	Peak               float64            `json:"peak"`
	CandlestickHistory []game.CandleGroup `json:"candlestickHistory"`
	Rugged             bool               `json:"rugged"`
	CreatedAt          time.Time          `json:"createdAt"`
}

// InitPostgres initializes the PostgreSQL connection pool
func InitPostgres() error {
	log.Println("🔌 Connecting to PostgreSQL (Supabase)...")

	// Get DATABASE_URL from environment
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL environment variable not set")
	}

	// Create connection pool
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("failed to parse database URL: %w", err)
	}

	// Configure pool settings
	poolConfig.MaxConns = 25
	poolConfig.MinConns = 5
	poolConfig.MaxConnLifetime = 5 * time.Minute

	// Create pool
	PostgresPool, err = pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test connection
	if err := PostgresPool.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	log.Println("✅ PostgreSQL connected successfully (Supabase)")

	// Initialize schema
	if err := InitSchema(context.Background()); err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	return nil
}

// ClosePostgres closes the PostgreSQL connection pool
func ClosePostgres() {
	if PostgresPool != nil {
		log.Println("🔌 Closing PostgreSQL connection...")
		PostgresPool.Close()
	}
}

// InitSchema creates the database tables if they don't exist
func InitSchema(ctx context.Context) error {
	log.Println("📋 Initializing database schema...")

	// Create crash_history table
	crashHistorySchema := `
	CREATE TABLE IF NOT EXISTS crash_history (
		id SERIAL PRIMARY KEY,
		game_id TEXT NOT NULL UNIQUE,
		server_seed TEXT NOT NULL,
		server_seed_hash TEXT NOT NULL,
		peak DOUBLE PRECISION NOT NULL,
		candlestick_history JSONB NOT NULL,
		rugged BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	-- Index on game_id for fast lookups
	CREATE INDEX IF NOT EXISTS idx_crash_history_game_id ON crash_history(game_id);

	-- Index on created_at for time-based queries
	CREATE INDEX IF NOT EXISTS idx_crash_history_created_at ON crash_history(created_at DESC);
	`

	if _, err := PostgresPool.Exec(ctx, crashHistorySchema); err != nil {
		return fmt.Errorf("failed to create crash_history table: %w", err)
	}

	log.Println("✅ Database schema initialized")
	return nil
}

/* =========================
   CRASH GAME HISTORY
========================= */

// StoreCrashHistory stores a crash game result in PostgreSQL
func StoreCrashHistory(ctx context.Context, record *CrashHistoryRecord) error {
	// Serialize candlestick history to JSON
	candlestickJSON, err := json.Marshal(record.CandlestickHistory)
	if err != nil {
		return fmt.Errorf("failed to marshal candlestick history: %w", err)
	}

	query := `
		INSERT INTO crash_history
		(game_id, server_seed, server_seed_hash, peak, candlestick_history, rugged, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (game_id) DO NOTHING
	`

	_, err = PostgresPool.Exec(
		ctx,
		query,
		record.GameID,
		record.ServerSeed,
		record.ServerSeedHash,
		record.Peak,
		candlestickJSON,
		record.Rugged,
		record.CreatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to store crash history: %w", err)
	}

	log.Printf("✅ Stored crash history - Game: %s, Peak: %.2fx, Rugged: %v",
		record.GameID, record.Peak, record.Rugged)
	return nil
}

// GetCrashHistory retrieves a crash game history by game ID
func GetCrashHistory(ctx context.Context, gameID string) (*CrashHistoryRecord, error) {
	query := `
		SELECT game_id, server_seed, server_seed_hash, peak, candlestick_history, rugged, created_at
		FROM crash_history
		WHERE game_id = $1
	`

	var record CrashHistoryRecord
	var candlestickJSON []byte

	err := PostgresPool.QueryRow(ctx, query, gameID).Scan(
		&record.GameID,
		&record.ServerSeed,
		&record.ServerSeedHash,
		&record.Peak,
		&candlestickJSON,
		&record.Rugged,
		&record.CreatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, nil // Game not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get crash history: %w", err)
	}

	// Deserialize candlestick history
	if err := json.Unmarshal(candlestickJSON, &record.CandlestickHistory); err != nil {
		return nil, fmt.Errorf("failed to unmarshal candlestick history: %w", err)
	}

	return &record, nil
}

// GetRecentCrashHistory retrieves the N most recent crash games
func GetRecentCrashHistory(ctx context.Context, limit int) ([]*CrashHistoryRecord, error) {
	query := `
		SELECT game_id, server_seed, server_seed_hash, peak, candlestick_history, rugged, created_at
		FROM crash_history
		ORDER BY created_at DESC
		LIMIT $1
	`

	rows, err := PostgresPool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query crash history: %w", err)
	}
	defer rows.Close()

	var records []*CrashHistoryRecord
	for rows.Next() {
		var record CrashHistoryRecord
		var candlestickJSON []byte

		if err := rows.Scan(
			&record.GameID,
			&record.ServerSeed,
			&record.ServerSeedHash,
			&record.Peak,
			&candlestickJSON,
			&record.Rugged,
			&record.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Deserialize candlestick history
		if err := json.Unmarshal(candlestickJSON, &record.CandlestickHistory); err != nil {
			return nil, fmt.Errorf("failed to unmarshal candlestick history: %w", err)
		}

		records = append(records, &record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return records, nil
}

/* =========================
   HEALTH CHECK
========================= */

// HealthCheckPostgres performs a PostgreSQL health check
func HealthCheckPostgres(ctx context.Context) error {
	return PostgresPool.Ping(ctx)
}
