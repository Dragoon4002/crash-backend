# Server Restart Instructions

## The Issue
The server is running old code that doesn't recognize `crash_bet_placed` and `crash_cashout` messages.Error:
```
2026/01/03 01:32:33 ⚠️  Unknown message type from client 1767383324-19: crash_bet_placed
2026/01/03 01:32:36 ⚠️  Unknown message type from client 1767383324-19: crash_cashout
```

## Solution
You need to rebuild and restart the server to load the updated code.

## Steps:

### 1. Stop the currently running server
Press `Ctrl+C` in the terminal where the server is running

### 2. Rebuild the server
```bash
cd /home/sounak/programming/silonelabs/rugs/v2/crash-backend
go build -o server .
```

### 3. Restart the server
```bash
./server
```

OR if you're using `go run`:
```bash
go run main.go
```

## What to Look For

After restart, when you place a bet or cashout, you should see detailed logs like:
```
🎯 handleCrashBetPlaced called - Processing bet placement
🎲 Crash bet placed - Player: 0x123..., Amount: 0.0010, Entry: 1.50x, GameID: 1234567890, TxHash: 0xabc...
```

And for cashout:
```
🎯 handleCrashCashout called - Data: map[...]
💰 Crash cashout request - Player: 0x123..., GameID: 1234567890, Cashout: 2.50x...
💳 Contract client available: true
💸 Calling payPlayer contract - Player: 0x123..., PayoutWei: 2500000000000000000
🔄 Executing contract.PayPlayer...
✅ payPlayer contract call successful for 0x123...
```

## If Contract Client Shows `false`:

If you see `💳 Contract client available: false`, check that:
1. `SERVER_PRIVATE_KEY` is set in `.env` file
2. The private key is valid (64 hex characters)
3. Server has access to Mantle Sepolia RPC

