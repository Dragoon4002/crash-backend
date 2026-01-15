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
	Peak               float64            `json:"peak"`
	CandlestickHistory []game.CandleGroup `json:"candlestickHistory"`
	CreatedAt          time.Time          `json:"createdAt"`
}

// ChatHistoryRecord represents a chat message
type ChatHistoryRecord struct {
	PlayerAddress string    `json:"playerAddress"`
	Message       string    `json:"message"`
	Timestamp     time.Time `json:"timestamp"`
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
		peak DOUBLE PRECISION NOT NULL,
		candlestick_history JSONB NOT NULL,
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

	// Create chat_history table
	chatHistorySchema := `
	CREATE TABLE IF NOT EXISTS chat_history (
		id SERIAL PRIMARY KEY,
		player_address TEXT NOT NULL,
		message TEXT NOT NULL,
		timestamp TIMESTAMP NOT NULL DEFAULT NOW(),
		UNIQUE(player_address, timestamp)
	);

	-- Index on timestamp for recent messages
	CREATE INDEX IF NOT EXISTS idx_chat_history_timestamp ON chat_history(timestamp DESC);
	`

	if _, err := PostgresPool.Exec(ctx, chatHistorySchema); err != nil {
		return fmt.Errorf("failed to create chat_history table: %w", err)
	}

	// Create crash_bets table
	crashBetsSchema := `
	CREATE TABLE IF NOT EXISTS crash_bets (
		id SERIAL PRIMARY KEY,
		game_id TEXT NOT NULL,
		player_address TEXT NOT NULL,
		user_id TEXT NOT NULL,
		bet_amount DOUBLE PRECISION NOT NULL,
		entry_multiplier DOUBLE PRECISION NOT NULL,
		cashout_multiplier DOUBLE PRECISION,
		payout_amount DOUBLE PRECISION,
		transaction_hash TEXT,
		payout_hash TEXT,
		status TEXT NOT NULL DEFAULT 'active',
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		cashed_out_at TIMESTAMP,
		UNIQUE(game_id, player_address)
	);

	-- Index on game_id for fast lookups
	CREATE INDEX IF NOT EXISTS idx_crash_bets_game_id ON crash_bets(game_id);

	-- Index on player_address for player history
	CREATE INDEX IF NOT EXISTS idx_crash_bets_player ON crash_bets(player_address);

	-- Index on status for active bets
	CREATE INDEX IF NOT EXISTS idx_crash_bets_status ON crash_bets(status);
	`

	if _, err := PostgresPool.Exec(ctx, crashBetsSchema); err != nil {
		return fmt.Errorf("failed to create crash_bets table: %w", err)
	}

	// Create wallet_pnl table
	walletPnLSchema := `
	CREATE TABLE IF NOT EXISTS wallet_pnl (
		wallet_address TEXT PRIMARY KEY,
		amount DOUBLE PRECISION NOT NULL DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_wallet_pnl_amount ON wallet_pnl(amount DESC);
	`

	if _, err := PostgresPool.Exec(ctx, walletPnLSchema); err != nil {
		return fmt.Errorf("failed to create wallet_pnl table: %w", err)
	}

	log.Println("✅ Database schema initialized")
	return nil
}

/* =========================
   CRASH GAME HISTORY
========================= */

// StoreCrashHistory stores a crash game result in PostgreSQL
func StoreCrashHistory(ctx context.Context, record *CrashHistoryRecord) error {
	if PostgresPool == nil {
		log.Println("⚠️  PostgreSQL not initialized, skipping crash history storage")
		return nil
	}

	// Serialize candlestick history to JSON
	candlestickJSON, err := json.Marshal(record.CandlestickHistory)
	if err != nil {
		return fmt.Errorf("failed to marshal candlestick history: %w", err)
	}

	query := `
		INSERT INTO crash_history
		(game_id, server_seed, peak, candlestick_history, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (game_id) DO NOTHING
	`

	_, err = PostgresPool.Exec(
		ctx,
		query,
		record.GameID,
		record.ServerSeed,
		record.Peak,
		candlestickJSON,
		record.CreatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to store crash history: %w", err)
	}

	log.Printf("✅ Stored crash history - Game: %s, Peak: %.2fx",
		record.GameID, record.Peak)
	return nil
}

