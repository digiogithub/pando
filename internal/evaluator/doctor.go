package evaluator

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/prompt"
)

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// recentEligibleWindow is how many of the most recent eligible sessions the
// "nothing evaluated lately" check looks at.
const recentEligibleWindow = 20

// Pattern lint kinds reported by Diagnose.
const (
	PatternIssueCompile         = "compile"
	PatternIssueDoubleBackslash = "double_backslash"
)

// PatternIssue is a problem found in a correction or task pattern.
type PatternIssue struct {
	// Kind is PatternIssueCompile or PatternIssueDoubleBackslash.
	Kind string `json:"kind"`
	// Source is the config key holding the pattern.
	Source  string `json:"source"`
	Pattern string `json:"pattern"`
	Message string `json:"message"`
	// Hint tells how to fix it.
	Hint string `json:"hint"`
}

// DoctorError is a timestamped failure recorded in memory by the service.
type DoctorError struct {
	Message string `json:"message"`
	At      int64  `json:"at"`
}

// SectionVariants is one prompt section that has variant files.
type SectionVariants struct {
	Section string `json:"section"`
	// Files is the number of variant files (the embedded default is extra).
	Files int `json:"files"`
	// Competing is true when the section has the default plus at least one file,
	// i.e. two or more candidates, so A/B selection actually happens.
	Competing bool `json:"competing"`
}

// DoctorJudge is the judge part of the report.
type DoctorJudge struct {
	// Configured is true when a judge model is set and the judge initialised
	// (or, without a service, when a model is set).
	Configured          bool         `json:"configured"`
	HighReward          float64      `json:"high_reward"`
	LowReward           float64      `json:"low_reward"`
	MinTurns            int          `json:"min_turns"`
	DailyCalls          int          `json:"daily_calls"`
	DailyTokens         int64        `json:"daily_tokens"`
	CallsToday          int64        `json:"calls_today"`
	TokensToday         int64        `json:"tokens_today"`
	BudgetExhausted     bool         `json:"budget_exhausted"`
	LastError           *DoctorError `json:"last_error,omitempty"`
	BackfillJudge       bool         `json:"backfill_judge"`
	MaxTranscriptTokens int          `json:"max_transcript_tokens"`
}

// DoctorVariants is the template-variant part of the report.
type DoctorVariants struct {
	Enabled bool `json:"enabled"`
	// Directories are the variant roots that exist on disk.
	Directories []string          `json:"directories"`
	Sections    []SectionVariants `json:"sections"`
	// Competing counts sections with two or more candidates.
	Competing int `json:"competing"`
}

// DoctorSkills counts the learned skills by review state.
type DoctorSkills struct {
	Directory string `json:"directory"`
	Pending   int    `json:"pending"`
	Approved  int    `json:"approved"`
	Rejected  int    `json:"rejected"`
}

// DoctorBackground describes the idle sweeper and backfill.
type DoctorBackground struct {
	IdleTimeout      string `json:"idle_timeout"`
	BackfillLimit    int    `json:"backfill_limit"`
	IncludeSubagents bool   `json:"include_subagents"`
	// Known is true when the report came from a process running the workers.
	Known bool `json:"known"`
	BackgroundState
}

