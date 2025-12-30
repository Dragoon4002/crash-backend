package ws

import (
	"context"
	"log"
	"math"
	"net/http"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"goLangServer/crypto"
	"goLangServer/db"
	"goLangServer/game"

	"github.com/gorilla/websocket"
)

// CrashGameHistory stores info about past crash games
type CrashGameHistory struct {
	GameID         string             `json:"gameId"`
	PeakMultiplier float64            `json:"peakMultiplier"`
	Rugged         bool               `json:"rugged"`
	Candles        []game.CandleGroup `json:"candles"`
	Timestamp      time.Time          `json:"timestamp"`
}

// ActiveBettor represents a player with an active bet
type ActiveBettor struct {
	Address         string    `json:"address"`
	BetAmount       float64   `json:"betAmount"`
	EntryMultiplier float64   `json:"entryMultiplier"`
	BetTime         time.Time `json:"betTime"`
}

const (
	MaxGameHistory           = 10
	InitialGroupDurationMs = 1000 // 1 second candles
	MergeThreshold         = 25   // Merge when we have 25+ groups
)

var clientCount int64

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var (
	crashGameHistory      []CrashGameHistory
	gameHistoryMutex      sync.RWMutex
	currentCrashGame      *CrashGameState
	currentCrashGameMutex sync.RWMutex
	activeBettors         = make(map[string]*ActiveBettor)
	activeBettorsMutex    sync.RWMutex
)

type CrashGameState struct {
	GameID         string
	ServerSeed     string
	ServerSeedHash string
	Status         string // "countdown", "running", "crashed"
	ContractGameID *big.Int
}

func init() {
	// Start the crash game loop
	go runCrashGameLoop()
}

