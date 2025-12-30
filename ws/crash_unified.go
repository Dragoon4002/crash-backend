package ws

import (
	"context"
	"log"
	"math"
	"math/big"
	"sync"
	"time"

	"goLangServer/contract"
	"goLangServer/crypto"
	"goLangServer/db"
	"goLangServer/game"
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

const MaxGameHistory = 10

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

		// Broadcast game end FIRST to prevent client from asking for more bets/sells
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

		// Store game result in PostgreSQL for provably fair verification
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
				log.Printf("⚠️  Failed to store crash history in PostgreSQL: %v", err)
			}
		}()

		// Call rugGame on contract if game rugged
		if rugged {
			go func() {
				contractClient, err := contract.NewGameHouseContract()
				if err != nil {
					log.Printf("⚠️  Failed to initialize contract client: %v", err)
					return
				}
				defer contractClient.Close()

				rugCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()

				txHash, err := contractClient.RugGame(rugCtx, contractGameID)
				if err != nil {
					log.Printf("⚠️  Failed to call rugGame on contract: %v", err)
				} else {
					log.Printf("✅ rugGame called on contract - TX: %s", txHash)
				}
			}()
		}

		// Clean up Redis for this game (remove all active bets)
		gameIDStr := contractGameID.String()
		go func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := db.CleanupCrashGame(cleanupCtx, gameIDStr); err != nil {
				log.Printf("⚠️  Failed to cleanup Redis: %v", err)
			}
		}()

		log.Printf("🎲 Crash game %s finished - Peak: %.2fx, Rugged: %v", gameID, peak, rugged)

		// Broadcast updated history to all crash subscribers
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
