package ws

import (
	"log"
	"math"
	"net/http"
	"sync/atomic"
	"time"

	"goLangServer/crypto"
	"goLangServer/game"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var clientCount int64

const (
	InitialGroupDurationMs = 5000 // 5 seconds candles
	MergeThreshold         = 30    // Merge when we have 30+ groups
)

func mergeGroups(groups []game.CandleGroup, currentDuration int64) ([]game.CandleGroup, int64) {
	newGroups := make([]game.CandleGroup, 0)

	// Merge pairs
	for i := 0; i < len(groups)-1; i += 2 {
		g1 := groups[i]
		g2 := groups[i+1]

		// Create a new slice for merged values to avoid shared references
		mergedValues := make([]float64, 0, len(g1.ValueList)+len(g2.ValueList))
		mergedValues = append(mergedValues, g1.ValueList...)
		mergedValues = append(mergedValues, g2.ValueList...)

		merged := game.CandleGroup{
			Open:       g1.Open,
			Close:      g2.Close,
			Max:        math.Max(g1.Max, g2.Max),
			Min:        math.Min(g1.Min, g2.Min),
			ValueList:  mergedValues,
			StartTime:  g1.StartTime,
			DurationMs: g1.DurationMs * 2,
			IsComplete: true,
		}
		newGroups = append(newGroups, merged)
	}

	// If odd number, add the last one with a deep copy
	if len(groups)%2 == 1 {
		lastGroup := groups[len(groups)-1]
		copiedValues := make([]float64, len(lastGroup.ValueList))
		copy(copiedValues, lastGroup.ValueList)

		lastGroupCopy := game.CandleGroup{
			Open:       lastGroup.Open,
			Close:      lastGroup.Close,
			Max:        lastGroup.Max,
			Min:        lastGroup.Min,
			ValueList:  copiedValues,
			StartTime:  lastGroup.StartTime,
			DurationMs: lastGroup.DurationMs,
			IsComplete: lastGroup.IsComplete,
		}
		newGroups = append(newGroups, lastGroupCopy)
	}

	// Double the duration for next groups
	newDuration := currentDuration * 2

	return newGroups, newDuration
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
			if rng.Float64() < game.RugProbBeforePeak {
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
