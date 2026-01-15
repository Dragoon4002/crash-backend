package ws

import (
	"context"
	"log"
	"math"
	"math/big"
	"sync"
	"time"

	"goLangServer/crypto"
	"goLangServer/db"
	"goLangServer/game"
)

// CrashGameHistory stores info about past crash games
type CrashGameHistory struct {
	GameID         string             `json:"gameId"`
	PeakMultiplier float64            `json:"peakMultiplier"`
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

const MaxGameHistory = 15

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
	Status         string // "countdown", "running", "crashed"
	ContractGameID *big.Int
}

// StartCrashGameLoop starts the crash game loop - call after DB init
func StartCrashGameLoop() {
	go runCrashGameLoop()
}

// LoadCrashHistoryFromDB loads recent crash history from database into memory
func LoadCrashHistoryFromDB() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	records, err := db.GetRecentCrashHistory(ctx, MaxGameHistory)
	if err != nil {
		log.Printf("⚠️  Failed to load crash history from DB: %v", err)
		return
	}

	if len(records) == 0 {
		log.Println("📭 No crash history found in DB")
		return
	}

	gameHistoryMutex.Lock()
	defer gameHistoryMutex.Unlock()

	// Convert DB records to in-memory format (reverse to get oldest first)
	for i := len(records) - 1; i >= 0; i-- {
		r := records[i]
		crashGameHistory = append(crashGameHistory, CrashGameHistory{
			GameID:         r.GameID,
			PeakMultiplier: r.Peak,
			Candles:        r.CandlestickHistory,
			Timestamp:      r.CreatedAt,
		})
	}

	log.Printf("📥 Loaded %d crash games from DB", len(crashGameHistory))
}