func HandleWS(w http.ResponseWriter, r *http.Request) {
	log.Println("📥 WebSocket connection attempt from:", r.RemoteAddr)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("❌ WebSocket upgrade failed:", err)
		return
	}
	defer conn.Close()

	// Increment client count
	atomic.AddInt64(&clientCount, 1)
	count := atomic.LoadInt64(&clientCount)
	log.Printf("✅ Client connected! Total clients: %d\n", count)
	defer func() {
		atomic.AddInt64(&clientCount, -1)
		log.Printf("👋 Client disconnected. Total clients: %d\n", atomic.LoadInt64(&clientCount))
	}()

	// Game loop - restart games with 15 second delay
	for {
		serverSeed, seedHash := crypto.GenerateServerSeed()
		gameID := time.Now().Format("20060102-150405.000")

		// Send game start
		startMsg := map[string]interface{}{
			"type": "game_start",
			"data": map[string]interface{}{
				"gameId":         gameID,
				"serverSeedHash": seedHash,
				"startingPrice":  1.0,
				"connectedUsers": atomic.LoadInt64(&clientCount),
			},
		}
		if err := conn.WriteJSON(startMsg); err != nil {
			return
		}

		// Simulate game tick-by-tick
		combined := serverSeed + "-" + gameID
		rng := game.NewSeededRNG(combined)

		price := 1.0
		peak := 1.0
		tick := 0
		rugged := false

		// Candle grouping state
		var groups []game.CandleGroup
		var currentGroup *game.CandleGroup
		groupDuration := int64(InitialGroupDurationMs)
		groupStartTime := time.Now().UnixMilli()

		for tick < 5000 {
			if rng.Float64() < game.RugProb {
				rugged = true
				break
			}

			// God candle (v3)
			if rng.Float64() < game.GodCandleChance && price <= 100 {
				price *= game.GodCandleMult
			} else {
				var change float64

				// Big move
				if rng.Float64() < game.BigMoveChance {
					move := game.BigMoveMin + rng.Float64()*(game.BigMoveMax-game.BigMoveMin)
					if rng.Float64() > 0.5 {
						change = move
					} else {
						change = -move
					}
				} else {
					// Normal drift
					drift := game.DriftMin + rng.Float64()*(game.DriftMax-game.DriftMin)
					volatility := 0.005 * math.Min(10, math.Sqrt(price))
					noise := volatility * (2*rng.Float64() - 1)
					change = drift + noise
				}

				price = price * (1 + change)
				if price < 0 {
					price = 0
				}
			}

			if price > peak {
				peak = price
			}

			// Candle grouping logic
			now := time.Now().UnixMilli()

			// Initialize first group if needed
			if currentGroup == nil {
				currentGroup = &game.CandleGroup{
					Open:       price,
					Close:      &price,
					Max:        price,
					Min:        price,
					ValueList:  []float64{price},
					StartTime:  now,
					DurationMs: groupDuration,
					IsComplete: false,
				}
				groupStartTime = now
			} else {
				// Check if we need to complete current group and start a new one
				elapsed := now - groupStartTime

				if elapsed >= groupDuration {
					// Complete current group - create a deep copy with FINAL CLOSE VALUE
					// CRITICAL: Must copy the close VALUE, not the pointer reference
					finalCloseValue := *currentGroup.Close // Dereference the pointer to get the actual value
					completedGroup := game.CandleGroup{
						Open:       currentGroup.Open,
						Close:      &finalCloseValue, // New pointer to the final value
						Max:        currentGroup.Max,
						Min:        currentGroup.Min,
						ValueList:  []float64{}, // Empty valueList for completed candles (save bandwidth)
						StartTime:  currentGroup.StartTime,
						DurationMs: currentGroup.DurationMs,
						IsComplete: true,
					}
					// Don't copy valueList - completed candles don't need it
					groups = append(groups, completedGroup)
					log.Printf("📊 Completed candle #%d: Open=%.2f, Close=%.2f (IMMUTABLE at %p), Max=%.2f, Min=%.2f",
						len(groups), completedGroup.Open, *completedGroup.Close, completedGroup.Close, completedGroup.Max, completedGroup.Min)

					// Check if we need to merge
					if len(groups) >= MergeThreshold {
						log.Printf("🔄 Merging %d groups (threshold reached)", len(groups))
						groups, groupDuration = mergeGroups(groups, groupDuration)
						log.Printf("✅ After merge: %d groups, new duration: %dms", len(groups), groupDuration)
					}

					// Start new group
					currentGroup = &game.CandleGroup{
						Open:       price,
						Close:      &price,
						Max:        price,
						Min:        price,
						ValueList:  []float64{price},
						StartTime:  now,
						DurationMs: groupDuration,
						IsComplete: false,
					}
					groupStartTime = now
					log.Printf("🆕 Started new candle group with price %.2f, duration %dms", price, groupDuration)
				} else {
					// Update current group
					currentGroup.ValueList = append(currentGroup.ValueList, price)
					currentGroup.Close = &price
					currentGroup.Max = math.Max(currentGroup.Max, price)
					currentGroup.Min = math.Min(currentGroup.Min, price)
				}
			}

			// Send completed groups separately from current group
			// Always ensure previousCandles is an array (not nil) for JSON serialization
			var previousCandles []game.CandleGroup
			if len(groups) > 0 {
				previousCandles = make([]game.CandleGroup, len(groups))
				copy(previousCandles, groups)
			} else {
				previousCandles = []game.CandleGroup{} // Empty array instead of nil
			}

			response := map[string]interface{}{
				"type": "price_update",
				"data": map[string]interface{}{
					"tick":            tick,
					"price":           price,
					"multiplier":      price,
					"gameEnded":       false,
					"connectedUsers":  atomic.LoadInt64(&clientCount),
					"previousCandles": previousCandles,
				},
			}

			// Add current candle if it exists
			if currentGroup != nil {
				response["data"].(map[string]interface{})["currentCandle"] = *currentGroup
			}

			// Debug log first few ticks to verify data structure
			if tick < 5 {
				log.Printf("📤 Tick %d - Previous: %d candles, Current: %v, CurrentGroup details: %+v",
					tick, len(previousCandles), currentGroup != nil, currentGroup)
			}

			if err := conn.WriteJSON(response); err != nil {
				log.Printf("❌ Failed to send JSON: %v", err)
				return
			}

			time.Sleep(500 * time.Millisecond)
			tick++
		}

		// Complete the final group if game ended
		if currentGroup != nil && !currentGroup.IsComplete {
			// Get the final close value BEFORE creating the copy
			var finalCloseValue float64
			if rugged {
				finalCloseValue = 0.0
				currentGroup.Min = 0.0
			} else {
				finalCloseValue = *currentGroup.Close
			}

			// Create deep copy with FINAL VALUE (not pointer reference)
			finalGroup := game.CandleGroup{
				Open:       currentGroup.Open,
				Close:      &finalCloseValue, // New pointer to final value
				Max:        currentGroup.Max,
				Min:        currentGroup.Min,
				ValueList:  []float64{}, // Empty for completed candles
				StartTime:  currentGroup.StartTime,
				DurationMs: currentGroup.DurationMs,
				IsComplete: true,
			}
			// Don't copy valueList - completed candles don't need it
			groups = append(groups, finalGroup)
		}

		// End game - send all completed candles (no current candle since game ended)
		if err := conn.WriteJSON(map[string]interface{}{
			"type": "game_end",
			"data": map[string]interface{}{
				"gameId":          gameID,
				"serverSeed":      serverSeed,
				"serverSeedHash":  seedHash,
				"peakMultiplier":  peak,
				"rugged":          rugged,
				"totalTicks":      tick,
				"connectedUsers":  atomic.LoadInt64(&clientCount),
				"previousCandles": groups,
			},
		}); err != nil {
			return
		}

		// Wait 15 seconds before starting next game
		time.Sleep(15 * time.Second)
	}
}


