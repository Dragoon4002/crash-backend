package game

import (
	"math"
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

	price := StartingPrice
	peak := price
	tick := 0
	rugged := false
	momentum := 0.0 // Track price momentum

	for tick < MaxTicks {
		// Dynamic rug probability - lower when building momentum, higher when declining
		// This creates periods of growth before potential rugs
		rugProb := RugProb
		if momentum > 0.05 {
			rugProb *= 0.05 // Reduce rug chance during upward momentum
		} else if momentum < -0.02 {
			rugProb *= 0.2 // Increase rug chance during downward momentum
		}

		// Rug check with dynamic probability
		if tick > 50 && rng.Float64() < rugProb { // Don't rug too early
			rugged = true
			break
		}

		// God candle (v3)
		if rng.Float64() < GodCandleChance && price <= 100 {
			price *= GodCandleMult
			if price > peak {
				peak = price
			}
			tick++
			continue
		}

		var change float64

		// Big move
		if rng.Float64() < BigMoveChance {
			move := BigMoveMin + rng.Float64()*(BigMoveMax-BigMoveMin)
			if rng.Float64() > 0.5 {
				change = move
			} else {
				change = -move
			}
		} else {
			// Normal drift with increased volatility
			drift := DriftMin + rng.Float64()*(DriftMax-DriftMin)
			// Increased base volatility from 0.005 to 0.015 for more variation
			volatility := 0.015 * math.Min(15, math.Sqrt(price))
			noise := volatility * (2*rng.Float64() - 1)
			change = drift + noise
		}

		price = price * (1 + change)
		if price < 0 {
			price = 0
		}

		if price > peak {
			peak = price
		}

		// Update momentum (exponential moving average of change)
		momentum = momentum*0.9 + change*0.1

		tick++
	}

	return GameResult{
		PeakMultiplier: peak,
		FinalPrice:     price,
		Rugged:         rugged,
		TotalTicks:     tick,
	}
}
