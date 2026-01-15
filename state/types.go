package state

import (
	"math/big"
	"sync"
	"time"
)

// ==============================================================================
// GLOBAL GAME STATE (Single Source of Truth)
// ==============================================================================
//
// NOTE:
// CandleFlip now uses a GENERAL subscription model.
// Rooms are pure state objects (NO websocket connections inside rooms).
// Broadcasting is handled at a higher layer by sending ALL rooms together.
//
// Source file reference: :contentReference[oaicite:0]{index=0}
//
// ==============================================================================

type GlobalGameState struct {
	mu sync.RWMutex

	// Subsystems
	Chat       *ChatState
	Crash      *CrashGameState
	CandleFlip *CandleFlipState

	// Server metadata
	ServerStartTime  time.Time
	TotalConnections int64
}

func NewGlobalGameState() *GlobalGameState {
	return &GlobalGameState{
		Chat:             NewChatState(),
		Crash:            NewCrashGameState(),
		CandleFlip:       NewCandleFlipState(),
		ServerStartTime:  time.Now(),
		TotalConnections: 0,
	}
}

// ==============================================================================
// CHAT STATE
// ==============================================================================

type ChatState struct {
	mu       sync.RWMutex
	Messages []ChatMessage
	MaxSize  int
}

type ChatMessage struct {
	UserID    string    `json:"userId"`
	Username  string    `json:"username"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

func NewChatState() *ChatState {
	return &ChatState{
		Messages: make([]ChatMessage, 0, 100),
		MaxSize:  100,
	}
}

func (c *ChatState) AddMessage(msg ChatMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.Messages = append(c.Messages, msg)
	if len(c.Messages) > c.MaxSize {
		c.Messages = c.Messages[1:]
	}
}

func (c *ChatState) GetMessages() []ChatMessage {
	c.mu.RLock()
	defer c.mu.RUnlock()

	msgs := make([]ChatMessage, len(c.Messages))
	copy(msgs, c.Messages)
	return msgs
}

// ==============================================================================
// CRASH GAME STATE
// ==============================================================================

type CrashPhase string

const (
	CrashPhaseWaiting   CrashPhase = "waiting"
	CrashPhaseCountdown CrashPhase = "countdown"
	CrashPhaseRunning   CrashPhase = "running"
	CrashPhaseCrashed   CrashPhase = "crashed"
)

type CrashGameState struct {
	mu sync.RWMutex

	Phase CrashPhase

	GameID         string
	ServerSeed     string
	ServerSeedHash string
	ContractGameID *big.Int

	CurrentTick    int
	CurrentPrice   float64
	PeakMultiplier float64
	StartTime      time.Time

	CompletedCandles []*CandleGroup
	CurrentCandle    *CandleGroup
	CandleGroupState *CandleGroupingState

	ActiveBettors map[string]*ActiveBettor
	GameHistory   []CrashGameHistory

	HasReachedPeak bool
	Rugged         bool
	CountdownValue int
}

type CandleGroupingState struct {
	GroupDuration  int64
	GroupStartTime int64
	MergeCount     int
}

type CrashGameHistory struct {
	GameID         string         `json:"gameId"`
	PeakMultiplier float64        `json:"peakMultiplier"`
	Rugged         bool           `json:"rugged"`
	Candles        []*CandleGroup `json:"candles"`
	Timestamp      time.Time      `json:"timestamp"`
}

type ActiveBettor struct {
	Address         string    `json:"address"`
	BetAmount       float64   `json:"betAmount"`
	EntryMultiplier float64   `json:"entryMultiplier"`
	BetTime         time.Time `json:"betTime"`
}

func NewCrashGameState() *CrashGameState {
	return &CrashGameState{
		Phase:            CrashPhaseWaiting,
		CompletedCandles: make([]*CandleGroup, 0),
		ActiveBettors:    make(map[string]*ActiveBettor),
		GameHistory:      make([]CrashGameHistory, 0, 15),
	}
}

func (c *CrashGameState) ResetForNewGame() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.Phase = CrashPhaseCountdown
	c.CurrentTick = 0
	c.CurrentPrice = 1.0
	c.PeakMultiplier = 1.0
	c.CompletedCandles = make([]*CandleGroup, 0)
	c.CurrentCandle = nil
	c.CandleGroupState = &CandleGroupingState{
		GroupDuration:  1000,
		GroupStartTime: time.Now().UnixMilli(),
	}
	c.HasReachedPeak = false
	c.Rugged = false
	c.ActiveBettors = make(map[string]*ActiveBettor)
	c.CountdownValue = 3
}

func (c *CrashGameState) AddToHistory(history CrashGameHistory) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.GameHistory = append(c.GameHistory, history)
	if len(c.GameHistory) > 15 {
		c.GameHistory = c.GameHistory[len(c.GameHistory)-15:]
	}
}

func (c *CrashGameState) AddBettor(address string, amount, multiplier float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ActiveBettors[address] = &ActiveBettor{
		Address:         address,
		BetAmount:       amount,
		EntryMultiplier: multiplier,
		BetTime:         time.Now(),
	}
}

func (c *CrashGameState) RemoveBettor(address string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.ActiveBettors, address)
}

func (c *CrashGameState) GetActiveBettors() []*ActiveBettor {
	c.mu.RLock()
	defer c.mu.RUnlock()

	bettors := make([]*ActiveBettor, 0, len(c.ActiveBettors))
	for _, b := range c.ActiveBettors {
		bettors = append(bettors, b)
	}
	return bettors
}

// ==============================================================================
// CANDLEFLIP STATE (GENERAL SUBSCRIPTION)
// ==============================================================================

type CandleFlipState struct {
	mu sync.RWMutex

	// All active rooms (broadcasted together)
	Rooms map[string]*CandleFlipRoom

	// Contract game tracking
	ContractGameTrackers map[string]*ContractGameTracker
}

type ContractGameTracker struct {
	mu             sync.Mutex
	GameID         string
	TotalRooms     int
	FinishedRooms  int
	RoomsWon       int
	ResolveStarted bool
}

func NewCandleFlipState() *CandleFlipState {
	return &CandleFlipState{
		Rooms:                make(map[string]*CandleFlipRoom),
		ContractGameTrackers: make(map[string]*ContractGameTracker),
	}
}

func (c *CandleFlipState) CreateRoom(roomID string) *CandleFlipRoom {
	c.mu.Lock()
	defer c.mu.Unlock()

	room := NewCandleFlipRoom(roomID)
	c.Rooms[roomID] = room
	return room
}

func (c *CandleFlipState) GetRoom(roomID string) (*CandleFlipRoom, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	room, ok := c.Rooms[roomID]
	return room, ok
}

func (c *CandleFlipState) RemoveRoom(roomID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.Rooms, roomID)
}

func (c *CandleFlipState) GetAllRooms() []*CandleFlipRoom {
	c.mu.RLock()
	defer c.mu.RUnlock()

	rooms := make([]*CandleFlipRoom, 0, len(c.Rooms))
	for _, r := range c.Rooms {
		rooms = append(rooms, r)
	}
	return rooms
}

// ==============================================================================
// CANDLEFLIP ROOM STATE (PURE STATE, NO WS)
// ==============================================================================

type RoomPhase string

const (
	RoomPhaseStarting RoomPhase = "starting"
	RoomPhaseRunning  RoomPhase = "running"
	RoomPhaseFinished RoomPhase = "finished"
)

type CandleFlipRoom struct {
	mu sync.RWMutex

	RoomID         string
	ContractGameID string
	Phase          RoomPhase

	GameID         string
	ServerSeed     string
	ServerSeedHash string

	CurrentTick  int
	CurrentPrice float64
	PriceHistory []float64

	CompletedCandles []*CandleGroup
	CurrentCandle    *CandleGroup
	CandleGroupState *CandleGroupingState

	BotName     string
	PlayerTrend string
	BearSide    string
	BullSide    string
	RoomsCount  int

	Winner    string
	PlayerWon bool

	StartTime time.Time
	EndTime   time.Time
}

func NewCandleFlipRoom(roomID string) *CandleFlipRoom {
	return &CandleFlipRoom{
		RoomID:           roomID,
		Phase:            RoomPhaseStarting,
		CurrentPrice:     1.0,
		PriceHistory:     []float64{1.0},
		CompletedCandles: make([]*CandleGroup, 0),
		CandleGroupState: &CandleGroupingState{
			GroupDuration:  1000,
			GroupStartTime: time.Now().UnixMilli(),
		},
	}
}

// ==============================================================================
// COMMON TYPES
// ==============================================================================

type CandleGroup struct {
	Open       float64   `json:"open"`
	Close      *float64  `json:"close,omitempty"`
	Max        float64   `json:"max"`
	Min        float64   `json:"min"`
	ValueList  []float64 `json:"valueList"`
	StartTime  int64     `json:"startTime"`
	DurationMs int64     `json:"durationMs"`
	IsComplete bool      `json:"isComplete"`
}

func (c *CandleGroup) DeepCopy() *CandleGroup {
	out := &CandleGroup{
		Open:       c.Open,
		Max:        c.Max,
		Min:        c.Min,
		StartTime:  c.StartTime,
		DurationMs: c.DurationMs,
		IsComplete: c.IsComplete,
	}

	if c.Close != nil {
		v := *c.Close
		out.Close = &v
	}

	if c.ValueList != nil {
		out.ValueList = make([]float64, len(c.ValueList))
		copy(out.ValueList, c.ValueList)
	}

	return out
}