func runCrashGameLoop() {
	log.Println("🎰 Crash game loop started")

	for {
		serverSeed, seedHash := crypto.GenerateServerSeed()
		gameID := time.Now().Format("20060102-150405.000")

		// Convert gameID to big.Int for contract (use Unix timestamp)
		timestamp := time.Now().Unix()
		contractGameID := big.NewInt(timestamp)

		currentCrashGameMutex.Lock()
		currentCrashGame = &CrashGameState{
			GameID:         gameID,
			ServerSeed:     serverSeed,
			ServerSeedHash: seedHash,
			Status:         "countdown",
			ContractGameID: contractGameID,
		}
		currentCrashGameMutex.Unlock()

		// Set current game ID for API handlers to access
		SetCurrentGameID(contractGameID.String())

		// Broadcast game start (send contractGameID as string for client)
		crashBroadcast <- map[string]interface{}{
			"type": "game_start",
			"data": map[string]interface{}{
				"gameId":         contractGameID.String(), // Send contract game ID to client
				"serverSeedHash": seedHash,
				"startingPrice":  1.0,
			},
		}

		// Countdown: 3, 2, 1
		for i := 3; i > 0; i-- {
			crashBroadcast <- map[string]interface{}{
				"type": "countdown",
				"data": map[string]interface{}{
					"countdown": i,
				},
			}
			time.Sleep(1 * time.Second)
		}

		// Update status to running
		currentCrashGameMutex.Lock()
		currentCrashGame.Status = "running"
		currentCrashGameMutex.Unlock()

		// Run game simulation
		combined := serverSeed + "-" + gameID
		rng := game.NewSeededRNG(combined)

		price := 1.0
		peak := 1.0
		tick := 0
		rugged := false

		// Candle grouping state
		var groups []game.CandleGroup
		var currentGroup *game.CandleGroup
		groupDuration := int64(InitialGroupDurationMs)
		groupStartTime := time.Now().UnixMilli()

		for tick < 5000 {
			if rng.Float64() < game.RugProb {
				rugged = true
				break
			}

			// God candle
			if rng.Float64() < game.GodCandleChance && price <= 100 {
				price *= game.GodCandleMult
			} else {
				var change float64

				// Big move
				if rng.Float64() < game.BigMoveChance {
					move := game.BigMoveMin + rng.Float64()*(game.BigMoveMax-game.BigMoveMin)
					if rng.Float64() > 0.5 {
						change = move
					} else {
						change = -move
					}
				} else {
					// Normal drift
					drift := game.DriftMin + rng.Float64()*(game.DriftMax-game.DriftMin)
					volatility := 0.005 * math.Min(10, math.Sqrt(price))
					noise := volatility * (2*rng.Float64() - 1)
					change = drift + noise
				}

				price = price * (1 + change)
				if price < 0 {
					price = 0
				}
			}

			if price > peak {
				peak = price
			}

			// Candle grouping logic
			now := time.Now().UnixMilli()

			if currentGroup == nil {
				currentGroup = &game.CandleGroup{
					Open:       price,
					Close:      &price,
					Max:        price,
					Min:        price,
					ValueList:  []float64{price},
					StartTime:  now,
					DurationMs: groupDuration,
					IsComplete: false,
				}
				groupStartTime = now
			} else {
				elapsed := now - groupStartTime

				if elapsed >= groupDuration {
					// Complete current group
					finalCloseValue := *currentGroup.Close
					completedGroup := game.CandleGroup{
						Open:       currentGroup.Open,
						Close:      &finalCloseValue,
						Max:        currentGroup.Max,
						Min:        currentGroup.Min,
						ValueList:  []float64{},
						StartTime:  currentGroup.StartTime,
						DurationMs: currentGroup.DurationMs,
						IsComplete: true,
					}
					groups = append(groups, completedGroup)

					// Check if we need to merge
					if len(groups) >= MergeThreshold {
						groups, groupDuration = mergeGroups(groups, groupDuration)
					}

					// Start new group
					currentGroup = &game.CandleGroup{
						Open:       price,
						Close:      &price,
						Max:        price,
						Min:        price,
						ValueList:  []float64{price},
						StartTime:  now,
						DurationMs: groupDuration,
						IsComplete: false,
					}
					groupStartTime = now
				} else {
					// Update current group
					currentGroup.ValueList = append(currentGroup.ValueList, price)
					currentGroup.Close = &price
					currentGroup.Max = math.Max(currentGroup.Max, price)
					currentGroup.Min = math.Min(currentGroup.Min, price)
				}
			}

			// Broadcast price update
			var previousCandles []game.CandleGroup
			if len(groups) > 0 {
				previousCandles = make([]game.CandleGroup, len(groups))
				copy(previousCandles, groups)
			} else {
				previousCandles = []game.CandleGroup{}
			}

			message := map[string]interface{}{
				"type": "price_update",
				"data": map[string]interface{}{
					"gameId":          contractGameID.String(), // Include gameId in every update
					"tick":            tick,
					"price":           price,
					"multiplier":      price,
					"gameEnded":       false,
					"previousCandles": previousCandles,
				},
			}

			if currentGroup != nil {
				message["data"].(map[string]interface{})["currentCandle"] = *currentGroup
			}

			crashBroadcast <- message

			time.Sleep(500 * time.Millisecond)
			tick++
		}

		// Complete final group
		if currentGroup != nil && !currentGroup.IsComplete {
			var finalCloseValue float64
			if rugged {
				finalCloseValue = 0.0
				currentGroup.Min = 0.0
			} else {
				finalCloseValue = *currentGroup.Close
			}

			finalGroup := game.CandleGroup{
				Open:       currentGroup.Open,
				Close:      &finalCloseValue,
				Max:        currentGroup.Max,
				Min:        currentGroup.Min,
				ValueList:  []float64{},
				StartTime:  currentGroup.StartTime,
				DurationMs: currentGroup.DurationMs,
				IsComplete: true,
			}
			groups = append(groups, finalGroup)
		}

		// Update status to crashed
		currentCrashGameMutex.Lock()
		currentCrashGame.Status = "crashed"
		currentCrashGameMutex.Unlock()

		// Broadcast game end FIRST
		crashBroadcast <- map[string]interface{}{
			"type": "game_end",
			"data": map[string]interface{}{
				"gameId":          contractGameID.String(),
				"serverSeed":      serverSeed,
				"serverSeedHash":  seedHash,
				"peakMultiplier":  peak,
				"rugged":          rugged,
				"totalTicks":      tick,
				"previousCandles": groups,
			},
		}

		// Add to history
		gameHistoryMutex.Lock()
		crashGameHistory = append(crashGameHistory, CrashGameHistory{
			GameID:         gameID,
			PeakMultiplier: peak,
			Rugged:         rugged,
			Candles:        groups,
			Timestamp:      time.Now(),
		})
		// Keep only last 10 games
		if len(crashGameHistory) > MaxGameHistory {
			crashGameHistory = crashGameHistory[len(crashGameHistory)-MaxGameHistory:]
		}
		gameHistoryMutex.Unlock()

		// Store game result in PostgreSQL
		go func() {
			storeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			historyRecord := &db.CrashHistoryRecord{
				GameID:             gameID,
				ServerSeed:         serverSeed,
				ServerSeedHash:     seedHash,
				Peak:               peak,
				CandlestickHistory: groups,
				Rugged:             rugged,
				CreatedAt:          time.Now(),
			}

			if err := db.StoreCrashHistory(storeCtx, historyRecord); err != nil {
				log.Printf("⚠️ Failed to store crash history in PostgreSQL: %v", err)
			}
		}()

		// Clean up Redis for this game
		gameIDStr := contractGameID.String()
		go func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := db.CleanupCrashGame(cleanupCtx, gameIDStr); err != nil {
				log.Printf("⚠️ Failed to cleanup Redis: %v", err)
			}
		}()

		log.Printf("🎲 Crash game %s finished - Peak: %.2fx, Rugged: %v", gameID, peak, rugged)

		// Broadcast updated history
		updatedHistory := getCrashGameHistory()
		crashBroadcast <- map[string]interface{}{
			"type":    "crash_history",
			"history": updatedHistory,
		}
		log.Printf("📜 Broadcasted updated crash history (%d games)", len(updatedHistory))

		// Clear all active bettors for next game
		ClearActiveBettors()

		// Wait before next game
		time.Sleep(15 * time.Second)
	}
}

