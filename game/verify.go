package game

// VerifyGamePeak calculates the predetermined peak multiplier for a game
// without simulating the entire game. This is useful for verification purposes.
// Given the same serverSeed and gameID, it will always return the same peak.
func VerifyGamePeak(serverSeed, gameID string) float64 {
	combined := serverSeed + "-" + gameID
	rng := NewSeededRNG(combined)
	return determineTargetPeak(rng)
}

// VerifyGameResult runs a full game simulation and returns the complete result
// This allows users to verify the entire game outcome, not just the peak
func VerifyGameResult(serverSeed, gameID string) GameResult {
	return CalculateGame(serverSeed, gameID)
}
