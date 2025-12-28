package ws

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"sync"
	"time"

	"goLangServer/contract"
	"goLangServer/crypto"
	"goLangServer/game"

	"github.com/gorilla/websocket"
)

type CandleflipRoom struct {
	RoomID         string
	Connections    map[*websocket.Conn]bool
	mu             sync.RWMutex
	currentGameID  string
	serverSeed     string
	serverSeedHash string
	gameStarted    bool
}

var (
	candleflipRooms = make(map[string]*CandleflipRoom)
	roomsMutex      sync.RWMutex

	// Track contract game results to batch resolve calls
	contractGameResults = make(map[string]*ContractGameTracker)
	contractGameMutex   sync.RWMutex
)

type ContractGameTracker struct {
	GameID          string
	TotalRooms      int
	FinishedRooms   int
	RoomsWon        int
	mu              sync.Mutex
	resolveStarted  bool
}

func getCandleflipRoom(roomID string) *CandleflipRoom {
	roomsMutex.Lock()
	defer roomsMutex.Unlock()

	room, exists := candleflipRooms[roomID]
	if !exists {
		room = &CandleflipRoom{
			RoomID:      roomID,
			Connections: make(map[*websocket.Conn]bool),
			gameStarted: false,
		}
		candleflipRooms[roomID] = room
		log.Printf("🆕 Created new Candleflip WebSocket room: %s (awaiting game start)", roomID)
	} else {
		log.Printf("♻️  Client connecting to existing Candleflip room: %s", roomID)
	}
	return room
}

// StartCandleflipGame starts the game for a room (called from unified.go after room creation)
func StartCandleflipGame(roomID string) {
	roomsMutex.RLock()
	room, exists := candleflipRooms[roomID]
	roomsMutex.RUnlock()

	if !exists {
		// Room doesn't exist yet, create it
		roomsMutex.Lock()
		room = &CandleflipRoom{
			RoomID:      roomID,
			Connections: make(map[*websocket.Conn]bool),
			gameStarted: false,
		}
		candleflipRooms[roomID] = room
		roomsMutex.Unlock()
		log.Printf("🆕 Created Candleflip room %s (game starting)", roomID)
	}

	// Start game in goroutine
	go runCandleflipRoom(room)
}

func (r *CandleflipRoom) addConnection(conn *websocket.Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Connections[conn] = true

	// Update player count in global rooms
	UpdateRoomPlayers(r.RoomID, len(r.Connections))
}

func (r *CandleflipRoom) removeConnection(conn *websocket.Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.Connections, conn)

	// Update player count in global rooms
	UpdateRoomPlayers(r.RoomID, len(r.Connections))
}

func (r *CandleflipRoom) broadcast(message map[string]interface{}) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for conn := range r.Connections {
		if err := conn.WriteJSON(message); err != nil {
			log.Printf("❌ Failed to send to connection in room %s: %v", r.RoomID, err)
		}
	}
}

