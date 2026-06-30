package savings

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// SourceTotal aggregates the savings of one source.
type SourceTotal struct {
	Source   Source `json:"source"`
	Events   int    `json:"events"`
	Baseline int    `json:"baseline_tokens"`
	Actual   int    `json:"actual_tokens"`
	Saved    int    `json:"saved_tokens"`
}

// DayTotal aggregates the savings of one calendar day (UTC).
type DayTotal struct {
	Day      string `json:"day"` // YYYY-MM-DD
	Events   int    `json:"events"`
	Saved    int    `json:"saved_tokens"`
	Baseline int    `json:"baseline_tokens"`
}

// Report is the aggregated view of the ledger.
type Report struct {
	Events         int           `json:"events"`
	BaselineTokens int           `json:"baseline_tokens"`
	ActualTokens   int           `json:"actual_tokens"`
	SavedTokens    int           `json:"saved_tokens"`
	ReductionPct   float64       `json:"reduction_pct"`
	BySource       []SourceTotal `json:"by_source"`
	ByDay          []DayTotal    `json:"by_day"`
	Since          *time.Time    `json:"since,omitempty"`
}

// SummaryOptions filters the aggregation.
type SummaryOptions struct {
	// Since, when non-zero, ignores entries older than it.
	Since time.Time
}

// LedgerPath returns the ledger file path for a data directory.
func LedgerPath(dataDir string) string {
	return filepath.Join(dataDir, ledgerSubdir, ledgerFile)
}

// Summarize reads and aggregates the ledger under dataDir. A missing ledger
// yields a zero-value report (no error). Malformed lines are skipped. Both the
// active ledger and its rotated ".1" backup are read so totals survive rotation.
func Summarize(dataDir string, opts SummaryOptions) (Report, error) {
	path := LedgerPath(dataDir)

	bySource := map[Source]*SourceTotal{}
	byDay := map[string]*DayTotal{}
	rep := Report{}
	if !opts.Since.IsZero() {
		s := opts.Since
		rep.Since = &s
	}

	scan := func(p string) error {
		f, err := os.Open(p)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		defer f.Close()

		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Bytes()
			if len(line) == 0 {
				continue
			}
			var e Entry
			if err := json.Unmarshal(line, &e); err != nil {
				continue
			}
			if !opts.Since.IsZero() && e.TS.Before(opts.Since) {
				continue
			}
			rep.Events++
			rep.BaselineTokens += e.BaselineTokens
			rep.ActualTokens += e.ActualTokens
			rep.SavedTokens += e.Saved

			st := bySource[e.Source]
			if st == nil {
				st = &SourceTotal{Source: e.Source}
				bySource[e.Source] = st
			}
			st.Events++
			st.Baseline += e.BaselineTokens
			st.Actual += e.ActualTokens
			st.Saved += e.Saved

			day := e.TS.UTC().Format("2006-01-02")
			dt := byDay[day]
			if dt == nil {
				dt = &DayTotal{Day: day}
				byDay[day] = dt
			}
			dt.Events++
			dt.Saved += e.Saved
			dt.Baseline += e.BaselineTokens
		}
		return sc.Err()
	}

	// Read the rotated backup first (older) then the active file (newer).
	if err := scan(path + ".1"); err != nil {
		return rep, err
	}
	if err := scan(path); err != nil {
		return rep, err
	}

	if rep.BaselineTokens > 0 {
		rep.ReductionPct = float64(rep.SavedTokens) / float64(rep.BaselineTokens) * 100
	}

	for _, st := range bySource {
		rep.BySource = append(rep.BySource, *st)
	}
	sort.Slice(rep.BySource, func(i, j int) bool {
		return rep.BySource[i].Saved > rep.BySource[j].Saved
	})
	for _, dt := range byDay {
		rep.ByDay = append(rep.ByDay, *dt)
	}
	sort.Slice(rep.ByDay, func(i, j int) bool {
		return rep.ByDay[i].Day < rep.ByDay[j].Day
	})

	return rep, nil
}

// EstimatedUSD converts saved tokens to an estimated dollar amount given a price
// per million tokens. Purely indicative — actual cost depends on the model.
func (r Report) EstimatedUSD(pricePerMillion float64) float64 {
	return float64(r.SavedTokens) / 1_000_000 * pricePerMillion
}
