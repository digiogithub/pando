package evaluator

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v2"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
)

// Learned skills are reviewable files under <workdir>/.pando/skills/learned/.
// The files are the source of truth (versioned with the repo, edited by
// humans); the skill_library table only mirrors their status and keeps the
// statistics. The directory sits inside the skills discovery path, but the
// skills loader only reads SKILL.md files and these are named <id>.md.

// Skill review states.
const (
	SkillStatusPending  = "pending"
	SkillStatusApproved = "approved"
	SkillStatusRejected = "rejected"

	// skillStatusLegacy marks a mirror row from before review existed.
	skillStatusLegacy = "legacy"
	// skillStatusDeleted marks a mirror row whose file was removed.
	skillStatusDeleted = "deleted"
)

// skillProposalMinConfidence is the judge confidence needed to propose a skill.
const skillProposalMinConfidence = 0.7

// maxInjectedSkills bounds the skills injected into one session.
const maxInjectedSkills = 10

// maxSkillTitleLen bounds generated skill titles.
const maxSkillTitleLen = 64

// SkillFile is a parsed learned-skill file.
type SkillFile struct {
	ID            string
	Title         string
	Status        string
	TaskType      string
	Confidence    float64
	SourceSession string
	JudgeModel    string
	Created       time.Time
	// Content is the rule text (the file body).
	Content string
	// Path is the absolute file path.
	Path string
}

// skillFrontMatter is the YAML header of a skill file.
type skillFrontMatter struct {
	ID            string  `yaml:"id"`
	Title         string  `yaml:"title"`
	Status        string  `yaml:"status"`
	TaskType      string  `yaml:"task_type"`
	Confidence    float64 `yaml:"confidence"`
	SourceSession string  `yaml:"source_session,omitempty"`
	JudgeModel    string  `yaml:"judge_model,omitempty"`
	Created       string  `yaml:"created"`
}

// LearnedSkillsDir returns the directory holding the learned-skill files.
func LearnedSkillsDir(workDir string) string {
	return filepath.Join(workDir, ".pando", "skills", "learned")
}

// ValidSkillStatus reports whether s is a skill review state.
func ValidSkillStatus(s string) bool {
	return s == SkillStatusPending || s == SkillStatusApproved || s == SkillStatusRejected
}

// Marshal renders the skill file (YAML front matter + rule text).
func (f SkillFile) Marshal() []byte {
	created := f.Created
	if created.IsZero() {
		created = time.Now()
	}
	fm, _ := yaml.Marshal(skillFrontMatter{
		ID: f.ID, Title: f.Title, Status: f.Status, TaskType: f.TaskType,
		Confidence: f.Confidence, SourceSession: f.SourceSession, JudgeModel: f.JudgeModel,
		Created: created.UTC().Format(time.RFC3339),
	})
	var b bytes.Buffer
	b.WriteString("---\n")
	b.Write(fm)
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(f.Content))
	b.WriteString("\n")
	return b.Bytes()
}

// parseSkillFile parses a skill file. id is the file name without extension.
func parseSkillFile(id string, data []byte) (SkillFile, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return SkillFile{}, errors.New("missing front matter")
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return SkillFile{}, errors.New("unterminated front matter")
	}
	var fm skillFrontMatter
	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		return SkillFile{}, fmt.Errorf("front matter: %w", err)
	}
	body := strings.TrimSpace(strings.TrimPrefix(rest[end+len("\n---"):], "\n"))
	if body == "" {
		return SkillFile{}, errors.New("empty rule text")
	}
	f := SkillFile{
		ID: id, Title: fm.Title, Status: strings.ToLower(strings.TrimSpace(fm.Status)),
		TaskType: fm.TaskType, Confidence: fm.Confidence, SourceSession: fm.SourceSession,
		JudgeModel: fm.JudgeModel, Content: body,
	}
	if !ValidSkillStatus(f.Status) {
		f.Status = SkillStatusPending // an unknown status is never injected
	}
	if f.TaskType == "" {
		f.TaskType = "general"
	}
	if f.Title == "" {
		f.Title = skillTitle(body)
	}
	if t, err := time.Parse(time.RFC3339, fm.Created); err == nil {
		f.Created = t
	}
	return f, nil
}

