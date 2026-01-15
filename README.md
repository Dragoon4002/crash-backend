# CRAZD Backend

Go WebSocket server for the crash game and candleflip betting platform.

## Tech Stack

- **Language**: Go 1.24
- **WebSocket**: gorilla/websocket
- **Database**: PostgreSQL (pgx/v5)
- **Cache**: Redis
- **Blockchain**: go-ethereum (contract interactions)

## Endpoints

### WebSocket

| Endpoint | Description |
|----------|-------------|
| `/ws` | Unified WebSocket - crash game, chat, rooms |
| `/candleflip` | Candleflip game WebSocket |

**Subscriptions via `/ws`:**
- `crash` - Crash game state + history
- `chat` - Server chat
- `rooms` - Global rooms list
- `candleflip:<roomId>` - Specific candleflip room

### REST API

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/crash` | Crash game history (last 50) |
| GET | `/api/crash/:gameId` | Specific game details |
| GET | `/api/health` | Health check (Redis + PostgreSQL) |
| POST | `/api/bettor/add` | Add active bettor |
| POST | `/api/bettor/remove` | Remove active bettor |
| GET | `/api/bettor/list` | List active bettors |
| GET | `/api/leaderboard` | Get leaderboard |

## Project Structure

```
crash-backend/
├── api/          # REST API handlers
├── config/       # Configuration constants
├── contract/     # Ethereum contract interactions
├── crypto/       # Cryptographic utilities (seeds)
├── db/           # PostgreSQL + Redis clients
├── game/         # Game logic (crash, candleflip, PRNG)
├── state/        # State types
├── ws/           # WebSocket handlers
└── main.go       # Entry point
```

## Setup

### Prerequisites

- Go 1.24+
- PostgreSQL
- Redis

### Environment Variables

Create `.env`:

```env
DATABASE_URL=postgres://user:pass@localhost:5432/crazd
REDIS_URL=redis://localhost:6379
CONTRACT_ADDRESS=0x...
PRIVATE_KEY=...
RPC_URL=https://...
```

### Run

```bash
go run main.go
# or
./start.sh
```

Server starts on `0.0.0.0:8080`

## Game Logic

### Crash Game

- Provably fair using server seed + game ID
- Peak multiplier distribution: 85% in 1.0x-3.0x range
- Candle-based price movement with god candles, big moves, drift

### Candleflip

- Bull vs Bear betting
- Batch-based room system
- Contract-based payouts
