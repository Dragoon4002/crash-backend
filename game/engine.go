package game

import (
	"math"
	"math/rand"
)

const (
	StartingPrice   = 1.0
	MaxTicks        = 5000
	RugProb         = 0.01  // 1% chance to rug (increased from 0.5%)
	GodCandleChance = 0.002 // Increased god candle chance for high peaks
	GodCandleMult   = 2.5   // Moderate boost (not too extreme)
	BigMoveChance   = 0.12  // 12% chance of big moves (increased from 5%)
	BigMoveMin      = 0.08  // Minimum big move: 8%
	BigMoveMax      = 0.50  // Maximum big move: 50% (increased variation)
	DriftMin        = -0.04 // More negative drift (larger downward swings)
	DriftMax        = 0.04  // More positive drift (larger upward swings)
)

func CalculateGame(serverSeed, gameID string) GameResult {
	combined := serverSeed + "-" + gameID
	rng := NewSeededRNG(combined)

	// Determine target peak upfront
	targetPeak := determineTargetPeak(rng)

	price := StartingPrice
	tick := 0
	rugged := false
	peakReached := (StartingPrice >= targetPeak) // Handle peak=1.0 case

	// Phase 1: Growth to peak (if peak > 1.0)
	if !peakReached {
		for tick < MaxTicks && !peakReached {
			// Only upward or neutral movements during growth
			var change float64

			// God candle during growth phase
			if rng.Float64() < GodCandleChance && price <= 100 {
				change = GodCandleMult - 1.0 // Convert multiplier to change
			} else if rng.Float64() < BigMoveChance {
				// Big upward move
				move := BigMoveMin + rng.Float64()*(BigMoveMax-BigMoveMin)
				change = move // Only positive during growth
			} else {
				// Normal upward drift
				drift := rng.Float64() * DriftMax // 0 to DriftMax (upward)
				volatility := 0.015 * math.Min(15, math.Sqrt(price))
				noise := volatility * rng.Float64() // 0 to volatility (positive bias)
				change = drift + noise
			}

			price = price * (1 + change)

			// Check if peak reached
			if price >= targetPeak {
				price = targetPeak
				peakReached = true
			}

			tick++
		}
	}

	// Phase 2: Post-peak decline and potential rug
	// Price can now move randomly but must stay <= peak
	for tick < MaxTicks {
		// Rug check - can happen any time after peak is reached
		if rng.Float64() < RugProb {
			rugged = true
			break
		}

		// Price movement (can go up or down, but constrained to <= peak)
		var change float64

		if rng.Float64() < BigMoveChance {
			move := BigMoveMin + rng.Float64()*(BigMoveMax-BigMoveMin)
			if rng.Float64() > 0.5 {
				change = move
			} else {
				change = -move
			}
		} else {
			// Normal drift
			drift := DriftMin + rng.Float64()*(DriftMax-DriftMin)
			volatility := 0.015 * math.Min(15, math.Sqrt(price))
			noise := volatility * (2*rng.Float64() - 1)
			change = drift + noise
		}

		price = price * (1 + change)

		// Enforce constraints
		if price < 0 {
			price = 0
		}
		if price > targetPeak {
			price = targetPeak // Hard cap at peak
		}

		tick++
	}

	return GameResult{
		PeakMultiplier: targetPeak,
		FinalPrice:     price,
		Rugged:         rugged,
		TotalTicks:     tick,
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