// Report is the outcome of Diagnose: everything needed to tell whether the
// self-improvement loop is working and, if not, why.
type Report struct {
	Enabled  bool   `json:"enabled"`
	Model    string `json:"model,omitempty"`
	Provider string `json:"provider,omitempty"`
	// DisabledReasons explain why the loop is off or cannot run.
	DisabledReasons []string `json:"disabled_reasons,omitempty"`
	// Warnings are problems that do not stop the loop but need attention.
	Warnings []string `json:"warnings,omitempty"`

	EligibleSessions  int64 `json:"eligible_sessions"`
	EvaluatedSessions int64 `json:"evaluated_sessions"`
	// NeverEvaluated counts eligible sessions without a score.
	NeverEvaluated int64 `json:"never_evaluated"`
	// RecentWindow / RecentEvaluated: of the last RecentWindow idle eligible
	// sessions, how many have a score.
	RecentWindow    int64 `json:"recent_window"`
	RecentEvaluated int64 `json:"recent_evaluated"`
	// LastEvaluatedAt is the unix time of the newest score (0 = none).
	LastEvaluatedAt     int64        `json:"last_evaluated_at"`
	LastEvaluationError *DoctorError `json:"last_evaluation_error,omitempty"`

	Judge             DoctorJudge      `json:"judge"`
	Variants          DoctorVariants   `json:"variants"`
	Skills            DoctorSkills     `json:"skills"`
	ContextTrimmer    bool             `json:"context_trimmer"`
	Background        DoctorBackground `json:"background"`
	PatternIssues     []PatternIssue   `json:"pattern_issues,omitempty"`
	CollectionErrors  []string         `json:"collection_errors,omitempty"`
	GeneratedAt       int64            `json:"generated_at"`
	SessionsAvailable bool             `json:"sessions_available"`
}

// DiagnoseOptions are the inputs of Diagnose.
type DiagnoseOptions struct {
	// Config is the evaluator config as loaded (not defaulted).
	Config config.EvaluatorConfig
	// DB reads sessions and scores; nil skips the database checks.
	DB db.Querier
	// Service is the running evaluator, when there is one in this process. It
	// supplies the in-memory failures and the background worker state.
	Service *EvaluatorService
	// WorkDir is the project directory (variants, learned skills).
	WorkDir string
	// Now overrides the clock (tests).
	Now time.Time
}

