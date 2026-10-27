package chat

import (
	"encoding/json"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/i18n"
	"github.com/egoist/mygo/ui"
)

// ItemKind is the kind of a transcript row.
type ItemKind int

// Transcript row kinds.
const (
	ItemUser ItemKind = iota
	ItemAssistant
	ItemNotice
	ItemError
	ItemRouting
	ItemContextFilter
	ItemPersona
)

// Tool is a tool call shown in an assistant turn, with its result once it
// arrives.
type Tool struct {
	ID      string
	Name    string
	Title   string
	Kind    string
	Status  string
	Input   string
	Output  string
	IsError bool
	Open    bool

	Locations []client.Location
	Diff      *client.ToolDiff
	Terminal  *client.ToolTerminal
	// Images are data URLs of image results (screenshots).
	Images []string

	parsedFor string
	parsed    map[string]any
}

// Label is the text shown for the call.
func (t *Tool) Label() string {
	if t.Title != "" && t.Title != t.Name {
		return t.Title
	}
	return t.Name
}

// InputMap returns the call's input as a JSON object (nil if it is not one).
func (t *Tool) InputMap() map[string]any {
	if t.parsedFor == t.Input && t.parsed != nil {
		return t.parsed
	}
	t.parsedFor, t.parsed = t.Input, nil
	var m map[string]any
	if t.Input != "" && json.Unmarshal([]byte(t.Input), &m) == nil {
		t.parsed = m
	}
	return t.parsed
}

// PartKind is the kind of a part of an assistant turn.
type PartKind int

// Assistant turn parts, in the order they were produced.
const (
	PText PartKind = iota
	PThinking
	PTool
)

// Part is one piece of an assistant turn.
type Part struct {
	Key  int
	Kind PartKind
	Text string
	Tool *Tool
	// Open: a thinking part is expanded.
	Open bool
}

// Item is a row of the transcript.
type Item struct {
	// Key is stable for the life of the row (list identity).
	Key  int
	Kind ItemKind
	// Text is the text of user/notice/error/persona rows.
	Text string
	// Images are data URLs attached to a user message.
	Images []string
	// Parts is the assistant turn: text, thinking and tool calls in order.
	Parts []*Part
	// Done is false while an assistant row is still streaming.
	Done  bool
	Model string
	Time  time.Time

	Routing       *client.RoutingInfo
	ContextFilter *client.ContextFilterInfo

	// Open: a collapsible row (persona, context filter) is expanded.
	Open bool
	// Group expansion state, keyed by the part key that starts the group.
	GroupOpen map[int]bool
}

// PlainText is the text of the row for copying: the text parts of an
// assistant turn joined by blank lines.
func (it *Item) PlainText() string {
	if it.Kind != ItemAssistant {
		return it.Text
	}
	var out []string
	for _, p := range it.Parts {
		if p.Kind == PText && strings.TrimSpace(p.Text) != "" {
			out = append(out, p.Text)
		}
	}
	return strings.Join(out, "\n\n")
}

// Tools lists the tool calls of an assistant turn.
func (it *Item) Tools() []*Tool {
	var out []*Tool
	for _, p := range it.Parts {
		if p.Kind == PTool {
			out = append(out, p.Tool)
		}
	}
	return out
}

// FileChange is a file the session modified (the modified-files panel).
type FileChange struct {
	Path      string
	Name      string
	Additions int
	Removals  int
	// FromVCS: the entry came from agent-vcs only (no inline edits).
	FromVCS bool
	Edits   []Edit
	Updated int
}

// Edit is one old -> new replacement of a file.
type Edit struct{ Old, New string }

