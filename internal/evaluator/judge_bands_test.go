package evaluator

import (
	"testing"
)

func TestShouldJudge_Bands(t *testing.T) {
	j := (&EvaluatorService{}).judgeSettings() // defaults: 0.8 / 0.3 / 4 turns
	cases := []struct {
		name   string
		reward float64
		turns  int
		want   bool
	}{
		{"mid reward, few turns", 0.55, 3, false},
		{"mid reward, many turns", 0.55, 6, false},
		{"low reward, many turns", 0.15, 6, true},
		{"low reward, few turns", 0.15, 3, false},
		{"high reward, many turns", 0.9, 4, true},
		{"boundary high", 0.8, 4, true},
		{"boundary low", 0.3, 4, true},
	}
	for _, c := range cases {
		if got := shouldJudge(j, c.reward, c.turns); got != c.want {
			t.Errorf("%s: shouldJudge(%v, %d) = %v, want %v", c.name, c.reward, c.turns, got, c.want)
		}
	}
}
