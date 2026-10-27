package chat

import (
	"strings"

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
	ItemPermission
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
}

// Label is the text shown for the call.
func (t *Tool) Label() string {
	if t.Title != "" && t.Title != t.Name {
		return t.Title
	}
	return t.Name
}

// Item is a row of the transcript.
type Item struct {
	// Key is stable for the life of the row (list identity).
	Key  int
	Kind ItemKind
	// Text is the markdown of user/assistant rows, the text of notices and
	// errors.
	Text     string
	Thinking string
	Tools    []*Tool
	// Done is false while an assistant row is still streaming.
	Done  bool
	Model string
	// Permission is set for ItemPermission; Answer once answered.
	Permission *client.PermissionRequest
	Answer     string

	ThinkingOpen bool
}

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
}

func newConversation(id string) *Conversation {
	cv := &Conversation{ID: id, tools: map[string]*Tool{}}
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
	cv.Items = append(cv.Items, it)
	return it
}

// SetHistory replaces the transcript with folded history.
func (cv *Conversation) SetHistory(msgs []client.Message) {
	cv.Items = nil
	cv.tools = map[string]*Tool{}
	for _, it := range FoldHistory(msgs) {
		cv.add(it)
		for _, t := range it.Tools {
			cv.tools[t.ID] = t
		}
	}
	cv.runStart = len(cv.Items)
	cv.Loaded = true
}

// TrimToLastPrompt drops the rows after the last user prompt: a run being
// attached replays them.
func (cv *Conversation) TrimToLastPrompt() {
	for i := len(cv.Items) - 1; i >= 0; i-- {
		if cv.Items[i].Kind == ItemUser {
			cv.Items = cv.Items[:i+1]
			cv.runStart = i + 1
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

func (cv *Conversation) tool(id string) *Tool {
	if t, ok := cv.tools[id]; ok {
		return t
	}
	t := &Tool{ID: id}
	cv.tools[id] = t
	a := cv.assistant()
	a.Tools = append(a.Tools, t)
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
		cv.tools = map[string]*Tool{}
		for _, it := range cv.Items {
			for _, t := range it.Tools {
				cv.tools[t.ID] = t
			}
		}
		cv.content = false
	case client.EventContentDelta:
		cv.assistant().Text += ev.Text
	case client.EventThinkingDelta:
		cv.assistant().Thinking += ev.Text
	case client.EventToolCall:
		if tc := ev.ToolCall; tc != nil {
			t := cv.tool(tc.ID)
			t.Name, t.Kind, t.Title, t.Input = tc.Name, tc.Kind, tc.Title, tc.Input
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
			if u.Title != "" {
				t.Title = u.Title
			}
			if u.Input != "" {
				t.Input = u.Input
			}
		}
	case client.EventToolResult:
		if r := ev.ToolResult; r != nil {
			t := cv.tool(r.ToolCallID)
			if t.Name == "" {
				t.Name = r.Name
			}
			if r.Title != "" {
				t.Title = r.Title
			}
			t.Output, t.IsError = r.Content, r.IsError
			switch {
			case r.Status != "":
				t.Status = r.Status
			case r.IsError:
				t.Status = "failed"
			default:
				t.Status = "completed"
			}
		}
	case client.EventSystemMessage:
		if s := strings.TrimSpace(ev.Text); s != "" {
			cv.closeAssistant()
			cv.add(&Item{Kind: ItemNotice, Text: s, Done: true})
		}
	case client.EventSteeringQueued:
		cv.add(&Item{Kind: ItemNotice, Text: i18n.T("chat.queued") + ": " + ev.Message, Done: true})
	case client.EventSteeringInjected:
		cv.closeAssistant()
		cv.add(&Item{Kind: ItemUser, Text: ev.Message, Done: true})
	case client.EventResurrected:
		cv.add(&Item{Kind: ItemNotice, Text: i18n.T("chat.notice.resurrected"), Done: true})
	case client.EventConclusionQueued:
		cv.add(&Item{Kind: ItemNotice, Text: i18n.T("chat.notice.conclusionQueued"), Done: true})
	case client.EventConclusionInjected:
		cv.add(&Item{Kind: ItemNotice, Text: i18n.T("chat.notice.conclusionInjected"), Done: true})
	case client.EventPermissionRequest:
		if p := ev.PermissionRequest; p != nil {
			for _, it := range cv.Items {
				if it.Permission != nil && it.Permission.ID == p.ID {
					return false // replayed
				}
			}
			cv.closeAssistant()
			cv.add(&Item{Kind: ItemPermission, Permission: p, Done: true})
		}
	case client.EventQuestionRequest:
		if q := ev.QuestionRequest; q != nil && len(q.Questions) > 0 {
			cv.add(&Item{Kind: ItemNotice, Text: "Question pending: " + q.Questions[0].Question +
				" (answer it from the browser UI; native question prompts arrive in P1)", Done: true})
		}
	case client.EventTokenUsage:
		cv.Usage = ev.TokenUsage
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
	return !cv.content
}

// FoldHistory turns stored messages into transcript rows: tool messages are
// folded into the call they answer (as the Web UI does), finish and empty
// parts dropped.
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
	for _, m := range msgs {
		switch m.Role {
		case client.RoleUser:
			if txt := m.Text(); strings.TrimSpace(txt) != "" {
				out = append(out, &Item{Kind: ItemUser, Text: txt, Done: true})
			}
		case client.RoleAssistant:
			it := &Item{Kind: ItemAssistant, Done: true, Model: m.Model}
			for _, p := range m.Parts {
				switch p.Kind {
				case client.PartText:
					it.Text += p.Text
				case client.PartThinking:
					it.Thinking += p.Text
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
					it.Tools = append(it.Tools, t)
				case client.PartFinish:
					if p.FinishReason == "error" || p.FinishReason == "canceled" {
						if it.Text == "" && len(it.Tools) == 0 {
							it.Kind, it.Text = ItemNotice, "Run "+p.FinishReason
						}
					}
				}
			}
			if it.Text != "" || it.Thinking != "" || len(it.Tools) > 0 || it.Kind == ItemNotice {
				out = append(out, it)
			}
		case client.RoleSystem:
			if txt := m.Text(); strings.TrimSpace(txt) != "" {
				out = append(out, &Item{Kind: ItemNotice, Text: txt, Done: true})
			}
		}
	}
	return out
}
