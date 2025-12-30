package config

import (
	"math/big"
	"time"
)

/* =========================
   NETWORK CONFIGURATION
========================= */

const (
	// Mantle Sepolia Testnet
	MantleSepoliaRPC = "https://rpc.sepolia.mantle.xyz"
	MantleChainID    = 5003
)

/* =========================
   CONTRACT CONFIGURATION
========================= */

const (
	// GameHouseV2 Contract
	GameHouseV2Address = "0x43a01A18a2C947179595A7b17bDCc3d88ecF04F5"

	// GameHouseV3 Contract (to be deployed)
	// TODO: Update this once V3 is deployed
	GameHouseV3Address = "0x0000000000000000000000000000000000000000"
)

/* =========================
   GAME MECHANICS - CRASH
========================= */

const (
	// Price simulation
	StartingPrice   = 1.0
	MaxTicks        = 5000
	TickInterval    = 500 * time.Millisecond // 500ms per tick
	RugProbability  = 0.01                   // 1% chance to rug each tick
	GodCandleChance = 0.005                  // 0.5% chance for god candle
	GodCandleMult   = 1.0                    // 1.0x multiplier on god candle
	BigMoveChance   = 0.10                   // 10% chance of big moves
	BigMoveMin      = 0.08                   // Minimum 8% move
	BigMoveMax      = 0.50                   // Maximum 50% move
	DriftMin        = -0.04                  // Minimum drift -4%
	DriftMax        = 0.04                   // Maximum drift +4%

	// Game timing
	CountdownDuration    = 5 * time.Second  // 5 second countdown before game starts
	GameEndWaitDuration  = 10 * time.Second // 10 second wait after game ends
	MaxGameHistory       = 50               // Keep last 50 games in memory
	InitialGroupDuration = 500              // Initial candle group duration in ms
	MergeThreshold       = 100              // Number of candles before merging

	// Peak distribution probabilities (sum to 1.0)
	PeakVeryLow  = 0.40 // 40% chance: 1.0x - 1.5x
	PeakLow      = 0.70 // 30% chance: 1.5x - 3.0x (cumulative)
	PeakMedium   = 0.88 // 18% chance: 3.0x - 10.0x (cumulative)
	PeakHigh     = 0.97 // 9% chance: 10.0x - 50.0x (cumulative)
	PeakExtreme  = 1.00 // 3% chance: 50.0x - 200.0x (cumulative)
	PeakVeryLowMax  = 1.5
	PeakLowMax      = 3.0
	PeakMediumMax   = 10.0
	PeakHighMax     = 50.0
	PeakExtremeMax  = 200.0
)

/* =========================
   ODDS CONFIGURATION
========================= */

var (
	// Base odds for CandleFlip (2.0x in 18-decimal format)
	BaseOdds = big.NewInt(2e18)

	// Minimum odds (1.2x in 18-decimal format)
	MinOdds = big.NewInt(1.2e18)

	// Reserve games for liquidity calculation
	ReserveGames = uint64(20)

	// Decimal precision
	DecimalPrecision = big.NewInt(1e18)
)

// GetBaseOddsFloat returns BASE_ODDS as float64 (2.0)
func GetBaseOddsFloat() float64 {
	return 2.0
}

// GetMinOddsFloat returns MIN_ODDS as float64 (1.2)
func GetMinOddsFloat() float64 {
	return 1.2
}

/* =========================
   REDIS TTL CONFIGURATION
========================= */

const (
	// Crash game active bet TTL (1 hour)
	// Key: crash:{gameId}:{playerAddress}
	CrashGameTTL = 1 * time.Hour

	// Crash game cashed out TTL (10 minutes)
	// Key: crash:cashedout:{gameId}:{playerAddress}
	CrashCashedOutTTL = 10 * time.Minute

	// CandleFlip game TTL (2 hours)
	// Key: candle:{gameId}:{playerAddress}
	CandleFlipTTL = 2 * time.Hour

	// Active players set TTL per game (1 hour)
	// Key: game:crash:{gameId}:players
	ActivePlayersTTL = 1 * time.Hour

	// Buyback eligibility TTL (5 minutes after cashout)
	BuybackTTL = 5 * time.Minute
)

/* =========================
   REDIS KEY PATTERNS
========================= */