// Conversation is the transcript and run state of one session.
type Conversation struct {
	// ID is empty until the server assigns the session (new chat).
	ID    string
	Items []*Item
	// List is the transcript list state (follows the end).
	List ui.ListState

	// Loaded reports the history was fetched; Loading while it is.
	Loaded, Loading bool
	LoadErr         string

	// Running: a run is active server side (own, or followed).
	Running bool
	// Streaming: this client has a stream open for the run.
	Streaming bool
	// RunSeq is the last run sequence seen.
	RunSeq int64
	Usage  *client.TokenUsage
	// ContextWindow is the context size of the session's model (0 unknown).
	ContextWindow int64
	// BusyElsewhere: another process (a subagent) runs the session.
	BusyElsewhere bool

	Plan []client.PlanEntry
	Goal *client.GoalStatus
	// Files are the modified files of the session by path.
	Files map[string]*FileChange
	// AutoApprove mirrors the session's auto-approve state.
	AutoApprove bool
	// Permissions and Questions wait for an answer, oldest first.
	Permissions []client.PermissionRequest
	Questions   []client.QuestionRequest
	// answered remembers resolved request ids so a replayed or polled
	// request does not reopen its dialog.
	answered map[string]bool
	// Queued is feedback sent while the run goes on, not yet injected.
	Queued []string

	// runStart is the index of the first row of the current run, which a
	// reattach (full replay) truncates back to.
	runStart int
	// content reports whether the current run delivered visible content.
	content bool
	// streamTag identifies the stream feeding the conversation; events of
	// older streams (stopped, superseded) are dropped.
	streamTag int
	cancel    func()
	// checking is set while a /pending check is in flight.
	checking bool
	nextKey  int
	tools    map[string]*Tool
	fileSeq  int
}

func newConversation(id string) *Conversation {
	cv := &Conversation{ID: id, tools: map[string]*Tool{}, Files: map[string]*FileChange{}, answered: map[string]bool{}}
	cv.setupList()
	return cv
}

func (cv *Conversation) setupList() {
	cv.List.Key = func(i int) any {
		if i < len(cv.Items) {
			return cv.Items[i].Key
		}
		return i
	}
	cv.List.FollowEnd = true
	cv.List.FollowAnimation = &ui.Smooth
}

func (cv *Conversation) add(it *Item) *Item {
	cv.nextKey++
	it.Key = cv.nextKey
	if it.Time.IsZero() {
		it.Time = time.Now()
	}
	cv.Items = append(cv.Items, it)
	return it
}

func (cv *Conversation) newPart(it *Item, k PartKind) *Part {
	cv.nextKey++
	p := &Part{Key: cv.nextKey, Kind: k}
	it.Parts = append(it.Parts, p)
	return p
}

func (cv *Conversation) indexTools() {
	cv.tools = map[string]*Tool{}
	for _, it := range cv.Items {
		for _, t := range it.Tools() {
			cv.tools[t.ID] = t
		}
	}
}

// SetHistory replaces the transcript with folded history.
func (cv *Conversation) SetHistory(msgs []client.Message) {
	cv.Items = nil
	for _, it := range FoldHistory(msgs) {
		cv.nextKey++
		it.Key = cv.nextKey
		for _, p := range it.Parts {
			cv.nextKey++
			p.Key = cv.nextKey
		}
		cv.Items = append(cv.Items, it)
	}
	cv.indexTools()
	cv.runStart = len(cv.Items)
	cv.Loaded = true
	cv.Files = hydrateFiles(cv.Items)
}

// TrimToLastPrompt drops the rows after the last user prompt: a run being
// attached replays them.
func (cv *Conversation) TrimToLastPrompt() {
	for i := len(cv.Items) - 1; i >= 0; i-- {
		if cv.Items[i].Kind == ItemUser {
			cv.Items = cv.Items[:i+1]
			cv.runStart = i + 1
			cv.indexTools()
			return
		}
	}
	cv.runStart = len(cv.Items)
}

// BeginRun adds the user's prompt and marks a run as started by this client.
func (cv *Conversation) BeginRun(prompt string) {
	cv.add(&Item{Kind: ItemUser, Text: prompt, Done: true})
	cv.runStart = len(cv.Items)
	cv.content = false
	cv.Running, cv.Streaming = true, true
}

// BeginAttach marks the conversation as following a run it did not start.
func (cv *Conversation) BeginAttach() {
	cv.content = false
	cv.Running, cv.Streaming = true, true
}

// assistant returns the open assistant row of the run, creating one.
func (cv *Conversation) assistant() *Item {
	if n := len(cv.Items); n > 0 && n-1 >= cv.runStart {
		if last := cv.Items[n-1]; last.Kind == ItemAssistant && !last.Done {
			return last
		}
	}
	return cv.add(&Item{Kind: ItemAssistant})
}

