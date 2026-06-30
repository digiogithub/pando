package savings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTokensFromChars(t *testing.T) {
	cases := map[int]int{0: 0, -5: 0, 1: 1, 4: 1, 5: 2, 8: 2, 9: 3}
	for in, want := range cases {
		if got := TokensFromChars(in); got != want {
			t.Errorf("TokensFromChars(%d)=%d want %d", in, got, want)
		}
	}
}

// writeLedger writes entries as JSONL to <dir>/savings/ledger.jsonl.
func writeLedger(t *testing.T, dir string, entries []Entry) {
	t.Helper()
	sdir := filepath.Join(dir, ledgerSubdir)
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(sdir, ledgerFile))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSummarizeMissingLedger(t *testing.T) {
	rep, err := Summarize(t.TempDir(), SummaryOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rep.Events != 0 || rep.SavedTokens != 0 {
		t.Fatalf("missing ledger should be zero, got %+v", rep)
	}
}

func TestSummarizeAggregates(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeLedger(t, dir, []Entry{
		{TS: now, Source: SourceView, BaselineTokens: 1000, ActualTokens: 200, Saved: 800},
		{TS: now, Source: SourceView, BaselineTokens: 500, ActualTokens: 100, Saved: 400},
		{TS: now, Source: SourceBash, BaselineTokens: 300, ActualTokens: 50, Saved: 250},
		{TS: now, Source: SourceDedup, BaselineTokens: 200, ActualTokens: 10, Saved: 190},
	})

	rep, err := Summarize(dir, SummaryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Events != 4 {
		t.Fatalf("Events=%d want 4", rep.Events)
	}
	if rep.SavedTokens != 1640 {
		t.Fatalf("SavedTokens=%d want 1640", rep.SavedTokens)
	}
	if rep.BaselineTokens != 2000 {
		t.Fatalf("BaselineTokens=%d want 2000", rep.BaselineTokens)
	}
	// 1640/2000 = 82%
	if rep.ReductionPct < 81.9 || rep.ReductionPct > 82.1 {
		t.Fatalf("ReductionPct=%.2f want ~82", rep.ReductionPct)
	}
	// Sorted by saved desc → view (1200) first.
	if len(rep.BySource) != 3 || rep.BySource[0].Source != SourceView || rep.BySource[0].Saved != 1200 {
		t.Fatalf("BySource[0]=%+v want view/1200", rep.BySource[0])
	}
	if usd := rep.EstimatedUSD(3); usd < 0.0049 || usd > 0.0050 {
		t.Fatalf("EstimatedUSD(3)=%f want ~0.00492", usd)
	}
}

func TestSummarizeSinceFilter(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeLedger(t, dir, []Entry{
		{TS: now.AddDate(0, 0, -10), Source: SourceView, BaselineTokens: 1000, Saved: 1000},
		{TS: now, Source: SourceView, BaselineTokens: 500, Saved: 500},
	})

	rep, err := Summarize(dir, SummaryOptions{Since: now.AddDate(0, 0, -2)})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Events != 1 || rep.SavedTokens != 500 {
		t.Fatalf("since-filter Events=%d Saved=%d want 1/500", rep.Events, rep.SavedTokens)
	}
	if rep.Since == nil {
		t.Fatal("Since should be reported")
	}
}

func TestSummarizeReadsRotatedBackup(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, ledgerSubdir)
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	// Backup (older) + active (newer).
	backup := filepath.Join(sdir, ledgerFile+".1")
	b, _ := json.Marshal(Entry{TS: now, Source: SourceBash, BaselineTokens: 100, Saved: 60})
	if err := os.WriteFile(backup, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	writeLedger(t, dir, []Entry{{TS: now, Source: SourceView, BaselineTokens: 200, Saved: 150}})

	rep, err := Summarize(dir, SummaryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Events != 2 || rep.SavedTokens != 210 {
		t.Fatalf("rotated read Events=%d Saved=%d want 2/210", rep.Events, rep.SavedTokens)
	}
}

func TestSummarizeSkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, ledgerSubdir)
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"ts":"2026-06-30T00:00:00Z","source":"view","baseline_tokens":100,"saved":80}
this is not json
{"ts":"2026-06-30T00:00:00Z","source":"bash","baseline_tokens":50,"saved":30}
`
	if err := os.WriteFile(filepath.Join(sdir, ledgerFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := Summarize(dir, SummaryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Events != 2 || rep.SavedTokens != 110 {
		t.Fatalf("malformed-skip Events=%d Saved=%d want 2/110", rep.Events, rep.SavedTokens)
	}
}

func TestRecorderRotation(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, ledgerSubdir)
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := &recorder{path: filepath.Join(sdir, ledgerFile), dir: sdir}
	// Force the next write over the cap to trigger rotation.
	r.written = maxLedgerBytes
	// Seed an existing active file so rotation has something to move.
	if err := os.WriteFile(r.path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.writeOne(Entry{TS: time.Now(), Source: SourceView, BaselineTokens: 10, Saved: 5})

	if _, err := os.Stat(r.path + ".1"); err != nil {
		t.Fatalf("expected rotated backup: %v", err)
	}
	if r.written == 0 {
		t.Fatal("written should reflect the post-rotation line")
	}
}
