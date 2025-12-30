package ws

import (
	"sync"
)

// Shared game state accessible to both WebSocket and API handlers
var (
	currentGameID      string
	currentGameIDMutex sync.RWMutex
)

// SetCurrentGameID updates the current crash game ID
func SetCurrentGameID(gameID string) {
	currentGameIDMutex.Lock()
	defer currentGameIDMutex.Unlock()
	currentGameID = gameID
}

// GetCurrentGameID returns the current crash game ID
func GetCurrentGameID() string {
	currentGameIDMutex.RLock()
	defer currentGameIDMutex.RUnlock()
	return currentGameID
}
