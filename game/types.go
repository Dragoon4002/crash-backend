package game

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

type GameResult struct {
	PeakMultiplier float64
	FinalPrice     float64
	TotalTicks     int
	ServerSeed     string
	GameID         string
}