// Diagnose inspects the self-improvement loop and returns a Report. It only
// reads: it never writes to the database or to disk.
func Diagnose(ctx context.Context, opts DiagnoseOptions) (*Report, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	cfg := opts.Config
	r := &Report{
		Enabled:  cfg.Enabled,
		Model:    string(cfg.Model),
		Provider: cfg.Provider,
	}
	r.GeneratedAt = now.Unix()
	collect := func(what string, err error) {
		if err != nil {
			r.CollectionErrors = append(r.CollectionErrors, fmt.Sprintf("%s: %v", what, err))
		}
	}

	diagnoseEnablement(r, cfg)
	r.PatternIssues = LintPatterns(cfg)

	jc := cfg.Judge
	if jc.HighReward == 0 {
		jc.HighReward = 0.8
	}
	if jc.LowReward == 0 {
		jc.LowReward = 0.3
	}
	if jc.MinTurns == 0 {
		jc.MinTurns = 4
	}
	if jc.MaxTranscriptTokens == 0 {
		jc.MaxTranscriptTokens = 6000
	}
	r.Judge = DoctorJudge{
		Configured:          strings.TrimSpace(string(cfg.Model)) != "",
		HighReward:          jc.HighReward,
		LowReward:           jc.LowReward,
		MinTurns:            jc.MinTurns,
		DailyCalls:          jc.DailyCalls,
		DailyTokens:         jc.DailyTokens,
		BackfillJudge:       cfg.BackfillJudge,
		MaxTranscriptTokens: jc.MaxTranscriptTokens,
	}
	r.ContextTrimmer = cfg.ContextTrimmer.Enabled
	r.Background = DoctorBackground{
		IdleTimeout:      firstNonEmpty(cfg.IdleTimeout, defaultIdleTimeout.String()),
		BackfillLimit:    cfg.BackfillLimit,
		IncludeSubagents: cfg.IncludeSubagents,
	}
	if r.Background.BackfillLimit == 0 {
		r.Background.BackfillLimit = defaultBackfillLimit
	}

	if svc := opts.Service; svc != nil {
		r.Background.Known = true
		r.Background.BackgroundState = svc.BackgroundState()
		if e := svc.LastEvaluationError(); e != nil {
			r.LastEvaluationError = &DoctorError{Message: e.Message, At: e.At.Unix()}
		}
		if e := svc.LastJudgeError(); e != nil {
			r.Judge.LastError = &DoctorError{Message: e.Message, At: e.At.Unix()}
		}
		r.Judge.Configured = r.Judge.Configured && svc.judge != nil
	}

	if q := opts.DB; q != nil {
		r.SessionsAvailable = true
		include := int64(0)
		if cfg.IncludeSubagents {
			include = 1
		}
		var err error
		if r.EligibleSessions, err = q.CountEligibleSessions(ctx, include); err != nil {
			collect("count eligible sessions", err)
			r.SessionsAvailable = false
		}
		if r.NeverEvaluated, err = q.CountUnscoredEligibleSessions(ctx, include); err != nil {
			collect("count unevaluated sessions", err)
		}
		if n, err := q.CountSessionScores(ctx); err == nil {
			r.EvaluatedSessions = n
		} else {
			collect("count session scores", err)
		}
		if at, err := q.GetLastEvaluationAt(ctx); err == nil {
			r.LastEvaluatedAt = at
		} else {
			collect("last evaluation", err)
		}
		cutoff := now.Add(-parseIdle(cfg.IdleTimeout)).Unix()
		if row, err := q.CountRecentEligibleScored(ctx, db.CountRecentEligibleScoredParams{
			UpdatedAt: cutoff, IncludeChildren: include, Limit: recentEligibleWindow,
		}); err == nil {
			r.RecentWindow, r.RecentEvaluated = row.Total, row.Scored
		} else {
			collect("recent sessions", err)
		}
		if usage, err := q.GetJudgeUsageSince(ctx, startOfDay(now)); err == nil {
			r.Judge.CallsToday, r.Judge.TokensToday = usage.Calls, usage.Tokens
			r.Judge.BudgetExhausted = (jc.DailyCalls > 0 && usage.Calls >= int64(jc.DailyCalls)) ||
				(jc.DailyTokens > 0 && usage.Tokens >= jc.DailyTokens)
		} else {
			collect("judge usage", err)
		}
	}

	r.Variants = diagnoseVariants(cfg, opts.WorkDir)
	r.Skills = diagnoseSkills(opts.WorkDir, collect)
	r.Warnings = append(r.Warnings, diagnoseWarnings(r, cfg)...)
	return r, nil
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func parseIdle(s string) time.Duration {
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return d
	}
	return defaultIdleTimeout
}

// diagnoseEnablement fills DisabledReasons: why the loop is off or cannot run.
func diagnoseEnablement(r *Report, cfg config.EvaluatorConfig) {
	if !cfg.Enabled {
		r.DisabledReasons = append(r.DisabledReasons,
			"evaluator.enabled is false (or it was enabled without a model, which disables it at load time)")
	}
	model := strings.TrimSpace(string(cfg.Model))
	if model == "" {
		r.DisabledReasons = append(r.DisabledReasons,
			"evaluator.model is empty: set a model (or configure a coder model so one can be seeded); the LLM judge cannot run without it")
		return
	}
	gc := config.Get()
	if gc == nil {
		return
	}
	providerName := cfg.Provider
	if providerName == "" {
		if m, ok := models.SupportedModels()[models.ModelID(model)]; ok {
			providerName = string(m.Provider)
		}
	}
	if providerName == "" {
		return
	}
	for name, p := range gc.Providers {
		if !strings.EqualFold(string(name), providerName) {
			continue
		}
		if p.Disabled {
			r.DisabledReasons = append(r.DisabledReasons, fmt.Sprintf("provider %q is disabled, so the judge cannot run", providerName))
		} else if strings.TrimSpace(p.APIKey) == "" {
			r.Warnings = append(r.Warnings, fmt.Sprintf("provider %q has no API key configured (fine for keyless or OAuth providers)", providerName))
		}
		return
	}
	r.DisabledReasons = append(r.DisabledReasons, fmt.Sprintf("provider %q is not configured, so the judge cannot run", providerName))
}