func (cv *Conversation) appendText(k PartKind, s string) {
	a := cv.assistant()
	if n := len(a.Parts); n > 0 && a.Parts[n-1].Kind == k {
		a.Parts[n-1].Text += s
		return
	}
	cv.newPart(a, k).Text = s
}

func (cv *Conversation) tool(id string) *Tool {
	if t, ok := cv.tools[id]; ok {
		return t
	}
	t := &Tool{ID: id}
	cv.tools[id] = t
	a := cv.assistant()
	cv.newPart(a, PTool).Tool = t
	return t
}

// closeAssistant ends the open assistant row so later content opens a new
// one.
func (cv *Conversation) closeAssistant() {
	if n := len(cv.Items); n > 0 {
		if last := cv.Items[n-1]; last.Kind == ItemAssistant {
			last.Done = true
		}
	}
}

// insertBeforeAssistant adds a muted row (routing, context filter) before
// the assistant turn being streamed, as the Web UI does (insertBeforeLast).
func (cv *Conversation) insertBeforeAssistant(it *Item) {
	cv.nextKey++
	it.Key = cv.nextKey
	it.Done = true
	it.Time = time.Now()
	if n := len(cv.Items); n > 0 && cv.Items[n-1].Kind == ItemAssistant && !cv.Items[n-1].Done {
		cv.Items = append(cv.Items, nil)
		copy(cv.Items[n:], cv.Items[n-1:])
		cv.Items[n-1] = it
		cv.runStart = min(cv.runStart, n-1)
		return
	}
	cv.Items = append(cv.Items, it)
}

// hasPermission reports whether a request id is queued or was answered.
func (cv *Conversation) seenRequest(id string) bool {
	if cv.answered[id] {
		return true
	}
	for _, p := range cv.Permissions {
		if p.ID == id {
			return true
		}
	}
	for _, q := range cv.Questions {
		if q.ID == id {
			return true
		}
	}
	return false
}

// AddPermission queues a permission request unless already seen.
func (cv *Conversation) AddPermission(p client.PermissionRequest) {
	if p.ID != "" && !cv.seenRequest(p.ID) {
		cv.Permissions = append(cv.Permissions, p)
	}
}

// AddQuestion queues a question request unless already seen.
func (cv *Conversation) AddQuestion(q client.QuestionRequest) {
	if q.ID != "" && len(q.Questions) > 0 && !cv.seenRequest(q.ID) {
		cv.Questions = append(cv.Questions, q)
	}
}

// Resolve removes an answered request from the queues.
func (cv *Conversation) Resolve(id string) {
	cv.answered[id] = true
	cv.Permissions = filter(cv.Permissions, func(p client.PermissionRequest) bool { return p.ID != id })
	cv.Questions = filter(cv.Questions, func(q client.QuestionRequest) bool { return q.ID != id })
}

func filter[T any](s []T, keep func(T) bool) []T {
	out := s[:0:0]
	for _, v := range s {
		if keep(v) {
			out = append(out, v)
		}
	}
	return out
}

// MergePending adopts the requests of GET /pending (the poll that catches
// prompts a dropped stream never delivered) and forgets queued ones the
// server no longer lists.
func (cv *Conversation) MergePending(p client.Pending) {
	live := map[string]bool{}
	for _, r := range p.Permissions {
		live[r.ID] = true
		cv.AddPermission(r)
	}
	for _, q := range p.Questions {
		live[q.ID] = true
		cv.AddQuestion(q)
	}
	cv.Permissions = filter(cv.Permissions, func(r client.PermissionRequest) bool { return live[r.ID] })
	cv.Questions = filter(cv.Questions, func(q client.QuestionRequest) bool { return live[q.ID] })
}

