package db

import (
	"context"
	"os"
	"testing"

	"github.com/joho/godotenv"
)

func TestWalletPnL(t *testing.T) {
	// Load env
	if err := godotenv.Load("../.env"); err != nil {
		t.Fatalf("Failed to load .env: %v", err)
	}

	// Check DATABASE_URL
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set")
	}

	// Init postgres
	if err := InitPostgres(); err != nil {
		t.Fatalf("Failed to init postgres: %v", err)
	}
	defer ClosePostgres()

	ctx := context.Background()
	testWallet := "0xTestWallet123456789012345678901234567890"

	// Cleanup before test
	_, _ = PostgresPool.Exec(ctx, "DELETE FROM wallet_pnl WHERE wallet_address = $1", testWallet)

	// Test 1: SubtractWalletPnL creates new wallet with negative amount
	t.Run("SubtractWalletPnL_NewWallet", func(t *testing.T) {
		err := SubtractWalletPnL(ctx, testWallet, 10.0)
		if err != nil {
			t.Fatalf("SubtractWalletPnL failed: %v", err)
		}

		// Verify
		record, err := GetWalletPnLRank(ctx, testWallet)
		if err != nil {
			t.Fatalf("GetWalletPnLRank failed: %v", err)
		}
		if record == nil {
			t.Fatal("Expected record, got nil")
		}
		if record.Amount != -10.0 {
			t.Errorf("Expected amount -10.0, got %f", record.Amount)
		}
		t.Logf("After subtract 10: amount = %f", record.Amount)
	})

	// Test 2: AddWalletPnL adds to existing wallet
	t.Run("AddWalletPnL_ExistingWallet", func(t *testing.T) {
		err := AddWalletPnL(ctx, testWallet, 25.0)
		if err != nil {
			t.Fatalf("AddWalletPnL failed: %v", err)
		}

		// Verify: -10 + 25 = 15
		record, err := GetWalletPnLRank(ctx, testWallet)
		if err != nil {
			t.Fatalf("GetWalletPnLRank failed: %v", err)
		}
		if record.Amount != 15.0 {
			t.Errorf("Expected amount 15.0, got %f", record.Amount)
		}
		t.Logf("After add 25: amount = %f", record.Amount)
	})

	// Test 3: Multiple operations
	t.Run("MultipleOperations", func(t *testing.T) {
		// Subtract 5 (15 - 5 = 10)
		SubtractWalletPnL(ctx, testWallet, 5.0)
		// Add 20 (10 + 20 = 30)
		AddWalletPnL(ctx, testWallet, 20.0)

		record, _ := GetWalletPnLRank(ctx, testWallet)
		if record.Amount != 30.0 {
			t.Errorf("Expected amount 30.0, got %f", record.Amount)
		}
		t.Logf("After -5 +20: amount = %f", record.Amount)
	})

	// Test 4: GetWalletPnLLeaderboard
	t.Run("GetWalletPnLLeaderboard", func(t *testing.T) {
		// Insert more test wallets
		testWallets := []struct {
			addr   string
			amount float64
		}{
			{"0xTestLeader1_1111111111111111111111111111", 100.0},
			{"0xTestLeader2_2222222222222222222222222222", 50.0},
			{"0xTestLeader3_3333333333333333333333333333", 25.0},
		}

		for _, w := range testWallets {
			_, _ = PostgresPool.Exec(ctx, "DELETE FROM wallet_pnl WHERE wallet_address = $1", w.addr)
			_, _ = PostgresPool.Exec(ctx, "INSERT INTO wallet_pnl (wallet_address, amount) VALUES ($1, $2)", w.addr, w.amount)
		}

		// Get leaderboard
		records, err := GetWalletPnLLeaderboard(ctx, 10)
		if err != nil {
			t.Fatalf("GetWalletPnLLeaderboard failed: %v", err)
		}

		if len(records) < 3 {
			t.Errorf("Expected at least 3 records, got %d", len(records))
		}

		// Verify sorted by amount DESC
		t.Logf("Leaderboard (%d entries):", len(records))
		for i, r := range records {
			t.Logf("  %d. %s: %.2f (rank %d)", i+1, r.WalletAddress[:20]+"...", r.Amount, r.Rank)
		}

		// First should be highest
		if records[0].Amount < records[len(records)-1].Amount {
			t.Error("Leaderboard not sorted DESC by amount")
		}

		// Cleanup test wallets
		for _, w := range testWallets {
			PostgresPool.Exec(ctx, "DELETE FROM wallet_pnl WHERE wallet_address = $1", w.addr)
		}
	})

	// Cleanup
	PostgresPool.Exec(ctx, "DELETE FROM wallet_pnl WHERE wallet_address = $1", testWallet)
	t.Log("Test cleanup complete")
}