func diagnoseVariants(cfg config.EvaluatorConfig, workDir string) DoctorVariants {
	v := DoctorVariants{Enabled: cfg.Enabled && cfg.Templates.Enabled}
	roots := prompt.VariantRoots(workDir)
	files := prompt.DiscoverAllVariants(roots)
	perSection := map[string]int{}
	for _, f := range files {
		perSection[f.Section]++
	}
	for section, n := range perSection {
		sv := SectionVariants{Section: section, Files: n, Competing: n >= 1}
		if sv.Competing {
			v.Competing++
		}
		v.Sections = append(v.Sections, sv)
	}
	sort.Slice(v.Sections, func(i, j int) bool { return v.Sections[i].Section < v.Sections[j].Section })
	for _, root := range roots {
		if dirExists(root) {
			v.Directories = append(v.Directories, root)
		}
	}
	return v
}

func diagnoseSkills(workDir string, collect func(string, error)) DoctorSkills {
	sk := DoctorSkills{Directory: LearnedSkillsDir(workDir)}
	if workDir == "" {
		return sk
	}
	files, err := ReadLearnedSkills(workDir)
	if err != nil {
		collect("learned skills", err)
		return sk
	}
	for _, f := range files {
		switch f.Status {
		case SkillStatusApproved:
			sk.Approved++
		case SkillStatusRejected:
			sk.Rejected++
		default:
			sk.Pending++
		}
	}
	return sk
}

// diagnoseWarnings derives the actionable warnings from the collected facts.
func diagnoseWarnings(r *Report, cfg config.EvaluatorConfig) []string {
	var w []string
	if !r.Enabled {
		return w
	}
	if n := len(r.PatternIssues); n > 0 {
		w = append(w, fmt.Sprintf("%d pattern issue(s), see Pattern lint above: patterns with a literal double backslash never match what was meant", n))
	}
	if r.RecentWindow > 0 && r.RecentEvaluated == 0 {
		w = append(w, fmt.Sprintf("nothing evaluated among the last %d eligible sessions: check that triggers fire (session switch, idle sweeper, backfill) and the last evaluation error", r.RecentWindow))
	}
	if r.LastEvaluationError != nil {
		w = append(w, "last evaluation error: "+r.LastEvaluationError.Message)
	}
	if r.Judge.LastError != nil {
		w = append(w, "last judge error: "+r.Judge.LastError.Message)
	}
	if r.Judge.BudgetExhausted {
		w = append(w, "daily judge budget exhausted: no more judge calls until tomorrow")
	}
	if r.Skills.Pending > 0 {
		w = append(w, fmt.Sprintf("%d learned skill(s) waiting for review (pando skills list --status pending)", r.Skills.Pending))
	}
	return w
}

// LintPatterns checks the correction and task patterns. It flags patterns that
// do not compile and patterns containing a literal double backslash, which
// almost always come from a TOML single-quoted string ('\\b') or an escaped
// pattern in a double-quoted one written with four backslashes; the regex then
// matches a backslash followed by "b" instead of a word boundary.
func LintPatterns(cfg config.EvaluatorConfig) []PatternIssue {
	var out []PatternIssue
	check := func(source, p, prefix string) {
		if strings.TrimSpace(p) == "" {
			return
		}
		if _, err := regexp.Compile(prefix + p); err != nil {
			out = append(out, PatternIssue{
				Kind: PatternIssueCompile, Source: source, Pattern: p,
				Message: fmt.Sprintf("pattern '%s' does not compile: %v", p, err),
				Hint:    "fix the regular expression (Go RE2 syntax); an evaluator with an invalid correction pattern fails to start",
			})
		}
		if strings.Contains(p, `\\`) {
			out = append(out, PatternIssue{
				Kind: PatternIssueDoubleBackslash, Source: source, Pattern: p,
				Message: fmt.Sprintf("pattern '%s' contains a literal double backslash: it matches a backslash character, not a word boundary or class", p),
				Hint:    `use a single backslash inside a TOML single-quoted string ('(?i)\bwrong\b'), or double every backslash only inside a double-quoted string ("(?i)\\bwrong\\b")`,
			})
		}
	}
	for _, p := range cfg.CorrectionsPatterns {
		if isLegacyBareNegation(p) {
			continue
		}
		check("evaluator.correctionsPatterns", p, "")
	}
	for _, tp := range cfg.TaskPatterns {
		check("evaluator.taskPatterns", tp.Pattern, "(?i)")
	}
	return out
}