// mergeGroups merges candlestick groups when threshold is reached
func mergeGroups(groups []game.CandleGroup, currentDuration int64) ([]game.CandleGroup, int64) {
	// Simple merge: combine pairs
	merged := make([]game.CandleGroup, 0, len(groups)/2+1)
	newDuration := currentDuration * 2

	for i := 0; i < len(groups); i += 2 {
		if i+1 < len(groups) {
			// Merge two groups
			g1, g2 := groups[i], groups[i+1]
			closeVal := *g2.Close
			merged = append(merged, game.CandleGroup{
				Open:       g1.Open,
				Close:      &closeVal,
				Max:        math.Max(g1.Max, g2.Max),
				Min:        math.Min(g1.Min, g2.Min),
				ValueList:  []float64{},
				StartTime:  g1.StartTime,
				DurationMs: newDuration,
				IsComplete: true,
			})
		} else {
			// Odd one out
			merged = append(merged, groups[i])
		}
	}

	log.Printf("🔄 Merged %d groups into %d (new duration: %dms)", len(groups), len(merged), newDuration)
	return merged, newDuration
}

// AddActiveBettor adds a new bettor to the active list
func AddActiveBettor(address string, amount, multiplier float64) {
	activeBettorsMutex.Lock()
	defer activeBettorsMutex.Unlock()

	activeBettors[address] = &ActiveBettor{
		Address:         address,
		BetAmount:       amount,
		EntryMultiplier: multiplier,
		BetTime:         time.Now(),
	}

	log.Printf("➕ Bettor added: %s @ %.2fx (%.4f MNT)", address, multiplier, amount)
	broadcastActiveBettors()
}

