package fswatch

import "testing"

func TestEstimateCost(t *testing.T) {
	cases := []struct {
		goos string
		want int
	}{
		{"darwin", 150}, {"freebsd", 150}, {"linux", 100}, {"windows", 100},
	}
	for _, c := range cases {
		if got := EstimateCost(c.goos, 100, 50); got != c.want {
			t.Errorf("%s: got %d want %d", c.goos, got, c.want)
		}
	}
}

func TestWatchBudgetWarnsOnce(t *testing.T) {
	var calls int
	b := NewWatchBudget("test", "/root")
	b.goos = "darwin"
	b.threshold = 10
	b.warn = func(string, ...any) { calls++ }

	for i := 0; i < 5; i++ {
		b.AddDir()
	}
	for i := 0; i < 5; i++ {
		b.AddFile()
	}
	if b.Check() || calls != 0 {
		t.Fatal("must not warn at threshold")
	}
	b.AddFile()
	if !b.Check() || calls != 1 {
		t.Fatal("expected warning above threshold")
	}
	b.AddFile()
	if b.Check() || calls != 1 {
		t.Fatal("must warn only once")
	}
}

func TestWatchBudgetLinuxIgnoresFiles(t *testing.T) {
	b := NewWatchBudget("test", "/root")
	b.goos = "linux"
	b.threshold = 10
	b.warn = func(string, ...any) { t.Fatal("unexpected warning") }
	b.AddDir()
	for i := 0; i < 100; i++ {
		b.AddFile()
	}
	b.Check()
	var nilB *WatchBudget
	nilB.AddDir()
	nilB.AddFile()
	if nilB.Check() || nilB.Cost() != 0 {
		t.Fatal("nil budget must be inert")
	}
}