// Healthy reports whether the report has no disabled reasons and no warnings.
func (r *Report) Healthy() bool {
	return r != nil && r.Enabled && len(r.DisabledReasons) == 0 && len(r.Warnings) == 0
}

// HasProblem reports whether a banner should be shown: the loop is enabled but
// nothing was evaluated among the recent sessions, or a pattern is broken.
func (r *Report) HasProblem() bool {
	if r == nil || !r.Enabled {
		return false
	}
	return len(r.PatternIssues) > 0 || (r.RecentWindow > 0 && r.RecentEvaluated == 0)
}

// ProblemLine is the one-line warning shown in the TUI and the WebUI, or "".
func (r *Report) ProblemLine() string {
	if !r.HasProblem() {
		return ""
	}
	if n := len(r.PatternIssues); n > 0 {
		return fmt.Sprintf("Evaluator: %d broken pattern(s) (%s). Run `pando evaluator doctor`.", n, r.PatternIssues[0].Message)
	}
	return fmt.Sprintf("Evaluator is enabled but none of the last %d eligible sessions was evaluated. Run `pando evaluator doctor`.", r.RecentWindow)
}

// Summary renders the one-paragraph startup summary.
func (r *Report) Summary() string {
	if r == nil {
		return "evaluator: no report"
	}
	state := "disabled"
	if r.Enabled {
		state = "enabled"
	}
	last := "never"
	if r.LastEvaluatedAt > 0 {
		last = time.Unix(r.LastEvaluatedAt, 0).Format("2006-01-02 15:04")
	}
	return fmt.Sprintf("evaluator %s: %d eligible session(s), %d evaluated, %d never evaluated, last evaluation %s, judge %d/%s calls today, %d variant section(s) competing, %d pending skill(s), %d pattern issue(s)",
		state, r.EligibleSessions, r.EvaluatedSessions, r.NeverEvaluated, last,
		r.Judge.CallsToday, limitText(int64(r.Judge.DailyCalls)), r.Variants.Competing, r.Skills.Pending, len(r.PatternIssues))
}

func limitText(n int64) string {
	if n <= 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%d", n)
}