// RemoveActiveBettor removes a bettor from the active list
func RemoveActiveBettor(address string) {
	activeBettorsMutex.Lock()
	defer activeBettorsMutex.Unlock()

	if _, exists := activeBettors[address]; exists {
		delete(activeBettors, address)
		log.Printf("➖ Bettor removed: %s", address)
		broadcastActiveBettors()
	}
}

// ClearActiveBettors removes all bettors
func ClearActiveBettors() {
	activeBettorsMutex.Lock()
	defer activeBettorsMutex.Unlock()

	count := len(activeBettors)
	activeBettors = make(map[string]*ActiveBettor)

	if count > 0 {
		log.Printf("🧹 Cleared %d active bettors", count)
		broadcastActiveBettors()
	}
}

// GetActiveBettors returns a copy of current active bettors
func GetActiveBettors() []*ActiveBettor {
	activeBettorsMutex.RLock()
	defer activeBettorsMutex.RUnlock()

	list := make([]*ActiveBettor, 0, len(activeBettors))
	for _, bettor := range activeBettors {
		list = append(list, bettor)
	}
	return list
}

// broadcastActiveBettors sends updated bettor list to all subscribers
func broadcastActiveBettors() {
	list := make([]*ActiveBettor, 0, len(activeBettors))
	for _, bettor := range activeBettors {
		list = append(list, bettor)
	}

	crashBroadcast <- map[string]interface{}{
		"type":    "active_bettors",
		"bettors": list,
		"count":   len(list),
	}
}