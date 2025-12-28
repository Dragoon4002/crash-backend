package main

import (
	"fmt"
	"goLangServer/crypto"
	"goLangServer/game"
)

func main_test() {
	fmt.Println("🎲 Running 100 Candleflip simulations...")
	fmt.Println("=" + string(make([]byte, 60)) + "=")

	greenWins := 0
	redWins := 0
	var finalPrices []float64
	var priceChanges []float64 // Track total price change (final - initial)

	for i := 0; i < 100; i++ {
		// Generate unique server seed for each game
		serverSeed, _ := crypto.GenerateServerSeed()

		// Run simulation
		priceHistory, winner := game.SimulateCandleflipGame(serverSeed)
		finalPrice := priceHistory[len(priceHistory)-1]
		finalPrices = append(finalPrices, finalPrice)
		priceChange := finalPrice - 1.0
		priceChanges = append(priceChanges, priceChange)

		if winner == "GREEN" {
			greenWins++
		} else {
			redWins++
		}

		// Print every 10th game
		if (i+1)%10 == 0 {
			fmt.Printf("Progress: %d/100 games | GREEN: %d | RED: %d\n", i+1, greenWins, redWins)
		}
	}

	// Calculate statistics
	var sumPrices float64
	var sumChanges float64
	minPrice := finalPrices[0]
	maxPrice := finalPrices[0]

	for i, price := range finalPrices {
		sumPrices += price
		sumChanges += priceChanges[i]
		if price < minPrice {
			minPrice = price
		}
		if price > maxPrice {
			maxPrice = price
		}
	}

	avgFinalPrice := sumPrices / float64(len(finalPrices))
	avgPriceChange := sumChanges / float64(len(priceChanges))

	// Print results
	fmt.Println("\n" + "=" + string(make([]byte, 60)) + "=")
	fmt.Println("📊 RESULTS AFTER 100 GAMES")
	fmt.Println("=" + string(make([]byte, 60)) + "=")
	fmt.Printf("\n🟢 GREEN wins (price >= 1.0): %d (%.1f%%)\n", greenWins, float64(greenWins))
	fmt.Printf("🔴 RED wins (price < 1.0):    %d (%.1f%%)\n", redWins, float64(redWins))
	fmt.Printf("\n📈 Price Statistics:\n")
	fmt.Printf("   Average Final Price: %.3f\n", avgFinalPrice)
	fmt.Printf("   Average Price Change: %.3f (%.1f%%)\n", avgPriceChange, avgPriceChange*100)
	fmt.Printf("   Min Final Price: %.3f\n", minPrice)
	fmt.Printf("   Max Final Price: %.3f\n", maxPrice)

	// Analyze bias
	fmt.Println("\n🔍 BIAS ANALYSIS:")
	if avgFinalPrice > 1.0 {
		bias := (avgFinalPrice - 1.0) * 100
		fmt.Printf("   ⚠️  UPWARD BIAS detected: +%.2f%% above starting price\n", bias)
		fmt.Printf("   This means GREEN (bullish) has an unfair advantage!\n")
	} else if avgFinalPrice < 1.0 {
		bias := (1.0 - avgFinalPrice) * 100
		fmt.Printf("   ⚠️  DOWNWARD BIAS detected: -%.2f%% below starting price\n", bias)
		fmt.Printf("   This means RED (bearish) has an unfair advantage!\n")
	} else {
		fmt.Printf("   ✅ NO BIAS - Game is fair! Average price: %.3f\n", avgFinalPrice)
	}

	// Expected distribution
	fmt.Println("\n📐 EXPECTED vs ACTUAL:")
	fmt.Printf("   Expected: ~50%% GREEN / ~50%% RED (for fair game)\n")
	fmt.Printf("   Actual:   %.0f%% GREEN / %.0f%% RED\n", float64(greenWins), float64(redWins))

	deviation := float64(greenWins) - 50.0
	if deviation > 10 || deviation < -10 {
		fmt.Printf("   ⚠️  SIGNIFICANT DEVIATION: %.0f%% from expected\n", deviation)
	} else {
		fmt.Printf("   ✅ Within acceptable range (±10%%)\n")
	}

	// Distribution breakdown
	fmt.Println("\n📊 PRICE DISTRIBUTION:")
	ranges := map[string]int{
		"< 0.5":       0,
		"0.5 - 0.9":   0,
		"0.9 - 1.0":   0,
		"1.0 - 1.1":   0,
		"1.1 - 1.5":   0,
		"1.5 - 2.0":   0,
		"> 2.0":       0,
	}

	for _, price := range finalPrices {
		if price < 0.5 {
			ranges["< 0.5"]++
		} else if price < 0.9 {
			ranges["0.5 - 0.9"]++
		} else if price < 1.0 {
			ranges["0.9 - 1.0"]++
		} else if price < 1.1 {
			ranges["1.0 - 1.1"]++
		} else if price < 1.5 {
			ranges["1.1 - 1.5"]++
		} else if price < 2.0 {
			ranges["1.5 - 2.0"]++
		} else {
			ranges["> 2.0"]++
		}
	}

	fmt.Printf("   < 0.5:     %d games\n", ranges["< 0.5"])
	fmt.Printf("   0.5-0.9:   %d games\n", ranges["0.5 - 0.9"])
	fmt.Printf("   0.9-1.0:   %d games (RED wins)\n", ranges["0.9 - 1.0"])
	fmt.Printf("   1.0-1.1:   %d games (GREEN wins)\n", ranges["1.0 - 1.1"])
	fmt.Printf("   1.1-1.5:   %d games (GREEN wins)\n", ranges["1.1 - 1.5"])
	fmt.Printf("   1.5-2.0:   %d games (GREEN wins)\n", ranges["1.5 - 2.0"])
	fmt.Printf("   > 2.0:     %d games (GREEN wins)\n", ranges["> 2.0"])

	fmt.Println("\n" + "=" + string(make([]byte, 60)) + "=")
}
