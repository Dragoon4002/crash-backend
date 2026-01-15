package game

import (
	"math"
	"math/rand"
)

const (
	StartingPrice   = 1.0
	GodCandleChance = 0.002 // 0.2% chance for god candle
	GodCandleMult   = 2.5   // 2.5x boost on god candle
	BigMoveChance   = 0.12  // 12% chance of big moves
	BigMoveMin      = 0.08  // Minimum big move: 8%
	BigMoveMax      = 0.50  // Maximum big move: 50%
	DriftMin        = -0.04 // Minimum drift -4%
	DriftMax        = 0.04  // Maximum drift +4%

	// Rug probabilities
	RugProbBeforePeak = 0.0001 // 0.01% rug chance before peak (reduced by 100x)
	RugProbAfterPeak  = 0.10   // 10% rug chance after peak

	// Peak distribution probabilities (heavily favor 1-3x range)
	PeakVeryLow  = 0.50 // 50% chance: 1.0x - 1.5x
	PeakLow      = 0.85 // 35% chance: 1.5x - 3.0x (cumulative)
	PeakMedium   = 0.95 // 10% chance: 3.0x - 10.0x (cumulative)
	PeakHigh     = 0.99 // 4% chance: 10.0x - 50.0x (cumulative)
	PeakExtreme  = 1.00 // 1% chance: 50.0x - 200.0x (cumulative)

	PeakMin         = 1.0   // Minimum peak
	PeakVeryLowMax  = 1.5
	PeakLowMax      = 3.0
	PeakMediumMax   = 10.0
	PeakHighMax     = 50.0
	PeakExtremeMax  = 200.0
)

// GeneratePeakMultiplier generates a provably fair peak multiplier from seed
// Distribution heavily favors 1.0x - 3.0x range (85% probability)
func GeneratePeakMultiplier(serverSeed, gameID string) float64 {
	combined := serverSeed + "-" + gameID + "-peak"
	rng := NewSeededRNG(combined)

	// Generate random value [0, 1)
	randValue := rng.Float64()

	// Determine peak based on distribution
	var peak float64
	if randValue < PeakVeryLow {
		// 50% chance: 1.0x - 1.5x
		normalized := randValue / PeakVeryLow
		peak = PeakMin + normalized*(PeakVeryLowMax-PeakMin)
	} else if randValue < PeakLow {
		// 35% chance: 1.5x - 3.0x
		normalized := (randValue - PeakVeryLow) / (PeakLow - PeakVeryLow)
		peak = PeakVeryLowMax + normalized*(PeakLowMax-PeakVeryLowMax)
	} else if randValue < PeakMedium {
		// 10% chance: 3.0x - 10.0x
		normalized := (randValue - PeakLow) / (PeakMedium - PeakLow)
		peak = PeakLowMax + normalized*(PeakMediumMax-PeakLowMax)
	} else if randValue < PeakHigh {
		// 4% chance: 10.0x - 50.0x
		normalized := (randValue - PeakMedium) / (PeakHigh - PeakMedium)
		peak = PeakMediumMax + normalized*(PeakHighMax-PeakMediumMax)
	} else {
		// 1% chance: 50.0x - 200.0x
		normalized := (randValue - PeakHigh) / (PeakExtreme - PeakHigh)
		peak = PeakHighMax + normalized*(PeakExtremeMax-PeakHighMax)
	}

	// Ensure minimum peak is 1.0
	if peak < PeakMin {
		peak = PeakMin
	}

	return peak
}

// CalculateGame simulates the crash game with predetermined peak
// Game continues past peak with increased rug probability (1% → 10%)
func CalculateGame(serverSeed, gameID string) GameResult {
	// First, generate the provable peak multiplier
	peakMultiplier := GeneratePeakMultiplier(serverSeed, gameID)

	// Now simulate price movement
	combined := serverSeed + "-" + gameID
	rng := NewSeededRNG(combined)

	price := StartingPrice
	peak := price // Track actual peak reached
	tick := 0
	momentum := 0.0
	reachedPredeterminedPeak := false

	// Run until rug occurs
	for tick < 10000 { // Safety limit
		// Determine rug probability based on whether we've passed predetermined peak
		rugProb := RugProbBeforePeak
		if reachedPredeterminedPeak {
			rugProb = RugProbAfterPeak // 10x higher chance after peak
		}

		// Check for rug (skip first 20 ticks to build momentum)
		if tick > 20 && rng.Float64() < rugProb {
			// Game rugs here
			break
		}

		// Check if we've reached the predetermined peak
		if price >= peakMultiplier {
			reachedPredeterminedPeak = true
		}

		// God candle (rare big jumps with directional bias)
		if rng.Float64() < GodCandleChance && price <= 100 {
			// Before peak: 60% chance down, 40% chance up
			// After peak: 40% chance down, 60% chance up
			var godCandleChange float64
			directionThreshold := 0.6 // 60% threshold
			if reachedPredeterminedPeak {
				directionThreshold = 0.4 // 40% threshold (reversed)
			}

			if rng.Float64() < directionThreshold {
				// Down movement
				godCandleChange = -GodCandleMult
			} else {
				// Up movement
				godCandleChange = GodCandleMult
			}

			if godCandleChange > 0 {
				price *= godCandleChange
			} else {
				price /= -godCandleChange
			}

			if price > peak {
				peak = price
			}
			tick++
			continue
		}

		var change float64

		// Big move with directional bias
		if rng.Float64() < BigMoveChance {
			move := BigMoveMin + rng.Float64()*(BigMoveMax-BigMoveMin)

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
			// Normal drift with volatility
			drift := DriftMin + rng.Float64()*(DriftMax-DriftMin)
			volatility := 0.015 * math.Min(15, math.Sqrt(price))
			noise := volatility * (2*rng.Float64() - 1)
			change = drift + noise
		}

		price = price * (1 + change)

		// Don't let price go too low
		if price < 0.5 {
			price = 0.5
		}

		// Track peak
		if price > peak {
			peak = price
		}

		// Update momentum
		momentum = momentum*0.9 + change*0.1

		tick++
	}

	return GameResult{
		PeakMultiplier: peakMultiplier, // Predetermined peak for provability
		FinalPrice:     price,          // Final price when rugged
		TotalTicks:     tick,
		ServerSeed:     serverSeed,
		GameID:         gameID,
	}
}

// determineTargetPeak generates a random peak value using weighted distribution
func determineTargetPeak(rng *rand.Rand) float64 {
	// Generate peak distribution:
	// - Higher chance of low peaks (1.0x - 2.0x)
	// - Lower chance of high peaks (2.0x - 100x+)

	r := rng.Float64()

	if r < 0.40 { // 40% chance: very low peaks (1.0x - 1.5x)
		return 1.0 + rng.Float64()*0.5
	} else if r < 0.70 { // 30% chance: low peaks (1.5x - 3.0x)
		return 1.5 + rng.Float64()*1.5
	} else if r < 0.88 { // 18% chance: medium peaks (3.0x - 10.0x)
		return 3.0 + rng.Float64()*7.0
	} else if r < 0.97 { // 9% chance: high peaks (10.0x - 50.0x)
		return 10.0 + rng.Float64()*40.0
	} else { // 3% chance: extreme peaks (50.0x - 200.0x)
		return 50.0 + rng.Float64()*150.0
	}
}