// Apply folds a stream event into the transcript. It reports whether the
// history should be reloaded (a finished run whose replay brought nothing).
func (cv *Conversation) Apply(ev client.StreamEvent) (reload bool) {
	if ev.HasContent() {
		cv.content = true
	}
	switch ev.Type {
	case client.EventSession:
		if ev.SessionID != "" && cv.ID == "" {
			cv.ID = ev.SessionID
		}
	case client.EventRun:
		cv.RunSeq = max(cv.RunSeq, ev.RunSeq)
	case client.EventReattach:
		if cv.runStart <= len(cv.Items) {
			cv.Items = cv.Items[:cv.runStart]
		}
		cv.indexTools()
		cv.content = false
	case client.EventContentDelta:
		cv.appendText(PText, ev.Text)
	case client.EventThinkingDelta:
		cv.appendText(PThinking, ev.Text)
	case client.EventToolCall:
		if tc := ev.ToolCall; tc != nil {
			t := cv.tool(tc.ID)
			t.Name, t.Kind, t.Title, t.Input = tc.Name, tc.Kind, tc.Title, tc.Input
			t.Locations = tc.Locations
			if tc.Status != "" {
				t.Status = tc.Status
			}
		}
	case client.EventToolCallUpdate:
		if u := ev.ToolCallUpdate; u != nil {
			t := cv.tool(u.ID)
			if u.Status != "" {
				t.Status = u.Status
			}
			if u.Kind != "" {
				t.Kind = u.Kind
			}
			if u.Title != "" {
				t.Title = u.Title
			}
			if u.Input != "" {
				t.Input = u.Input
			}
			if len(u.Locations) > 0 {
				t.Locations = u.Locations
			}
		}
	case client.EventToolResult:
		if r := ev.ToolResult; r != nil {
			t := cv.tool(r.ToolCallID)
			if t.Name == "" {
				t.Name = r.Name
			}
			if r.Kind != "" {
				t.Kind = r.Kind
			}
			if r.Title != "" {
				t.Title = r.Title
			}
			if len(r.Locations) > 0 {
				t.Locations = r.Locations
			}
			t.Output, t.IsError = r.Content, r.IsError
			t.Diff, t.Terminal, t.Images = r.Diff, r.Terminal, r.Images
			switch {
			case r.Status != "":
				t.Status = r.Status
			case r.IsError:
				t.Status = "failed"
			default:
				t.Status = "completed"
			}
			if !r.IsError {
				cv.trackFileChange(r)
			}
		}
	case client.EventPlanUpdate:
		cv.Plan = ev.Plan
	case client.EventGoalStatus:
		cv.Goal = ev.Goal
	case client.EventSystemMessage:
		cv.system(ev)
	case client.EventSteeringQueued:
		cv.Queued = append(cv.Queued, ev.Message)
	case client.EventSteeringInjected:
		cv.closeAssistant()
		cv.Queued = removeFirst(cv.Queued, ev.Message)
		cv.add(&Item{Kind: ItemUser, Text: ev.Message, Done: true})
		cv.runStart = min(cv.runStart, len(cv.Items))
	case client.EventResurrected:
		if !cv.lastIsNotice(i18n.T("chat.notice.resurrected")) {
			cv.insertBeforeAssistant(&Item{Kind: ItemNotice, Text: i18n.T("chat.notice.resurrected")})
		}
	case client.EventConclusionQueued:
		cv.insertBeforeAssistant(&Item{Kind: ItemNotice, Text: i18n.T("chat.notice.conclusionQueued")})
	case client.EventConclusionInjected:
		cv.insertBeforeAssistant(&Item{Kind: ItemNotice, Text: i18n.T("chat.notice.conclusionInjected")})
	case client.EventPermissionRequest:
		if p := ev.PermissionRequest; p != nil {
			cv.AddPermission(*p)
		}
	case client.EventQuestionRequest:
		if q := ev.QuestionRequest; q != nil {
			cv.AddQuestion(*q)
		}
	case client.EventTokenUsage:
		cv.Usage = ev.TokenUsage
		if ev.TokenUsage != nil && ev.TokenUsage.ContextWindow > 0 {
			cv.ContextWindow = ev.TokenUsage.ContextWindow
		}
	case client.EventError:
		if ev.Error != "" {
			cv.closeAssistant()
			cv.add(&Item{Kind: ItemError, Text: ev.Error, Done: true})
		}
	case client.EventDone:
		return cv.Finish()
	}
	return false
}