// ReadLearnedSkills reads every skill file of workDir, newest first. Files that
// cannot be parsed are skipped with a warning.
func ReadLearnedSkills(workDir string) ([]SkillFile, error) {
	if workDir == "" {
		return nil, nil
	}
	dir := LearnedSkillsDir(workDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []SkillFile
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		f, err := parseSkillFile(strings.TrimSuffix(name, ".md"), data)
		if err != nil {
			slog.Warn("evaluator: skipping unreadable learned skill", "path", path, "err", err)
			continue
		}
		f.Path = path
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Created.Equal(out[j].Created) {
			return out[i].Created.After(out[j].Created)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// writeSkillFile writes f atomically to <dir>/<f.ID>.md.
func writeSkillFile(workDir string, f SkillFile) (string, error) {
	dir := LearnedSkillsDir(workDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, f.ID+".md")
	tmp, err := os.CreateTemp(dir, ".skill-*.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(f.Marshal()); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	return path, nil
}

// resolveSkillFile finds a skill by exact id or unique id prefix.
func resolveSkillFile(files []SkillFile, id string) (SkillFile, error) {
	id = strings.TrimSpace(strings.TrimSuffix(id, ".md"))
	if id == "" {
		return SkillFile{}, errors.New("skill id is required")
	}
	var matches []SkillFile
	for _, f := range files {
		if f.ID == id {
			return f, nil
		}
		if strings.HasPrefix(f.ID, id) {
			matches = append(matches, f)
		}
	}
	switch len(matches) {
	case 0:
		return SkillFile{}, fmt.Errorf("learned skill %q not found", id)
	case 1:
		return matches[0], nil
	}
	return SkillFile{}, fmt.Errorf("skill id %q is ambiguous (%d matches)", id, len(matches))
}

// SetSkillFileStatus sets the status of a learned-skill file (approve/reject).
// It touches the file only; the database mirror is refreshed by the next sync.
func SetSkillFileStatus(workDir, id, status string) (SkillFile, error) {
	if !ValidSkillStatus(status) {
		return SkillFile{}, fmt.Errorf("invalid skill status %q", status)
	}
	files, err := ReadLearnedSkills(workDir)
	if err != nil {
		return SkillFile{}, err
	}
	f, err := resolveSkillFile(files, id)
	if err != nil {
		return SkillFile{}, err
	}
	if f.Status == status {
		return f, nil
	}
	f.Status = status
	if _, err := writeSkillFile(workDir, f); err != nil {
		return SkillFile{}, err
	}
	return f, nil
}

// skillTitle derives a short title from a rule text: its first sentence,
// stripped of list/markdown markers and truncated at a word boundary.
func skillTitle(content string) string {
	s := strings.TrimSpace(content)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimLeft(s, "-*#> \t0123456789.)")
	s = strings.NewReplacer("`", "", "**", "").Replace(s)
	s = strings.TrimSpace(s)
	for i, r := range s {
		if (r == '.' || r == ';' || r == '!' || r == '?') && i > 12 {
			s = s[:i]
			break
		}
	}
	if len([]rune(s)) > maxSkillTitleLen {
		r := []rune(s)[:maxSkillTitleLen]
		t := string(r)
		if i := strings.LastIndexByte(t, ' '); i > maxSkillTitleLen/2 {
			t = t[:i]
		}
		s = strings.TrimRight(t, " ,:-") + "..."
	}
	if s == "" {
		return "Learned rule"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// skillSlug builds a file-name slug from a title.
func skillSlug(title string) string {
	var b strings.Builder
	dash := true
	for _, r := range strings.ToLower(title) {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= 48 {
			break
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" || s == "skill" {
		s = "learned-rule"
	}
	return s
}

// uniqueSkillID returns slug, or slug-N, so that no file <id>.md exists yet.
func uniqueSkillID(workDir, slug string) string {
	id := slug
	for n := 2; ; n++ {
		if _, err := os.Stat(filepath.Join(LearnedSkillsDir(workDir), id+".md")); errors.Is(err, os.ErrNotExist) {
			return id
		}
		id = fmt.Sprintf("%s-%d", slug, n)
	}
}

// workDir returns the project directory holding .pando/skills/learned.
func (s *EvaluatorService) workDir() string {
	s.mu.Lock()
	wd := s.workDirOverride
	s.mu.Unlock()
	if wd != "" {
		return wd
	}
	if c := config.Get(); c != nil {
		return c.WorkingDir
	}
	return ""
}

// SetWorkDir overrides the project directory used for learned-skill files.
func (s *EvaluatorService) SetWorkDir(dir string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.workDirOverride = dir
	s.mu.Unlock()
}

func skillFromRow(r db.SkillLibrary) Skill {
	return Skill{
		ID: r.ID, Title: r.Title, Content: r.Content, TaskType: r.TaskType,
		SuccessRate: r.SuccessRate, UsageCount: int(r.UsageCount), EvalCount: int(r.EvalCount),
		Status: r.Status, Confidence: r.Confidence, JudgeModel: r.JudgeModel,
		SourceSession: r.SourceSessionID.String,
	}
}

func mirrorParams(f SkillFile) db.UpsertSkillMirrorParams {
	active := int64(0)
	if f.Status == SkillStatusApproved {
		active = 1
	}
	created := f.Created.Unix()
	if f.Created.IsZero() {
		created = time.Now().Unix()
	}
	return db.UpsertSkillMirrorParams{
		ID: f.ID, Title: f.Title, Content: f.Content,
		SourceSessionID: sql.NullString{String: f.SourceSession, Valid: f.SourceSession != ""},
		TaskType:        f.TaskType, IsActive: active, Status: f.Status,
		Confidence: f.Confidence, JudgeModel: f.JudgeModel, CreatedAt: created,
	}
}

// SyncLearnedSkills mirrors the skill files into skill_library: new or changed
// files are upserted (status decides is_active), mirror rows whose file is gone
// are deactivated, and pre-review rows are exported as pending files. Only rows
// that differ are written, so calling it on every new session is cheap.
func (s *EvaluatorService) SyncLearnedSkills(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	wd := s.workDir()
	if wd == "" {
		return nil
	}
	s.skillFileMu.Lock()
	defer s.skillFileMu.Unlock()
	return s.syncLocked(ctx, wd)
}

func (s *EvaluatorService) syncLocked(ctx context.Context, wd string) error {
	files, err := ReadLearnedSkills(wd)
	if err != nil {
		return fmt.Errorf("evaluator: read learned skills: %w", err)
	}
	rows, err := s.db.ListAllSkills(ctx)
	if err != nil {
		return fmt.Errorf("evaluator: list skill mirror: %w", err)
	}
	byID := make(map[string]db.SkillLibrary, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	inFiles := make(map[string]bool, len(files))
	for _, f := range files {
		inFiles[f.ID] = true
		p := mirrorParams(f)
		r, ok := byID[f.ID]
		if ok && r.Status == f.Status && r.IsActive == p.IsActive && r.Title == f.Title &&
			r.Content == f.Content && r.TaskType == f.TaskType {
			continue
		}
		if err := s.db.UpsertSkillMirror(ctx, p); err != nil {
			return fmt.Errorf("evaluator: mirror skill %s: %w", f.ID, err)
		}
	}
	for _, r := range rows {
		if inFiles[r.ID] || r.Status == skillStatusDeleted {
			continue
		}
		if r.Status == skillStatusLegacy {
			f := SkillFile{
				ID: uniqueSkillID(wd, r.ID), Title: skillTitle(r.Content), Status: SkillStatusPending,
				TaskType: r.TaskType, Confidence: r.Confidence, SourceSession: r.SourceSessionID.String,
				JudgeModel: r.JudgeModel, Created: time.Unix(r.CreatedAt, 0), Content: r.Content,
			}
			if _, err := writeSkillFile(wd, f); err != nil {
				slog.Warn("evaluator: export legacy skill failed", "id", r.ID, "err", err)
				continue
			}
			slog.Info("evaluator: legacy skill exported for review", "id", f.ID)
			continue // the new file is mirrored on the next sync
		}
		if err := s.db.SetSkillMirrorState(ctx, db.SetSkillMirrorStateParams{
			Status: skillStatusDeleted, IsActive: 0, ID: r.ID,
		}); err != nil {
			return fmt.Errorf("evaluator: deactivate skill %s: %w", r.ID, err)
		}
	}
	return nil
}

// proposeSkill writes a judge proposal as a pending skill file and mirrors it.
// It returns false when the rule duplicates an existing skill of any status.
func (s *EvaluatorService) proposeSkill(ctx context.Context, wd string, out *JudgeOutput, sessionID, model string) (bool, error) {
	s.skillFileMu.Lock()
	defer s.skillFileMu.Unlock()

	existing, err := ReadLearnedSkills(wd)
	if err != nil {
		return false, err
	}
	contents := make([]string, 0, len(existing))
	for _, f := range existing {
		contents = append(contents, f.Content)
	}
	if isDuplicateSkillText(out.NewSkill, contents) {
		return false, nil
	}
	taskType := out.TaskType
	if taskType == "" {
		taskType = "general"
	}
	title := skillTitle(out.NewSkill)
	f := SkillFile{
		ID: uniqueSkillID(wd, skillSlug(title)), Title: title, Status: SkillStatusPending,
		TaskType: taskType, Confidence: out.Confidence, SourceSession: sessionID,
		JudgeModel: model, Created: time.Now(), Content: out.NewSkill,
	}
	if _, err := writeSkillFile(wd, f); err != nil {
		return false, err
	}
	if err := s.db.UpsertSkillMirror(ctx, mirrorParams(f)); err != nil {
		slog.Warn("evaluator: mirror new skill failed (next sync will retry)", "id", f.ID, "err", err)
	}
	slog.Info("evaluator: skill proposed for review", "id", f.ID, "task_type", taskType, "confidence", out.Confidence)
	return true, nil
}

// ListSkills returns the learned skills (files joined with their statistics),
// optionally filtered by status ("" = all) and task type ("" = all; general
// skills always match a task type filter).
func (s *EvaluatorService) ListSkills(ctx context.Context, status, taskType string) ([]Skill, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if status != "" && !ValidSkillStatus(status) {
		return nil, fmt.Errorf("invalid skill status %q (want pending, approved or rejected)", status)
	}
	wd := s.workDir()
	s.skillFileMu.Lock()
	err := s.syncLocked(ctx, wd)
	files, ferr := ReadLearnedSkills(wd)
	s.skillFileMu.Unlock()
	if err != nil {
		slog.Debug("evaluator: skill sync before list failed", "err", err)
	}
	if ferr != nil {
		return nil, ferr
	}
	rows, _ := s.db.ListAllSkills(ctx)
	stats := make(map[string]db.SkillLibrary, len(rows))
	for _, r := range rows {
		stats[r.ID] = r
	}
	out := make([]Skill, 0, len(files))
	for _, f := range files {
		if status != "" && f.Status != status {
			continue
		}
		if taskType != "" && f.TaskType != taskType && f.TaskType != "general" {
			continue
		}
		sk := Skill{
			ID: f.ID, Title: f.Title, Content: f.Content, TaskType: f.TaskType,
			Status: f.Status, Confidence: f.Confidence, JudgeModel: f.JudgeModel,
			SourceSession: f.SourceSession, Created: f.Created,
		}
		if r, ok := stats[f.ID]; ok {
			sk.SuccessRate, sk.UsageCount, sk.EvalCount = r.SuccessRate, int(r.UsageCount), int(r.EvalCount)
		}
		out = append(out, sk)
	}
	// Review queue first: pending, approved, rejected (files are newest first).
	rank := map[string]int{SkillStatusPending: 0, SkillStatusApproved: 1, SkillStatusRejected: 2}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Status] < rank[out[j].Status] })
	return out, nil
}

// ReviewSkill approves or rejects a learned skill: it updates the file (source
// of truth) and the mirror. Approving beyond MaxSkills evicts the lowest
// ranked approved skill (marked rejected so it is not proposed again).
func (s *EvaluatorService) ReviewSkill(ctx context.Context, id, status string) (*Skill, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("evaluator is not enabled")
	}
	if status != SkillStatusApproved && status != SkillStatusRejected {
		return nil, fmt.Errorf("invalid review status %q (want approved or rejected)", status)
	}
	wd := s.workDir()
	if wd == "" {
		return nil, errors.New("no project directory for learned skills")
	}
	s.skillFileMu.Lock()
	defer s.skillFileMu.Unlock()

	f, err := SetSkillFileStatus(wd, id, status)
	if err != nil {
		return nil, err
	}
	if err := s.syncLocked(ctx, wd); err != nil {
		return nil, err
	}
	if status == SkillStatusApproved {
		s.enforceSkillLimitLocked(ctx, wd, f.ID)
	}
	sk := Skill{ID: f.ID, Title: f.Title, Content: f.Content, TaskType: f.TaskType, Status: f.Status,
		Confidence: f.Confidence, JudgeModel: f.JudgeModel, SourceSession: f.SourceSession, Created: f.Created}
	if r, err := s.db.GetSkill(ctx, f.ID); err == nil {
		sk.SuccessRate, sk.UsageCount, sk.EvalCount = r.SuccessRate, int(r.UsageCount), int(r.EvalCount)
	}
	return &sk, nil
}

// enforceSkillLimitLocked keeps the approved skills within MaxSkills, evicting
// the lowest ranked one (success_rate, then usage) other than keep.
func (s *EvaluatorService) enforceSkillLimitLocked(ctx context.Context, wd, keep string) {
	if s.cfg.MaxSkills <= 0 {
		return
	}
	rows, err := s.db.ListAllActiveSkills(ctx) // success_rate DESC, usage_count DESC
	if err != nil {
		return
	}
	for len(rows) > s.cfg.MaxSkills {
		victim := rows[len(rows)-1]
		rows = rows[:len(rows)-1]
		if victim.ID == keep {
			continue
		}
		if _, err := SetSkillFileStatus(wd, victim.ID, SkillStatusRejected); err != nil {
			slog.Warn("evaluator: evict skill failed", "id", victim.ID, "err", err)
			continue
		}
		slog.Info("evaluator: skill evicted (limit reached)", "id", victim.ID, "success_rate", victim.SuccessRate)
	}
	_ = s.syncLocked(ctx, wd)
}

// pruneUnderperformingLocked rejects approved skills that were evaluated enough
// times and still average a low reward.
func (s *EvaluatorService) pruneUnderperformingLocked(ctx context.Context, wd string) {
	rows, err := s.db.ListAllActiveSkills(ctx)
	if err != nil {
		return
	}
	changed := false
	for _, r := range rows {
		if r.EvalCount >= 5 && r.SuccessRate < 0.3 {
			if _, err := SetSkillFileStatus(wd, r.ID, SkillStatusRejected); err == nil {
				slog.Info("evaluator: underperforming skill rejected", "id", r.ID, "success_rate", r.SuccessRate)
				changed = true
			}
		}
	}
	if changed {
		_ = s.syncLocked(ctx, wd)
	}
}