func runCrashGameLoop() {
	log.Println("🎰 Crash game loop started")

	for {
		serverSeed, _ := crypto.GenerateServerSeed()
		gameID := time.Now().Format("20060102-150405.000")

		// Convert gameID to big.Int for contract (use Unix timestamp)
		timestamp := time.Now().Unix()
		contractGameID := big.NewInt(timestamp)

		currentCrashGameMutex.Lock()
		currentCrashGame = &CrashGameState{
			GameID:         gameID,
			ServerSeed:     serverSeed,
			Status:         "countdown",
			ContractGameID: contractGameID,
		}
		currentCrashGameMutex.Unlock()

		// Broadcast game start (NO server seed hash - not needed)
		crashBroadcast <- map[string]interface{}{
			"type": "game_start",
			"data": map[string]interface{}{
				"gameId":        contractGameID.String(),
				"startingPrice": 1.0,
			},
		}

		// Countdown: 5.00 seconds down to 0.00
		countdownDuration := 5.0 // seconds
		countdownTick := 0.01    // 10ms per tick
		for countdown := countdownDuration; countdown >= 0; countdown -= countdownTick {
			crashBroadcast <- map[string]interface{}{
				"type": "countdown",
				"data": map[string]interface{}{
					"countdown": math.Max(0, countdown), // Ensure it doesn't go negative
				},
			}
			time.Sleep(10 * time.Millisecond)
		}

		// Send final 0.00 countdown
		crashBroadcast <- map[string]interface{}{
			"type": "countdown",
			"data": map[string]interface{}{
				"countdown": 0.0,
			},
		}

		// Update status to running
		currentCrashGameMutex.Lock()
		currentCrashGame.Status = "running"
		currentCrashGameMutex.Unlock()

		// Calculate game result using provably fair engine
		gameResult := game.CalculateGame(serverSeed, gameID)

		// Run live simulation for real-time broadcast
		combined := serverSeed + "-" + gameID
		rng := game.NewSeededRNG(combined)

		price := 1.0
		tick := 0
		reachedPredeterminedPeak := false

		// Candle grouping state
		var groups []game.CandleGroup
		var currentGroup *game.CandleGroup
		groupDuration := int64(InitialGroupDurationMs)
		groupStartTime := time.Now().UnixMilli()

		// Run until rug (same logic as CalculateGame)
		for tick < 10000 {
			// Determine rug probability
			rugProb := game.RugProbBeforePeak
			if reachedPredeterminedPeak {
				rugProb = game.RugProbAfterPeak
			}

			// Check for rug
			if tick > 20 && rng.Float64() < rugProb {
				break
			}

			// Check if reached predetermined peak
			if price >= gameResult.PeakMultiplier {
				reachedPredeterminedPeak = true
			}

			// God candle (with directional bias)
			if rng.Float64() < game.GodCandleChance && price <= 100 {
				// Before peak: 60% chance down, 40% chance up
				// After peak: 40% chance down, 60% chance up
				var godCandleChange float64
				directionThreshold := 0.6 // 60% threshold
				if reachedPredeterminedPeak {
					directionThreshold = 0.4 // 40% threshold (reversed)
				}

				if rng.Float64() < directionThreshold {
					// Down movement
					godCandleChange = -game.GodCandleMult
				} else {
					// Up movement
					godCandleChange = game.GodCandleMult
				}

				if godCandleChange > 0 {
					price *= godCandleChange
				} else {
					price /= -godCandleChange
				}
			} else {
				var change float64

				// Big move with directional bias
				if rng.Float64() < game.BigMoveChance {
					move := game.BigMoveMin + rng.Float64()*(game.BigMoveMax-game.BigMoveMin)

					// Before peak: 60% chance up, 40% chance down
					// After peak: 40% chance up, 60% chance down
					upwardThreshold := 0.6 // 60% chance upward before peak
					if reachedPredeterminedPeak {
						upwardThreshold = 0.4 // 40% chance upward after peak
					}

					if rng.Float64() < upwardThreshold {
						change = move // Upward
					} else {
						change = -move // Downward
					}
				} else {
					// Normal drift
					drift := game.DriftMin + rng.Float64()*(game.DriftMax-game.DriftMin)
					volatility := 0.015 * math.Min(15, math.Sqrt(price))
					noise := volatility * (2*rng.Float64() - 1)
					change = drift + noise
				}

				price = price * (1 + change)
				if price < 0.5 {
					price = 0.5
				}
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
					"gameId":          contractGameID.String(),
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
			finalCloseValue := *currentGroup.Close
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

		// Broadcast game end (with server seed for verification)
		crashBroadcast <- map[string]interface{}{
			"type": "game_end",
			"data": map[string]interface{}{
				"gameId":          contractGameID.String(),
				"serverSeed":      serverSeed,
				"peakMultiplier":  gameResult.PeakMultiplier,
				"finalPrice":      price,
				"totalTicks":      tick,
				"previousCandles": groups,
			},
		}

		// Add to history
		now := time.Now()
		gameHistoryMutex.Lock()
		crashGameHistory = append(crashGameHistory, CrashGameHistory{
			GameID:         gameID,
			PeakMultiplier: gameResult.PeakMultiplier,
			Candles:        groups,
			Timestamp:      now,
		})
		// Keep only last MaxGameHistory (15) games
		if len(crashGameHistory) > MaxGameHistory {
			crashGameHistory = crashGameHistory[len(crashGameHistory)-MaxGameHistory:]
		}
		gameHistoryMutex.Unlock()

		// Store to database
		go func(gid, seed string, peak float64, candles []game.CandleGroup, ts time.Time) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			err := db.StoreCrashHistory(ctx, &db.CrashHistoryRecord{
				GameID:             gid,
				ServerSeed:         seed,
				Peak:               peak,
				CandlestickHistory: candles,
				CreatedAt:          ts,
			})
			if err != nil {
				log.Printf("⚠️  Failed to store crash history: %v", err)
			}
		}(gameID, serverSeed, gameResult.PeakMultiplier, groups, now)

		// Broadcast updated history to all crash subscribers
		broadcastCrashHistory()

		log.Printf("🎲 Crash game %s finished - Peak: %.2fx, Final: %.2fx", gameID, gameResult.PeakMultiplier, price)

		// Mark all remaining active bets as lost in database
		go func(gid string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			err := db.MarkBetsAsLost(ctx, gid)
			if err != nil {
				log.Printf("⚠️  Failed to mark bets as lost: %v", err)
			}
		}(contractGameID.String())

		// Clear all active bettors for next game
		ClearActiveBettors()

		// Wait 10 seconds before next game
		time.Sleep(10 * time.Second)
	}
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

// RemoveActiveBettor removes a bettor from the active list (when they cash out)
func RemoveActiveBettor(address string) {
	activeBettorsMutex.Lock()
	defer activeBettorsMutex.Unlock()

	if _, exists := activeBettors[address]; exists {
		delete(activeBettors, address)
		log.Printf("➖ Bettor removed: %s", address)
		broadcastActiveBettors()
	}
}

// ClearActiveBettors removes all bettors (called when game ends)
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

// GetCrashHistory returns a copy of the crash game history
func GetCrashHistory() []CrashGameHistory {
	gameHistoryMutex.RLock()
	defer gameHistoryMutex.RUnlock()

	// Create copy to avoid concurrent modification
	history := make([]CrashGameHistory, len(crashGameHistory))
	copy(history, crashGameHistory)
	return history
}

// broadcastCrashHistory sends crash history to all crash subscribers
func broadcastCrashHistory() {
	gameHistoryMutex.RLock()
	history := make([]CrashGameHistory, len(crashGameHistory))
	copy(history, crashGameHistory)
	gameHistoryMutex.RUnlock()

	crashBroadcast <- map[string]interface{}{
		"type":    "crash_history",
		"history": history,
	}
}