const (
	// Crash game keys
	RedisCrashBetKey        = "crash:%s:%s"        // crash:{gameId}:{playerAddress}
	RedisCrashCashedOutKey  = "crash:cashedout:%s:%s" // crash:cashedout:{gameId}:{playerAddress}
	RedisCrashPlayersKey    = "game:crash:%s:players" // game:crash:{gameId}:players (SET)

	// CandleFlip game keys
	RedisCandleGameKey = "candle:%s:%s" // candle:{gameId}:{playerAddress}
)

/* =========================
   POSTGRESQL CONFIGURATION
========================= */

const (
	// Database connection settings
	PostgresHost     = "localhost"
	PostgresPort     = 5432
	PostgresUser     = "postgres"
	PostgresPassword = "password"
	PostgresDB       = "gamehouse"

	// Connection pool settings
	MaxOpenConns    = 25
	MaxIdleConns    = 5
	ConnMaxLifetime = 5 * time.Minute
)

/* =========================
   RELAYER CONFIGURATION
========================= */

const (
	// Gas limits and pricing
	RelayerGasLimit    = 150000           // Maximum gas for gasless transactions
	RelayerMaxGasPrice = 10000000000      // 10 Gwei max gas price
	RelayerMinBalance  = 50000000000000000 // 0.05 MNT minimum balance

	// Retry configuration
	MaxRetries     = 3
	RetryDelay     = 2 * time.Second
	TransactionTimeout = 30 * time.Second
)

/* =========================
   API CONFIGURATION
========================= */

const (
	// Server settings
	ServerPort = "8080"
	ServerHost = "0.0.0.0"

	// CORS settings
	AllowOrigin = "*"

	// Rate limiting
	MaxRequestsPerSecond = 100
)

/* =========================
   WEBSOCKET CONFIGURATION
========================= */

const (
	// WebSocket settings
	WSReadDeadline  = 60 * time.Second
	WSWriteDeadline = 10 * time.Second
	WSPingInterval  = 30 * time.Second

	// Buffer sizes
	WSReadBufferSize  = 1024
	WSWriteBufferSize = 1024

	// Message size limits
	MaxMessageSize = 512 * 1024 // 512KB
)

/* =========================
   VALIDATION CONSTANTS
========================= */

const (
	// Minimum bet amounts (in wei, 18 decimals)
	MinBetAmount = 1000000000000000 // 0.001 MNT

	// Maximum bet amounts (in wei, 18 decimals)
	MaxBetAmount = 100000000000000000000 // 100 MNT

	// CandleFlip room limits
	MinRooms = 1
	MaxRooms = 10

	// Multiplier limits
	MinMultiplier = 1.0  // 1.0x minimum
	MaxMultiplier = 1000.0 // 1000x maximum
)

/* =========================
   HELPER FUNCTIONS
========================= */

// WeiToMNT converts wei (uint256) to MNT (float64)
func WeiToMNT(wei *big.Int) float64 {
	if wei == nil {
		return 0
	}
	// Convert to float64 and divide by 1e18
	weiFloat := new(big.Float).SetInt(wei)
	divisor := new(big.Float).SetFloat64(1e18)
	result := new(big.Float).Quo(weiFloat, divisor)
	mnt, _ := result.Float64()
	return mnt
}

// MNTToWei converts MNT (float64) to wei (*big.Int)
func MNTToWei(mnt float64) *big.Int {
	// Multiply by 1e18 and convert to big.Int
	weiFloat := new(big.Float).SetFloat64(mnt * 1e18)
	wei, _ := weiFloat.Int(nil)
	return wei
}

// MultiplierToWei converts a multiplier (float64) to wei format (*big.Int)
func MultiplierToWei(multiplier float64) *big.Int {
	// Multiply by 1e18 for 18-decimal precision
	weiFloat := new(big.Float).SetFloat64(multiplier * 1e18)
	wei, _ := weiFloat.Int(nil)
	return wei
}

// WeiToMultiplier converts wei format (*big.Int) to multiplier (float64)
func WeiToMultiplier(wei *big.Int) float64 {
	if wei == nil {
		return 0
	}
	weiFloat := new(big.Float).SetInt(wei)
	divisor := new(big.Float).SetFloat64(1e18)
	result := new(big.Float).Quo(weiFloat, divisor)
	multiplier, _ := result.Float64()
	return multiplier
}
