//game/candleflip.go
package game

import (
	"math"
	"math/rand"
)

const (
	CandleflipStartingPrice    = 1.0
	CandleflipTotalTicks       = 40
	CandleflipBigMoveChance    = 0.01
	CandleflipBigMovePct       = 0.20  // ±20% for big moves
	CandleflipSmallMovePctMin  = 0.01  // ±1% minimum
	CandleflipSmallMovePctMax  = 0.05  // ±5% maximum
)

// GenerateCandleflipPrice generates next price based on percentage changes (fair)
func GenerateCandleflipPrice(rng *rand.Rand, lastPrice float64) float64 {
	chance := rng.Float64()
	var percentChange float64

	if chance < CandleflipBigMoveChance {
		// 1% chance for big move (±20%)
		if rng.Float64() < 0.5 {
			percentChange = -CandleflipBigMovePct
		} else {
			percentChange = CandleflipBigMovePct
		}
	} else {
		// 99% chance for small move (±1% to ±5%)
		magnitude := CandleflipSmallMovePctMin + rng.Float64()*(CandleflipSmallMovePctMax-CandleflipSmallMovePctMin)
		if rng.Float64() < 0.5 {
			percentChange = -magnitude
		} else {
			percentChange = magnitude
		}
	}

	newPrice := lastPrice * (1 + percentChange)
	// Prevent negative prices
	if newPrice < 0 {
		newPrice = 0
	}
	return newPrice
}

// VerifyCandleflip runs the complete game simulation and returns the winner
func VerifyCandleflip(serverSeed string) string {
	combined := serverSeed + "-candleflip"
	rng := NewSeededRNG(combined)

	currentPrice := CandleflipStartingPrice

	// Run exactly 40 price updates
	for i := 0; i < CandleflipTotalTicks; i++ {
		currentPrice = GenerateCandleflipPrice(rng, currentPrice)
	}

	// RED wins if final price < 1.0, GREEN wins if >= 1.0
	if currentPrice < 1.0 {
		return "RED"
	}
	return "GREEN"
}

// SimulateCandleflipGame runs the game and returns price history and winner
func SimulateCandleflipGame(serverSeed string) ([]float64, string) {
	combined := serverSeed + "-candleflip"
	rng := NewSeededRNG(combined)

	priceHistory := make([]float64, CandleflipTotalTicks+1)
	priceHistory[0] = CandleflipStartingPrice
	currentPrice := CandleflipStartingPrice

	// Run exactly 40 price updates
	for i := 0; i < CandleflipTotalTicks; i++ {
		currentPrice = GenerateCandleflipPrice(rng, currentPrice)
		// Prevent price from going negative
		if currentPrice < 0 {
			currentPrice = 0
		}
		priceHistory[i+1] = currentPrice
	}

	// Determine winner
	winner := "GREEN"
	if currentPrice < 1.0 {
		winner = "RED"
	}

	return priceHistory, winner
}

// CalculateCandleflipPayout calculates payout for a bet
func CalculateCandleflipPayout(betAmount float64, trend string, winner string) float64 {
	// trend: "bullish" or "bearish"
	// winner: "GREEN" or "RED"

	won := false
	if (trend == "bullish" && winner == "GREEN") || (trend == "bearish" && winner == "RED") {
		won = true
	}

	if won {
		return betAmount * 2.0 // 2x payout
	}
	return 0
}

// RoundToDecimal rounds a float to specified decimal places
func RoundToDecimal(val float64, precision int) float64 {
	ratio := math.Pow(10, float64(precision))
	return math.Round(val*ratio) / ratio
}
