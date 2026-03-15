package stats

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ClaudeStatsCache matches the structure of ~/.claude/stats-cache.json (version 2)
type ClaudeStatsCache struct {
	Version                     int                        `json:"version"`
	LastComputedDate            string                     `json:"lastComputedDate"`
	DailyActivity               []DailyActivity            `json:"dailyActivity"`
	DailyModelTokens            []DailyModelTokens         `json:"dailyModelTokens"`
	ModelUsage                  map[string]ModelTokenUsage `json:"modelUsage"`
	TotalSessions               int                        `json:"totalSessions"`
	TotalMessages               int                        `json:"totalMessages"`
	LongestSession              *SessionStat               `json:"longestSession"`
	FirstSessionDate            string                     `json:"firstSessionDate"`
	HourCounts                  map[string]int             `json:"hourCounts"`
	TotalSpeculationTimeSavedMs int64                      `json:"totalSpeculationTimeSavedMs"`
}

// DailyActivity holds per-day usage counts
type DailyActivity struct {
	Date          string `json:"date"` // "YYYY-MM-DD"
	MessageCount  int    `json:"messageCount"`
	SessionCount  int    `json:"sessionCount"`
	ToolCallCount int    `json:"toolCallCount"`
}

// ModelTokenUsage holds token counts for a specific model
type ModelTokenUsage struct {
	InputTokens              int64 `json:"inputTokens"`
	OutputTokens             int64 `json:"outputTokens"`
	CacheReadInputTokens     int64 `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int64 `json:"cacheCreationInputTokens"`
}

// DailyModelTokens holds per-day token usage broken down by model
type DailyModelTokens struct {
	Date          string                     `json:"date"`
	TokensByModel map[string]ModelTokenUsage `json:"tokensByModel"`
}

// SessionStat represents a session with a date and message count
type SessionStat struct {
	Date         string `json:"date"`
	MessageCount int    `json:"messageCount"`
}

// DailySummary aggregates activity for a single day
type DailySummary struct {
	Date              string
	MessageCount      int
	SessionCount      int
	ToolCallCount     int
	TotalInputTokens  int64
	TotalOutputTokens int64
	TopModel          string
}

// WeeklySummary aggregates activity for a 7-day window
type WeeklySummary struct {
	StartDate         string
	EndDate           string
	TotalMessages     int
	TotalSessions     int
	TotalToolCalls    int
	TotalInputTokens  int64
	TotalOutputTokens int64
	DailyBreakdown    []DailySummary
	TopModel          string
}

// AllTimeSummary aggregates all historical activity
type AllTimeSummary struct {
	TotalSessions    int
	TotalMessages    int
	FirstSessionDate string
	LongestSession   *SessionStat
	ModelUsage       map[string]ModelTokenUsage
	TopModel         string
}

// claudeCodeStatsPath returns the path to Claude Code's stats cache file
func claudeCodeStatsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "stats-cache.json")
}

// pandoStatsPath returns the path to Pando's own stats cache file
func pandoStatsPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(configDir, "pando", "stats-cache.json")
}

// loadStatsFromPath reads and parses a stats-cache.json at the given path.
// Returns an empty cache (not an error) if the file does not exist.
func loadStatsFromPath(path string) (*ClaudeStatsCache, error) {
	if path == "" {
		return &ClaudeStatsCache{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ClaudeStatsCache{}, nil
		}
		return nil, fmt.Errorf("reading stats file %s: %w", path, err)
	}
	var cache ClaudeStatsCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("parsing stats file %s: %w", path, err)
	}
	return &cache, nil
}

// LoadClaudeCodeStats reads stats from ~/.claude/stats-cache.json.
// Returns an empty cache if the file does not exist.
func LoadClaudeCodeStats() (*ClaudeStatsCache, error) {
	return loadStatsFromPath(claudeCodeStatsPath())
}

// LoadPandoStats reads stats from Pando's own stats file.
// Returns an empty cache if the file does not exist.
func LoadPandoStats() (*ClaudeStatsCache, error) {
	return loadStatsFromPath(pandoStatsPath())
}

// LoadBestAvailableStats tries Pando's stats first, then falls back to Claude Code's stats.
// Returns an error only if both files fail to load (not if they are simply absent).
func LoadBestAvailableStats() (*ClaudeStatsCache, error) {
	pandoCache, err := LoadPandoStats()
	if err == nil && pandoCache != nil && pandoCache.TotalMessages > 0 {
		return pandoCache, nil
	}
	claudeCache, err2 := LoadClaudeCodeStats()
	if err2 == nil {
		return claudeCache, nil
	}
	// Both failed with real errors
	return nil, fmt.Errorf("pando stats: %v; claude-code stats: %v", err, err2)
}

// GetTodayActivity returns activity data for today.
func GetTodayActivity(cache *ClaudeStatsCache) DailySummary {
	today := time.Now().Format("2006-01-02")
	summary := DailySummary{Date: today}

	if cache == nil {
		return summary
	}

	for _, a := range cache.DailyActivity {
		if a.Date == today {
			summary.MessageCount = a.MessageCount
			summary.SessionCount = a.SessionCount
			summary.ToolCallCount = a.ToolCallCount
			break
		}
	}

	// Collect token data for today
	todayModelUsage := make(map[string]ModelTokenUsage)
	for _, d := range cache.DailyModelTokens {
		if d.Date == today {
			for model, usage := range d.TokensByModel {
				existing := todayModelUsage[model]
				existing.InputTokens += usage.InputTokens
				existing.OutputTokens += usage.OutputTokens
				existing.CacheReadInputTokens += usage.CacheReadInputTokens
				existing.CacheCreationInputTokens += usage.CacheCreationInputTokens
				todayModelUsage[model] = existing
				summary.TotalInputTokens += usage.InputTokens
				summary.TotalOutputTokens += usage.OutputTokens
			}
			break
		}
	}

	summary.TopModel = TopModel(todayModelUsage)
	return summary
}

// GetWeeklySummary returns a summary of the last 7 days.
func GetWeeklySummary(cache *ClaudeStatsCache) WeeklySummary {
	now := time.Now()
	endDate := now.Format("2006-01-02")
	startDate := now.AddDate(0, 0, -6).Format("2006-01-02")

	weekly := WeeklySummary{
		StartDate: startDate,
		EndDate:   endDate,
	}

	if cache == nil {
		return weekly
	}

	// Build date set for quick lookup
	dateSet := make(map[string]bool)
	for i := 0; i < 7; i++ {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		dateSet[d] = true
	}

	// Activity per day map
	activityByDate := make(map[string]DailyActivity)
	for _, a := range cache.DailyActivity {
		if dateSet[a.Date] {
			activityByDate[a.Date] = a
		}
	}

	// Token data per day map
	tokensByDate := make(map[string]map[string]ModelTokenUsage)
	for _, d := range cache.DailyModelTokens {
		if dateSet[d.Date] {
			tokensByDate[d.Date] = d.TokensByModel
		}
	}

	// Aggregate model usage across the week for TopModel
	weeklyModelUsage := make(map[string]ModelTokenUsage)

	for i := 6; i >= 0; i-- {
		date := now.AddDate(0, 0, -i).Format("2006-01-02")
		ds := DailySummary{Date: date}

		if a, ok := activityByDate[date]; ok {
			ds.MessageCount = a.MessageCount
			ds.SessionCount = a.SessionCount
			ds.ToolCallCount = a.ToolCallCount
		}

		if models, ok := tokensByDate[date]; ok {
			dayUsage := make(map[string]ModelTokenUsage)
			for model, usage := range models {
				ds.TotalInputTokens += usage.InputTokens
				ds.TotalOutputTokens += usage.OutputTokens
				dayUsage[model] = usage

				existing := weeklyModelUsage[model]
				existing.InputTokens += usage.InputTokens
				existing.OutputTokens += usage.OutputTokens
				existing.CacheReadInputTokens += usage.CacheReadInputTokens
				existing.CacheCreationInputTokens += usage.CacheCreationInputTokens
				weeklyModelUsage[model] = existing
			}
			ds.TopModel = TopModel(dayUsage)
		}

		weekly.TotalMessages += ds.MessageCount
		weekly.TotalSessions += ds.SessionCount
		weekly.TotalToolCalls += ds.ToolCallCount
		weekly.TotalInputTokens += ds.TotalInputTokens
		weekly.TotalOutputTokens += ds.TotalOutputTokens
		weekly.DailyBreakdown = append(weekly.DailyBreakdown, ds)
	}

	weekly.TopModel = TopModel(weeklyModelUsage)
	return weekly
}

// GetAllTimeSummary returns aggregate statistics from the entire cache.
func GetAllTimeSummary(cache *ClaudeStatsCache) AllTimeSummary {
	if cache == nil {
		return AllTimeSummary{}
	}
	return AllTimeSummary{
		TotalSessions:    cache.TotalSessions,
		TotalMessages:    cache.TotalMessages,
		FirstSessionDate: cache.FirstSessionDate,
		LongestSession:   cache.LongestSession,
		ModelUsage:       cache.ModelUsage,
		TopModel:         TopModel(cache.ModelUsage),
	}
}

// TopModel returns the model name with the highest inputTokens.
// Returns an empty string if modelUsage is nil or empty.
func TopModel(modelUsage map[string]ModelTokenUsage) string {
	if len(modelUsage) == 0 {
		return ""
	}

	// Sort keys for deterministic output when counts are equal
	models := make([]string, 0, len(modelUsage))
	for m := range modelUsage {
		models = append(models, m)
	}
	sort.Strings(models)

	top := ""
	var topTokens int64
	for _, m := range models {
		if u := modelUsage[m]; u.InputTokens > topTokens {
			topTokens = u.InputTokens
			top = m
		}
	}
	return top
}

// FormatTokenCount formats a token count as a human-readable string.
// Examples: 1234567 → "1.2M", 45000 → "45K", 892 → "892"
func FormatTokenCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dK", n/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// shortModelName strips the date suffix from a model ID for compact display.
// e.g. "claude-sonnet-4-5-20250929" → "claude-sonnet-4-5"
func shortModelName(model string) string {
	// Model names often end with a date like -20250929 or -20251001
	parts := strings.Split(model, "-")
	if len(parts) > 0 {
		last := parts[len(parts)-1]
		if len(last) == 8 {
			allDigits := true
			for _, c := range last {
				if c < '0' || c > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				return strings.Join(parts[:len(parts)-1], "-")
			}
		}
	}
	return model
}

// FormatStats returns a formatted multi-line string suitable for TUI display.
func FormatStats(cache *ClaudeStatsCache) string {
	if cache == nil {
		return "No stats available."
	}

	today := GetTodayActivity(cache)
	weekly := GetWeeklySummary(cache)
	allTime := GetAllTimeSummary(cache)

	topModel := shortModelName(allTime.TopModel)
	if topModel == "" {
		topModel = "N/A"
	}

	firstUse := allTime.FirstSessionDate
	if firstUse == "" {
		firstUse = "N/A"
	}

	var sb strings.Builder
	sep := strings.Repeat("─", 33)

	sb.WriteString("📊 Usage Statistics\n")
	sb.WriteString(sep + "\n")
	sb.WriteString(fmt.Sprintf("Today:       %d messages · %d sessions · %d tool calls\n",
		today.MessageCount, today.SessionCount, today.ToolCallCount))
	sb.WriteString(fmt.Sprintf("This week:   %s messages · %d sessions\n",
		formatInt(weekly.TotalMessages), weekly.TotalSessions))
	sb.WriteString(fmt.Sprintf("All time:    %s messages · %d sessions\n",
		formatInt(allTime.TotalMessages), allTime.TotalSessions))
	sb.WriteString(fmt.Sprintf("Top model:   %s\n", topModel))
	sb.WriteString(fmt.Sprintf("First use:   %s\n", firstUse))
	sb.WriteString(sep + "\n")
	sb.WriteString(fmt.Sprintf("Tokens (today):  ↑ %s input · ↓ %s output\n",
		FormatTokenCount(today.TotalInputTokens),
		FormatTokenCount(today.TotalOutputTokens)))

	return sb.String()
}

// formatInt formats an integer with comma separators for readability.
func formatInt(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}