// Text renders the full human-readable report used by `pando evaluator doctor`.
func (r *Report) Text() string {
	var b strings.Builder
	line := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	yn := func(v bool) string {
		if v {
			return "yes"
		}
		return "no"
	}
	ts := func(unix int64) string {
		if unix <= 0 {
			return "never"
		}
		return time.Unix(unix, 0).Format("2006-01-02 15:04:05")
	}

	state := "DISABLED"
	if r.Enabled && len(r.DisabledReasons) == 0 {
		state = "enabled"
	} else if r.Enabled {
		state = "enabled, but cannot fully run"
	}
	line("Evaluator: %s", state)
	if r.Model != "" {
		line("  model: %s (provider %s)", r.Model, firstNonEmpty(r.Provider, "auto"))
	}
	for _, d := range r.DisabledReasons {
		line("  why: %s", d)
	}

	line("")
	line("Sessions")
	if r.SessionsAvailable {
		line("  eligible (>= %d user turns): %d", minUserTurns, r.EligibleSessions)
		line("  evaluated: %d", r.EvaluatedSessions)
		line("  never evaluated: %d", r.NeverEvaluated)
		line("  last %d idle eligible sessions evaluated: %d", r.RecentWindow, r.RecentEvaluated)
		line("  last evaluation: %s", ts(r.LastEvaluatedAt))
	} else {
		line("  database not available")
	}
	if r.LastEvaluationError != nil {
		line("  last evaluation error: %s (%s)", r.LastEvaluationError.Message, ts(r.LastEvaluationError.At))
	} else if r.Background.Known {
		line("  last evaluation error: none since start")
	} else {
		line("  last evaluation error: unknown (only tracked by a running instance; see the log or the WebUI)")
	}

	line("")
	line("Judge")
	line("  model configured: %s", yn(r.Judge.Configured))
	line("  runs for reward >= %.2f or <= %.2f with >= %d user turns (transcript cap %d tokens)",
		r.Judge.HighReward, r.Judge.LowReward, r.Judge.MinTurns, r.Judge.MaxTranscriptTokens)
	line("  budget today: %d/%s calls, %d/%s tokens%s", r.Judge.CallsToday, limitText(int64(r.Judge.DailyCalls)),
		r.Judge.TokensToday, limitText(r.Judge.DailyTokens), map[bool]string{true: " (EXHAUSTED)", false: ""}[r.Judge.BudgetExhausted])
	line("  judge during backfill: %s", yn(r.Judge.BackfillJudge))
	if r.Judge.LastError != nil {
		line("  last judge error: %s (%s)", r.Judge.LastError.Message, ts(r.Judge.LastError.At))
	} else if r.Background.Known {
		line("  last judge error: none since start")
	}

	line("")
	line("Prompt variants (templates %s)", map[bool]string{true: "on", false: "off"}[r.Variants.Enabled])
	if len(r.Variants.Directories) == 0 {
		line("  no variants directory found (create .pando/prompts/variants/<section>/<name>.md.tpl)")
	}
	for _, d := range r.Variants.Directories {
		line("  directory: %s", d)
	}
	for _, s := range r.Variants.Sections {
		line("  section %s: %d variant file(s)%s", s.Section, s.Files, map[bool]string{true: " (competing with the default)", false: ""}[s.Competing])
	}

	line("")
	line("Learned skills (%s)", r.Skills.Directory)
	line("  pending %d, approved %d, rejected %d", r.Skills.Pending, r.Skills.Approved, r.Skills.Rejected)

	line("")
	line("Other")
	line("  context trimmer: %s", map[bool]string{true: "on", false: "off"}[r.ContextTrimmer])
	line("  idle timeout %s, backfill limit %d, include subagents %s", r.Background.IdleTimeout, r.Background.BackfillLimit, yn(r.Background.IncludeSubagents))
	if r.Background.Known {
		bs := r.Background
		line("  background worker: started %s, primary %s, backfill done %s (%d scored), last idle sweep %s (%d scored)",
			yn(bs.Started), yn(bs.Primary), yn(bs.BackfillDone), bs.BackfillEvaluated, ts(bs.LastSweepAt), bs.LastSweepEvaluated)
	} else {
		line("  background worker: state only known inside a running instance (the primary owns the sweeper and backfill)")
	}

	if len(r.PatternIssues) > 0 {
		line("")
		line("Pattern lint")
		for _, is := range r.PatternIssues {
			line("  [%s] %s", is.Kind, is.Message)
			line("      fix: %s", is.Hint)
		}
	}
	if len(r.CollectionErrors) > 0 {
		line("")
		line("Collection errors")
		for _, e := range r.CollectionErrors {
			line("  %s", e)
		}
	}

	line("")
	if len(r.Warnings) == 0 && len(r.DisabledReasons) == 0 {
		line("Verdict: healthy")
	} else {
		line("Warnings")
		for _, w := range r.Warnings {
			line("  - %s", w)
		}
		if len(r.Warnings) == 0 {
			line("  (none besides the reasons above)")
		}
	}
	return b.String()
}