func runCandleflipRoom(room *CandleflipRoom) {
	log.Printf("🎮 Starting Candleflip game for room %s (Player vs Bot)", room.RoomID)

	// Check if game already started (prevent duplicate runs)
	room.mu.Lock()
	if room.gameStarted {
		room.mu.Unlock()
		log.Printf("⚠️  Game already started for room %s, skipping", room.RoomID)
		return
	}
	room.gameStarted = true
	room.mu.Unlock()

	// Update global room status to "running"
	UpdateRoomStatus(room.RoomID, "running")

	// Run only ONE game per room
	{
		// Generate new game
		serverSeed, seedHash := crypto.GenerateServerSeed()
		gameID := fmt.Sprintf("%s-%d", room.RoomID, time.Now().Unix())

		room.mu.Lock()
		room.currentGameID = gameID
		room.serverSeed = serverSeed
		room.serverSeedHash = seedHash
		room.mu.Unlock()

		// Get bot name and player sides from global rooms
		var botName, bearSide, bullSide, playerTrend string
		globalRoomsMutex.RLock()
		if globalRoom, exists := globalRooms[room.RoomID]; exists {
			botName = globalRoom.BotName
			bearSide = globalRoom.BearSide
			bullSide = globalRoom.BullSide
			playerTrend = globalRoom.Trend
		}
		globalRoomsMutex.RUnlock()

		// Broadcast game start with bot opponent info
		room.broadcast(map[string]interface{}{
			"type": "game_start",
			"data": map[string]interface{}{
				"roomId":         room.RoomID,
				"gameId":         gameID,
				"serverSeedHash": seedHash,
				"startingPrice":  game.CandleflipStartingPrice,
				"botName":        botName,
				"bearSide":       bearSide,
				"bullSide":       bullSide,
				"playerTrend":    playerTrend,
			},
		})

		// Small delay to let UI initialize
		time.Sleep(100 * time.Millisecond)

		// Run the game simulation
		combined := serverSeed + "-candleflip"
		rng := game.NewSeededRNG(combined)
		currentPrice := game.CandleflipStartingPrice
		priceHistory := []float64{currentPrice}

		// Send price updates tick by tick
		for tick := 0; tick < game.CandleflipTotalTicks; tick++ {
			currentPrice = game.GenerateCandleflipPrice(rng, currentPrice)
			if currentPrice < 0 {
				currentPrice = 0
			}
			priceHistory = append(priceHistory, currentPrice)

			room.broadcast(map[string]interface{}{
				"type": "price_update",
				"data": map[string]interface{}{
					"roomId": room.RoomID,
					"tick":   tick + 1,
					"price":  game.RoundToDecimal(currentPrice, 3),
					"status": "running",
				},
			})

			// 100ms per tick = 4 seconds total for 40 ticks
			time.Sleep(100 * time.Millisecond)
		}

		// Determine winner
		winner := "GREEN"
		playerWon := false
		if currentPrice < 1.0 {
			winner = "RED"
		}

		// Determine if player won based on trend
		if (playerTrend == "bullish" && winner == "GREEN") || (playerTrend == "bearish" && winner == "RED") {
			playerWon = true
		}

		// Broadcast game end
		room.broadcast(map[string]interface{}{
			"type": "game_end",
			"data": map[string]interface{}{
				"roomId":       room.RoomID,
				"gameId":       gameID,
				"serverSeed":   serverSeed,
				"finalPrice":   game.RoundToDecimal(currentPrice, 3),
				"winner":       winner,
				"priceHistory": priceHistory,
				"status":       "finished",
			},
		})

		log.Printf("🎲 Candleflip room %s - Game %s finished. Winner: %s, Final Price: %.3f, Player Won: %v",
			room.RoomID, gameID, winner, currentPrice, playerWon)

		// Update global room status to "finished"
		UpdateRoomStatus(room.RoomID, "finished")

		// Track contract game results for batched resolution
		globalRoomsMutex.RLock()
		var contractGameIDStr string
		var roomsCount int
		if globalRoom, exists := globalRooms[room.RoomID]; exists {
			contractGameIDStr = globalRoom.ContractGameID
			roomsCount = globalRoom.RoomsCount
		}
		globalRoomsMutex.RUnlock()

		if contractGameIDStr != "" && roomsCount > 0 {
			// Track this room's result
			contractGameMutex.Lock()
			tracker, exists := contractGameResults[contractGameIDStr]
			if !exists {
				tracker = &ContractGameTracker{
					GameID:     contractGameIDStr,
					TotalRooms: roomsCount,
				}
				contractGameResults[contractGameIDStr] = tracker
			}
			tracker.mu.Lock()
			tracker.FinishedRooms++
			if playerWon {
				tracker.RoomsWon++
			}
			isLastRoom := tracker.FinishedRooms == tracker.TotalRooms
			shouldResolve := isLastRoom && !tracker.resolveStarted
			if shouldResolve {
				tracker.resolveStarted = true
			}
			finalRoomsWon := tracker.RoomsWon
			tracker.mu.Unlock()
			contractGameMutex.Unlock()

			// Only the last room to finish calls the contract
			if shouldResolve {
				log.Printf("📊 All %d rooms finished for game %s. Total won: %d", roomsCount, contractGameIDStr, finalRoomsWon)
				go func() {
					contractClient, err := contract.NewGameHouseContract()
					if err != nil {
						log.Printf("⚠️  Failed to initialize contract client for CandleFlip: %v", err)
						return
					}
					defer contractClient.Close()

					// Parse contract game ID
					contractGameID := new(big.Int)
					contractGameID, ok := contractGameID.SetString(contractGameIDStr, 10)
					if !ok {
						log.Printf("⚠️  Failed to parse contract game ID: %s", contractGameIDStr)
						return
					}

					// Total rooms won
					totalRoomsWon := big.NewInt(int64(finalRoomsWon))

					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()

					txHash, err := contractClient.ResolveCandleFlip(ctx, contractGameID, totalRoomsWon)
					if err != nil {
						log.Printf("⚠️  Failed to call resolveCandleFlip on contract: %v", err)
					} else {
						log.Printf("✅ resolveCandleFlip called - GameID: %s, RoomsWon: %d/%d, TX: %s",
							contractGameIDStr, finalRoomsWon, roomsCount, txHash)
					}

					// Clean up tracker
					contractGameMutex.Lock()
					delete(contractGameResults, contractGameIDStr)
					contractGameMutex.Unlock()
				}()
			} else {
				log.Printf("📝 Room %s finished (%d/%d). Waiting for all rooms...", room.RoomID, tracker.FinishedRooms, tracker.TotalRooms)
			}
		}

		// Wait 5 seconds to let clients display the result
		log.Printf("⏳ Keeping room %s visible for 5 seconds to display results", room.RoomID)
		time.Sleep(5 * time.Second)

		// Room is finished after one game - no loop
		log.Printf("🏁 Candleflip room %s completed", room.RoomID)

		// Close all WebSocket connections gracefully before deleting room
		room.mu.Lock()
		for conn := range room.Connections {
			conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(1000, "Game finished"))
			conn.Close()
		}
		room.mu.Unlock()

		// Clean up room from map
		roomsMutex.Lock()
		delete(candleflipRooms, room.RoomID)
		roomsMutex.Unlock()

		// Remove from global rooms list
		RemoveRoom(room.RoomID)
		log.Printf("🗑️  Removed room %s from memory (after displaying results)", room.RoomID)
	}
}

func HandleCandleflipWS(w http.ResponseWriter, r *http.Request) {
	roomID := r.URL.Query().Get("roomId")
	if roomID == "" {
		roomID = "default"
	}

	log.Printf("📥 Candleflip WebSocket connection attempt for room %s from: %s", roomID, r.RemoteAddr)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("❌ WebSocket upgrade failed:", err)
		return
	}
	defer conn.Close()

	room := getCandleflipRoom(roomID)
	room.addConnection(conn)
	defer room.removeConnection(conn)

	log.Printf("✅ Client connected to Candleflip room %s", roomID)

	// Keep connection alive and listen for close
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			log.Printf("👋 Client disconnected from Candleflip room %s", roomID)
			break
		}
	}
}
