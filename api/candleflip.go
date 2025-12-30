// api/candleflip.go
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"goLangServer/ws"
)

/* =========================
   RESPONSE TYPES
========================= */

type RoomResponse struct {
	RoomNumber int     `json:"roomNumber"`
	Status     string  `json:"status"`
	FinalPrice float64 `json:"finalPrice,omitempty"`
	Winner     string  `json:"winner,omitempty"`
	PlayerWon  bool    `json:"playerWon"`
}

type BatchResponse struct {
	BatchID        string          `json:"batchId"`
	PlayerAddress  string          `json:"playerAddress"`
	AmountPerRoom  string          `json:"amountPerRoom"`
	TotalRooms     int             `json:"totalRooms"`
	PlayerSide     string          `json:"playerSide"`
	AISide         string          `json:"aiSide"`
	Status         string          `json:"status"`
	WonRooms       int             `json:"wonRooms"`
	PayoutAmount   string          `json:"payoutAmount,omitempty"`
	PayoutTxHash   string          `json:"payoutTxHash,omitempty"`
	PayoutError    string          `json:"payoutError,omitempty"`
	ServerSeed     string          `json:"serverSeed,omitempty"`
	ServerSeedHash string          `json:"serverSeedHash"`
	Rooms          []RoomResponse  `json:"rooms"`
}

type AllBatchesResponse struct {
	Success bool            `json:"success"`
	Batches []BatchResponse `json:"batches"`
	Count   int             `json:"count"`
}

type SingleBatchResponse struct {
	Success bool          `json:"success"`
	Batch   BatchResponse `json:"batch,omitempty"`
	Message string        `json:"message,omitempty"`
}

/* =========================
   HTTP ENDPOINTS
========================= */

// HandleGetAllBatches returns all active batches
// GET /api/candleflip/batches
func HandleGetAllBatches(w http.ResponseWriter, r *http.Request) {
	batches := ws.GetAllBatches()

	response := AllBatchesResponse{
		Success: true,
		Batches: make([]BatchResponse, 0, len(batches)),
		Count:   len(batches),
	}

	for _, batch := range batches {
		batch.RLock()
		
		batchResp := BatchResponse{
			BatchID:        batch.BatchID,
			PlayerAddress:  batch.PlayerAddress.Hex(),
			AmountPerRoom:  batch.AmountPerRoom.String(),
			TotalRooms:     batch.TotalRooms,
			PlayerSide:     batch.PlayerSide,
			AISide:         getOppositeSide(batch.PlayerSide),
			Status:         batch.Status,
			WonRooms:       batch.WonRooms,
			ServerSeedHash: batch.ServerSeedHash,
			Rooms:          make([]RoomResponse, len(batch.Rooms)),
		}

		// Include server seed only if batch is completed or paid
		if batch.Status == "completed" || batch.Status == "paid" {
			batchResp.ServerSeed = batch.ServerSeed
		}

		// Include payout info
		if batch.PayoutAmount != nil {
			batchResp.PayoutAmount = batch.PayoutAmount.String()
		}
		if batch.PayoutTxHash != "" {
			batchResp.PayoutTxHash = batch.PayoutTxHash
		}
		if batch.PayoutError != "" {
			batchResp.PayoutError = batch.PayoutError
		}

		// Copy room data
		for i, room := range batch.Rooms {
			batchResp.Rooms[i] = RoomResponse{
				RoomNumber: room.RoomNumber,
				Status:     room.Status,
				FinalPrice: room.FinalPrice,
				Winner:     room.Winner,
				PlayerWon:  room.PlayerWon,
			}
		}

		batch.RUnlock()

		response.Batches = append(response.Batches, batchResp)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	log.Printf("📋 Retrieved %d CandleFlip batches", len(batches))
}

// HandleGetBatchByID returns a specific batch by ID
// GET /api/candleflip/batch/:batchId
func HandleGetBatchByID(w http.ResponseWriter, r *http.Request) {
	// Extract batch ID from URL path
	// Expected format: /api/candleflip/batch/{batchId}
	path := r.URL.Path
	parts := strings.Split(path, "/")
	if len(parts) < 5 {
		sendError(w, http.StatusBadRequest, "Batch ID is required")
		return
	}
	batchID := parts[4]

	batch := ws.GetBatch(batchID)
	if batch == nil {
		sendError(w, http.StatusNotFound, "Batch not found")
		return
	}

	batch.RLock()
	defer batch.RUnlock()

	batchResp := BatchResponse{
		BatchID:        batch.BatchID,
		PlayerAddress:  batch.PlayerAddress.Hex(),
		AmountPerRoom:  batch.AmountPerRoom.String(),
		TotalRooms:     batch.TotalRooms,
		PlayerSide:     batch.PlayerSide,
		AISide:         getOppositeSide(batch.PlayerSide),
		Status:         batch.Status,
		WonRooms:       batch.WonRooms,
		ServerSeedHash: batch.ServerSeedHash,
		Rooms:          make([]RoomResponse, len(batch.Rooms)),
	}

	// Include server seed only if batch is completed or paid
	if batch.Status == "completed" || batch.Status == "paid" {
		batchResp.ServerSeed = batch.ServerSeed
	}

	// Include payout info
	if batch.PayoutAmount != nil {
		batchResp.PayoutAmount = batch.PayoutAmount.String()
	}
	if batch.PayoutTxHash != "" {
		batchResp.PayoutTxHash = batch.PayoutTxHash
	}
	if batch.PayoutError != "" {
		batchResp.PayoutError = batch.PayoutError
	}

	// Copy room data
	for i, room := range batch.Rooms {
		batchResp.Rooms[i] = RoomResponse{
			RoomNumber: room.RoomNumber,
			Status:     room.Status,
			FinalPrice: room.FinalPrice,
			Winner:     room.Winner,
			PlayerWon:  room.PlayerWon,
		}
	}

	response := SingleBatchResponse{
		Success: true,
		Batch:   batchResp,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	log.Printf("📋 Retrieved batch: %s", batchID)
}

// HandleVerifyBatch verifies a batch using server seed
// GET /api/verify/candleflip/:batchId
func HandleVerifyBatch(w http.ResponseWriter, r *http.Request) {
	// Extract batch ID from URL path
	// Expected format: /api/verify/candleflip/{batchId}
	path := r.URL.Path
	parts := strings.Split(path, "/")
	if len(parts) < 5 {
		sendError(w, http.StatusBadRequest, "Batch ID is required")
		return
	}
	batchID := parts[4]

	batch := ws.GetBatch(batchID)
	if batch == nil {
		sendError(w, http.StatusNotFound, "Batch not found")
		return
	}

	batch.RLock()
	defer batch.RUnlock()

	// Only return server seed if game is completed
	if batch.Status != "completed" && batch.Status != "paid" {
		sendError(w, http.StatusBadRequest, "Batch is not yet completed")
		return
	}

	response := map[string]interface{}{
		"success":        true,
		"batchId":        batch.BatchID,
		"serverSeed":     batch.ServerSeed,
		"serverSeedHash": batch.ServerSeedHash,
		"totalRooms":     batch.TotalRooms,
		"wonRooms":       batch.WonRooms,
		"message":        "Verify by hashing the serverSeed and comparing with serverSeedHash. Each room can be reproduced using the seed format: serverSeed-room-{roomNumber}",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	log.Printf("🔍 Batch verification - Batch: %s", batchID)
}

/* =========================
   HELPER FUNCTIONS
========================= */

func getOppositeSide(side string) string {
	if side == "bull" {
		return "bear"
	}
	return "bull"
}