// GetCrashHistory retrieves a crash game history by game ID
func GetCrashHistory(ctx context.Context, gameID string) (*CrashHistoryRecord, error) {
	if PostgresPool == nil {
		return nil, nil
	}

	query := `
		SELECT game_id, server_seed, peak, candlestick_history, created_at
		FROM crash_history
		WHERE game_id = $1
	`

	var record CrashHistoryRecord
	var candlestickJSON []byte

	err := PostgresPool.QueryRow(ctx, query, gameID).Scan(
		&record.GameID,
		&record.ServerSeed,
		&record.Peak,
		&candlestickJSON,
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
	if PostgresPool == nil {
		return []*CrashHistoryRecord{}, nil
	}

	query := `
		SELECT game_id, server_seed, peak, candlestick_history, created_at
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
			&record.Peak,
			&candlestickJSON,
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
   CHAT HISTORY
========================= */

// StoreChatMessage stores a chat message in PostgreSQL
func StoreChatMessage(ctx context.Context, record *ChatHistoryRecord) error {
	if PostgresPool == nil {
		log.Println("⚠️  PostgreSQL not initialized, skipping chat message storage")
		return nil
	}

	query := `
		INSERT INTO chat_history
		(player_address, message, timestamp)
		VALUES ($1, $2, $3)
	`

	_, err := PostgresPool.Exec(
		ctx,
		query,
		record.PlayerAddress,
		record.Message,
		record.Timestamp,
	)

	if err != nil {
		return fmt.Errorf("failed to store chat message: %w", err)
	}

	return nil
}

// GetRecentChatMessages retrieves the N most recent chat messages
func GetRecentChatMessages(ctx context.Context, limit int) ([]*ChatHistoryRecord, error) {
	if PostgresPool == nil {
		return []*ChatHistoryRecord{}, nil // Return empty slice if database not initialized
	}

	query := `
		SELECT player_address, message, timestamp
		FROM chat_history
		ORDER BY timestamp DESC
		LIMIT $1
	`

	rows, err := PostgresPool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query chat history: %w", err)
	}
	defer rows.Close()

	var records []*ChatHistoryRecord
	for rows.Next() {
		var record ChatHistoryRecord
		if err := rows.Scan(
			&record.PlayerAddress,
			&record.Message,
			&record.Timestamp,
		); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		records = append(records, &record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	// Reverse to get chronological order (oldest first)
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}

	return records, nil
}

/* =========================
   CRASH BETS
========================= */

// CrashBetRecord represents a crash game bet
type CrashBetRecord struct {
	ID                 int       `json:"id"`
	GameID             string    `json:"gameId"`
	PlayerAddress      string    `json:"playerAddress"`
	UserID             string    `json:"userId"`
	BetAmount          float64   `json:"betAmount"`
	EntryMultiplier    float64   `json:"entryMultiplier"`
	CashoutMultiplier  *float64  `json:"cashoutMultiplier,omitempty"`
	PayoutAmount       *float64  `json:"payoutAmount,omitempty"`
	TransactionHash    string    `json:"transactionHash"`
	PayoutHash         *string   `json:"payoutHash,omitempty"`
	Status             string    `json:"status"` // active, cashed_out, lost
	CreatedAt          time.Time `json:"createdAt"`
	CashedOutAt        *time.Time `json:"cashedOutAt,omitempty"`
}

// StoreCrashBetPostgres stores a new crash bet in PostgreSQL
func StoreCrashBetPostgres(ctx context.Context, bet *CrashBetRecord) error {
	if PostgresPool == nil {
		log.Println("⚠️  PostgreSQL not initialized, skipping bet storage")
		return nil
	}

	query := `
		INSERT INTO crash_bets
		(game_id, player_address, user_id, bet_amount, entry_multiplier, transaction_hash, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (game_id, player_address) DO NOTHING
	`

	_, err := PostgresPool.Exec(
		ctx,
		query,
		bet.GameID,
		bet.PlayerAddress,
		bet.UserID,
		bet.BetAmount,
		bet.EntryMultiplier,
		bet.TransactionHash,
		bet.Status,
		bet.CreatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to store crash bet: %w", err)
	}

	log.Printf("✅ Stored crash bet - Player: %s, Amount: %.4f, Entry: %.2fx",
		bet.PlayerAddress, bet.BetAmount, bet.EntryMultiplier)
	return nil
}

// UpdateCrashBetCashout updates a bet when player cashes out
func UpdateCrashBetCashout(ctx context.Context, gameID, playerAddress string, cashoutMultiplier, payoutAmount float64, payoutHash string) error {
	if PostgresPool == nil {
		log.Println("⚠️  PostgreSQL not initialized, skipping bet update")
		return nil
	}

	query := `
		UPDATE crash_bets
		SET cashout_multiplier = $1,
		    payout_amount = $2,
		    payout_hash = $3,
		    status = 'cashed_out',
		    cashed_out_at = NOW()
		WHERE game_id = $4 AND player_address = $5 AND status = 'active'
	`

	result, err := PostgresPool.Exec(
		ctx,
		query,
		cashoutMultiplier,
		payoutAmount,
		payoutHash,
		gameID,
		playerAddress,
	)

	if err != nil {
		return fmt.Errorf("failed to update crash bet: %w", err)
	}

	rowsAffected := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("no active bet found for player %s in game %s", playerAddress, gameID)
	}

	log.Printf("✅ Updated crash bet - Player: %s, Cashout: %.2fx, Payout: %.4f",
		playerAddress, cashoutMultiplier, payoutAmount)
	return nil
}

// MarkBetsAsLost marks all active bets for a game as lost
func MarkBetsAsLost(ctx context.Context, gameID string) error {
	if PostgresPool == nil {
		log.Println("⚠️  PostgreSQL not initialized, skipping")
		return nil
	}

	query := `
		UPDATE crash_bets
		SET status = 'lost',
		    payout_amount = 0
		WHERE game_id = $1 AND status = 'active'
	`

	result, err := PostgresPool.Exec(ctx, query, gameID)
	if err != nil {
		return fmt.Errorf("failed to mark bets as lost: %w", err)
	}

	rowsAffected := result.RowsAffected()
	log.Printf("🔴 Marked %d bets as lost for game %s", rowsAffected, gameID)
	return nil
}

// GetActiveBet retrieves the active bet for a player in a game
func GetActiveBet(ctx context.Context, gameID, playerAddress string) (*CrashBetRecord, error) {
	if PostgresPool == nil {
		return nil, nil
	}

	query := `
		SELECT id, game_id, player_address, user_id, bet_amount, entry_multiplier,
		       transaction_hash, status, created_at
		FROM crash_bets
		WHERE game_id = $1 AND player_address = $2 AND status = 'active'
	`

	var bet CrashBetRecord
	err := PostgresPool.QueryRow(ctx, query, gameID, playerAddress).Scan(
		&bet.ID,
		&bet.GameID,
		&bet.PlayerAddress,
		&bet.UserID,
		&bet.BetAmount,
		&bet.EntryMultiplier,
		&bet.TransactionHash,
		&bet.Status,
		&bet.CreatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get active bet: %w", err)
	}

	return &bet, nil
}

/* =========================
   HEALTH CHECK
========================= */

// HealthCheckPostgres performs a PostgreSQL health check
func HealthCheckPostgres(ctx context.Context) error {
	if PostgresPool == nil {
		return fmt.Errorf("PostgreSQL connection pool not initialized")
	}
	return PostgresPool.Ping(ctx)
}

/* =========================
   WALLET PNL
========================= */

// WalletPnLRecord represents a wallet's cumulative PnL
type WalletPnLRecord struct {
	WalletAddress string  `json:"walletAddress"`
	Amount        float64 `json:"amount"`
	Rank          int     `json:"rank,omitempty"`
}

// SubtractWalletPnL subtracts bet amount from wallet's PnL (upsert)
func SubtractWalletPnL(ctx context.Context, walletAddress string, betAmount float64) error {
	if PostgresPool == nil {
		log.Println("⚠️  PostgreSQL not initialized, skipping PnL update")
		return nil
	}

	query := `
		INSERT INTO wallet_pnl (wallet_address, amount)
		VALUES ($1, 0 - $2)
		ON CONFLICT (wallet_address) DO UPDATE
		SET amount = wallet_pnl.amount - $2
	`

	_, err := PostgresPool.Exec(ctx, query, walletAddress, betAmount)
	if err != nil {
		return fmt.Errorf("failed to subtract wallet PnL: %w", err)
	}

	log.Printf("📉 Subtracted %.4f from wallet %s PnL", betAmount, walletAddress)
	return nil
}

// AddWalletPnL adds payout amount to wallet's PnL
func AddWalletPnL(ctx context.Context, walletAddress string, payoutAmount float64) error {
	if PostgresPool == nil {
		log.Println("⚠️  PostgreSQL not initialized, skipping PnL update")
		return nil
	}

	query := `
		INSERT INTO wallet_pnl (wallet_address, amount)
		VALUES ($1, $2)
		ON CONFLICT (wallet_address) DO UPDATE
		SET amount = wallet_pnl.amount + $2
	`

	_, err := PostgresPool.Exec(ctx, query, walletAddress, payoutAmount)
	if err != nil {
		return fmt.Errorf("failed to add wallet PnL: %w", err)
	}

	log.Printf("📈 Added %.4f to wallet %s PnL", payoutAmount, walletAddress)
	return nil
}

// GetWalletPnLLeaderboard returns top N wallets sorted by PnL descending
func GetWalletPnLLeaderboard(ctx context.Context, limit int) ([]*WalletPnLRecord, error) {
	if PostgresPool == nil {
		return []*WalletPnLRecord{}, nil
	}

	query := `
		SELECT wallet_address, amount,
		       ROW_NUMBER() OVER (ORDER BY amount DESC) as rank
		FROM wallet_pnl
		ORDER BY amount DESC
		LIMIT $1
	`

	rows, err := PostgresPool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query leaderboard: %w", err)
	}
	defer rows.Close()

	var records []*WalletPnLRecord
	for rows.Next() {
		var record WalletPnLRecord
		if err := rows.Scan(&record.WalletAddress, &record.Amount, &record.Rank); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		records = append(records, &record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return records, nil
}

// GetWalletPnLRank returns a specific wallet's rank and PnL
func GetWalletPnLRank(ctx context.Context, walletAddress string) (*WalletPnLRecord, error) {
	if PostgresPool == nil {
		return nil, nil
	}

	query := `
		SELECT wallet_address, amount, rank FROM (
			SELECT wallet_address, amount,
			       ROW_NUMBER() OVER (ORDER BY amount DESC) as rank
			FROM wallet_pnl
		) ranked
		WHERE wallet_address = $1
	`

	var record WalletPnLRecord
	err := PostgresPool.QueryRow(ctx, query, walletAddress).Scan(
		&record.WalletAddress,
		&record.Amount,
		&record.Rank,
	)

	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get wallet rank: %w", err)
	}

	return &record, nil
}
