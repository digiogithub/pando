package evaluator

import "math"

// UCBScore computes the UCB1 value for a template.
// UCB_i = avg_reward_i + c * sqrt(ln(N) / n_i)
// where N = total sessions evaluated, n_i = times template i was used.
func UCBScore(avgReward float64, totalSessions, timesUsed int, explorationC float64) float64 {
	if timesUsed == 0 {
		return math.MaxFloat64 // never tried → highest priority for exploration
	}
	if totalSessions <= 0 {
		return avgReward
	}
	exploration := explorationC * math.Sqrt(math.Log(float64(totalSessions))/float64(timesUsed))
	return avgReward + exploration
}