func removeFirst(s []string, v string) []string {
	for i := range s {
		if s[i] == v {
			return append(s[:i:i], s[i+1:]...)
		}
	}
	return s
}

func (cv *Conversation) lastIsNotice(text string) bool {
	for i := len(cv.Items) - 1; i >= 0 && i >= len(cv.Items)-2; i-- {
		if cv.Items[i].Kind == ItemNotice && cv.Items[i].Text == text {
			return true
		}
	}
	return false
}

// system folds a system_message: a model auto mode routing notice, a context
// filter notice, or a generic notice.
func (cv *Conversation) system(ev client.StreamEvent) {
	text := strings.TrimSpace(ev.Text)
	routing, filter := ev.RoutingInfo(), ev.ContextFilterInfo()
	switch {
	case routing != nil:
		cv.insertBeforeAssistant(&Item{Kind: ItemRouting, Routing: routing, Text: firstNonEmpty(text, routing.Notice)})
	case filter != nil:
		cv.insertBeforeAssistant(&Item{Kind: ItemContextFilter, ContextFilter: filter, Text: firstNonEmpty(text, filter.Notice)})
	case text != "":
		cv.insertBeforeAssistant(&Item{Kind: ItemNotice, Text: text})
	}
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// Finish ends the run: every row is done. It reports whether the run
// delivered no content (a replay of a run already over), in which case the
// history holds the result and should be reloaded.
func (cv *Conversation) Finish() (reload bool) {
	for _, it := range cv.Items[min(cv.runStart, len(cv.Items)):] {
		it.Done = true
	}
	if n := len(cv.Items); n > 0 {
		cv.Items[n-1].Done = true
	}
	cv.Running, cv.Streaming = false, false
	cv.cancel = nil
	cv.Queued = nil
	return !cv.content
}

// editTools are the tools whose input describes a file modification.
var editTools = map[string]bool{"edit": true, "write": true, "patch": true, "multiedit": true}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func (cv *Conversation) addChange(p string, add, rem int, oldS, newS string) {
	cv.fileSeq++
	fc := cv.Files[p]
	if fc == nil {
		fc = &FileChange{Path: p, Name: path.Base(p)}
		cv.Files[p] = fc
	}
	fc.Additions += add
	fc.Removals += rem
	fc.FromVCS = false
	if oldS != "" || newS != "" {
		fc.Edits = append(fc.Edits, Edit{oldS, newS})
	}
	fc.Updated = cv.fileSeq
}

// trackFileChange feeds the modified-files panel from a tool result, as the
// Web UI's fileChangesStore does.
func (cv *Conversation) trackFileChange(r *client.ToolResultEvent) {
	var meta struct {
		Metadata struct {
			FilesChanged []string `json:"files_changed"`
			Additions    *int     `json:"additions"`
			Removals     *int     `json:"removals"`
		} `json:"metadata"`
	}
	if len(r.RawOutput) > 0 {
		_ = json.Unmarshal(r.RawOutput, &meta)
	}
	if strings.EqualFold(r.Name, "patch") {
		for _, p := range meta.Metadata.FilesChanged {
			if p != "" {
				cv.addChange(p, 0, 0, "", "")
			}
		}
	}
	if d := r.Diff; d != nil && d.FilePath != "" {
		add, rem := countLines(d.NewString), countLines(d.OldString)
		if meta.Metadata.Additions != nil {
			add = *meta.Metadata.Additions
		}
		if meta.Metadata.Removals != nil {
			rem = *meta.Metadata.Removals
		}
		cv.addChange(d.FilePath, add, rem, d.OldString, d.NewString)
	}
}

// hydrateFiles rebuilds the modified files from the edit/write/patch tool
// calls of the history.
func hydrateFiles(items []*Item) map[string]*FileChange {
	cv := &Conversation{Files: map[string]*FileChange{}}
	for _, it := range items {
		for _, t := range it.Tools() {
			if !editTools[strings.ToLower(t.Name)] {
				continue
			}
			m := t.InputMap()
			fp, _ := m["file_path"].(string)
			if fp == "" {
				continue
			}
			oldS, _ := m["old_string"].(string)
			newS, _ := m["new_string"].(string)
			if newS == "" {
				newS, _ = m["content"].(string)
			}
			cv.addChange(fp, countLines(newS), countLines(oldS), oldS, newS)
		}
	}
	return cv.Files
}

// MergeVCS adds the paths agent-vcs knows the session touched that the
// history did not name.
func (cv *Conversation) MergeVCS(entries []client.VCSDiffEntry) {
	for _, e := range entries {
		if e.Path == "" || cv.Files[e.Path] != nil {
			continue
		}
		cv.fileSeq++
		cv.Files[e.Path] = &FileChange{Path: e.Path, Name: path.Base(e.Path), FromVCS: true, Updated: cv.fileSeq}
	}
}

// FileList returns the modified files, most recently updated first.
func (cv *Conversation) FileList() []*FileChange {
	out := make([]*FileChange, 0, len(cv.Files))
	for _, f := range cv.Files {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Updated != out[j].Updated {
			return out[i].Updated > out[j].Updated
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// FoldHistory turns stored messages into transcript rows: tool messages are
// folded into the call they answer (as the Web UI does), finish and empty
// parts dropped, and consecutive assistant messages (one per agent step)
// merged into a single turn.
func FoldHistory(msgs []client.Message) []*Item {
	results := map[string]*client.ToolResultPart{}
	for _, m := range msgs {
		for _, p := range m.Parts {
			if p.Kind == client.PartToolResult {
				results[p.ToolResult.ToolCallID] = p.ToolResult
			}
		}
	}
	var out []*Item
	seq := 0
	mkPart := func(k PartKind) *Part { seq++; return &Part{Key: -seq, Kind: k} }
	for _, m := range msgs {
		ts := time.Unix(m.CreatedAt, 0)
		switch m.Role {
		case client.RoleUser:
			it := &Item{Kind: ItemUser, Text: m.Text(), Done: true, Time: ts}
			for _, p := range m.Parts {
				switch p.Kind {
				case client.PartImageURL:
					it.Images = append(it.Images, p.Text)
				case client.PartBinary:
					if strings.HasPrefix(p.MIMEType, "image/") {
						it.Text = strings.TrimSpace(it.Text + "\n[" + path.Base(p.Path) + "]")
					}
				}
			}
			if strings.TrimSpace(it.Text) != "" || len(it.Images) > 0 {
				out = append(out, it)
			}
		case client.RoleAssistant:
			var parts []*Part
			var errNotice string
			for _, p := range m.Parts {
				switch p.Kind {
				case client.PartText:
					if p.Text != "" {
						pp := mkPart(PText)
						pp.Text = p.Text
						parts = append(parts, pp)
					}
				case client.PartThinking:
					if p.Text != "" {
						pp := mkPart(PThinking)
						pp.Text = p.Text
						parts = append(parts, pp)
					}
				case client.PartToolCall:
					t := &Tool{ID: p.ToolCall.ID, Name: p.ToolCall.Name, Input: p.ToolCall.Input, Status: "completed"}
					if r, ok := results[t.ID]; ok {
						t.Output, t.IsError = r.Content, r.IsError
						if r.IsError {
							t.Status = "failed"
						}
					} else if !p.ToolCall.Finished {
						t.Status = "pending"
					}
					pp := mkPart(PTool)
					pp.Tool = t
					parts = append(parts, pp)
				case client.PartFinish:
					if p.FinishReason == "error" || p.FinishReason == "canceled" {
						errNotice = "Run " + p.FinishReason
					}
				}
			}
			if len(parts) == 0 {
				if errNotice != "" {
					out = append(out, &Item{Kind: ItemNotice, Text: errNotice, Done: true, Time: ts})
				}
				continue
			}
			if n := len(out); n > 0 && out[n-1].Kind == ItemAssistant {
				out[n-1].Parts = append(out[n-1].Parts, parts...)
				out[n-1].Time = ts
				continue
			}
			out = append(out, &Item{Kind: ItemAssistant, Done: true, Model: m.Model, Parts: parts, Time: ts})
		case client.RoleSystem:
			if txt := m.Text(); strings.TrimSpace(txt) != "" {
				out = append(out, &Item{Kind: ItemPersona, Text: txt, Done: true, Time: ts})
			}
		}
	}
	return out
}
