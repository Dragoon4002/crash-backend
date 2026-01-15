package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"goLangServer/db"
)

func main() {
	// Load env
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: .env not found")
	}

	if os.Getenv("DATABASE_URL") == "" {
		log.Fatal("DATABASE_URL not set")
	}

	// Init postgres
	if err := db.InitPostgres(); err != nil {
		log.Fatalf("Failed to init postgres: %v", err)
	}
	defer db.ClosePostgres()

	ctx := context.Background()

	// Test wallets with various PnL
	testWallets := []struct {
		addr   string
		amount float64
	}{
		{"0x1234567890123456789012345678901234567890", 250.75},
		{"0xABCDEF0123456789ABCDEF0123456789ABCDEF01", 185.50},
		{"0x9876543210987654321098765432109876543210", 120.25},
		{"0xDEADBEEF00000000000000000000000DEADBEEF", 95.00},
		{"0xCAFEBABE00000000000000000000000CAFEBABE", 67.50},
		{"0xFEEDFACE00000000000000000000000FEEDFACE", 45.25},
		{"0xBAADF00D00000000000000000000000BAADF00D", 32.00},
		{"0x8BADF00D00000000000000000000000000000000", 18.75},
		{"0xDEFEC8ED00000000000000000000000000000000", -5.50},
		{"0xB16B00B500000000000000000000000000000000", -25.00},
	}

	fmt.Println("Seeding leaderboard with test data...")

	for _, w := range testWallets {
		// Delete existing
		db.PostgresPool.Exec(ctx, "DELETE FROM wallet_pnl WHERE wallet_address = $1", w.addr)

		// Insert with amount
		_, err := db.PostgresPool.Exec(ctx,
			"INSERT INTO wallet_pnl (wallet_address, amount) VALUES ($1, $2)",
			w.addr, w.amount)
		if err != nil {
			log.Printf("Failed to insert %s: %v", w.addr[:10], err)
		} else {
			fmt.Printf("  %s... -> %.2f\n", w.addr[:10], w.amount)
		}
	}

	fmt.Println("\nDone! Testing leaderboard...")

	// Verify
	records, err := db.GetWalletPnLLeaderboard(ctx, 20)
	if err != nil {
		log.Fatalf("Failed to get leaderboard: %v", err)
	}

	fmt.Printf("\nLeaderboard (%d entries):\n", len(records))
	for _, r := range records {
		fmt.Printf("  #%d %s... %.2f\n", r.Rank, r.WalletAddress[:10], r.Amount)
	}
}
