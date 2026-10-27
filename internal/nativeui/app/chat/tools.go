package chat

import (
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/kit"
)

// Category is the coarse class of a tool call, used to build the "ran 3
// commands · read 2 files" summary of a folded run of calls
// (web MessageBubble ActivityCategory).
type Category int

// Activity categories, in the order the summary lists them.
const (
	CatCommands Category = iota
	CatRead
	CatEdited
	CatSearched
	CatFetched
	CatTools
	CatThought
)

var categoryKeys = map[Category]string{
	CatCommands: "commands", CatRead: "read", CatEdited: "edited", CatSearched: "searched",
	CatFetched: "fetched", CatTools: "tools", CatThought: "thought",
}

// Class is the display class of a tool call: it picks the detail layout.
type Class int

// Tool classes.
const (
	ClassJSON Class = iota // generic: input as JSON, output
	ClassExec
	ClassRead
	ClassSearch
	ClassFetch
	ClassEdit  // old/new replacement
	ClassWrite // whole file content
)

// ToolMeta describes how a call is shown.
type ToolMeta struct {
	Icon     string
	Label    string
	Summary  string
	Category Category
	Class    Class
}

func shortPath(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	return strings.Join(parts, "/")
}

func str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func cut(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// MetaOf classifies a call by its name and input, then lets the server's
// kind and title win (they mirror ACP), as the Web UI does.
func MetaOf(t *Tool) ToolMeta {
	m := t.InputMap()
	meta := localMeta(strings.ToLower(t.Name), t.Name, m)
	switch t.Kind {
	case "execute":
		meta.Label, meta.Category, meta.Icon = "bash", CatCommands, "square-terminal"
		if meta.Class != ClassExec {
			meta.Class = ClassExec
		}
	case "edit":
		meta.Label, meta.Category = "edit", CatEdited
	case "read":
		meta.Label, meta.Category = "read", CatRead
		if meta.Class == ClassJSON {
			meta.Class = ClassRead
		}
	case "search":
		meta.Label, meta.Category = "search", CatSearched
		if meta.Class == ClassJSON {
			meta.Class = ClassSearch
		}
	case "fetch":
		meta.Label, meta.Category = "fetch", CatFetched
		if meta.Class == ClassJSON {
			meta.Class = ClassFetch
		}
	case "think":
		meta.Label = "agent"
	case "switch_mode":
		meta.Label = "mode"
	}
	if t.Title != "" {
		meta.Summary = t.Title
	}
	return meta
}

func localMeta(n, name string, in map[string]any) ToolMeta {
	switch n {
	case "bash", "execute_bash", "run_command":
		return ToolMeta{"square-terminal", "bash", firstLine(str(in, "command", "cmd"), 80), CatCommands, ClassExec}
	case "str_replace_editor":
		cmd := str(in, "command")
		if cmd == "" {
			cmd = "view"
		}
		p := shortPath(str(in, "path"))
		switch cmd {
		case "view":
			return ToolMeta{"eye", "view", p, CatRead, ClassRead}
		case "create":
			return ToolMeta{"file-plus", "create", p, CatEdited, ClassWrite}
		case "str_replace":
			return ToolMeta{"pencil", "edit", p, CatEdited, ClassEdit}
		case "insert":
			return ToolMeta{"pencil", "insert", p, CatEdited, ClassWrite}
		case "delete_file":
			return ToolMeta{"trash", "delete", p, CatEdited, ClassJSON}
		}
		return ToolMeta{"pencil", cmd, p, CatEdited, ClassJSON}
	case "edit":
		return ToolMeta{"pencil", "edit", shortPath(str(in, "file_path", "path")), CatEdited, ClassEdit}
	case "write":
		return ToolMeta{"file-pen", "write", shortPath(str(in, "file_path", "path")), CatEdited, ClassWrite}
	case "read":
		return ToolMeta{"file-text", "read", shortPath(str(in, "file_path", "path")), CatRead, ClassRead}
	case "grep":
		return ToolMeta{"search", "grep", cut(str(in, "pattern"), 50), CatSearched, ClassSearch}
	case "glob":
		return ToolMeta{"folder", "glob", str(in, "pattern"), CatSearched, ClassSearch}
	case "web_fetch", "fetch", "http_request":
		return ToolMeta{"globe", "fetch", cut(str(in, "url"), 60), CatFetched, ClassFetch}
	}
	if strings.Contains(n, "search") {
		return ToolMeta{"search", "search", cut(str(in, "query", "q"), 50), CatSearched, ClassSearch}
	}
	return ToolMeta{"wrench", name, "", CatTools, ClassJSON}
}

func firstLine(s string, n int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return cut(strings.TrimSpace(s), n)
}

// Live reports whether the call is still running.
func (t *Tool) Live() bool { return t.Status == "pending" || t.Status == "in_progress" }

// Failed reports whether the call failed.
func (t *Tool) Failed() bool { return t.IsError || t.Status == "failed" }

// Block is a run of consecutive tool calls and thinking parts folded into
// one activity row, or a text part.
type Block struct {
	// Text is set for a text block.
	Text *Part
	// Events are the thinking/tool parts of an activity group.
	Events []*Part
}

// Blocks groups the parts of an assistant turn: consecutive tool calls and
// thinking parts become one activity group, text stands alone.
func Blocks(parts []*Part) []Block {
	var out []Block
	for _, p := range parts {
		if p.Kind == PText {
			if strings.TrimSpace(p.Text) != "" {
				out = append(out, Block{Text: p})
			}
			continue
		}
		if n := len(out); n > 0 && out[n-1].Text == nil {
			out[n-1].Events = append(out[n-1].Events, p)
		} else {
			out = append(out, Block{Events: []*Part{p}})
		}
	}
	return out
}

// GroupSummary builds "ran 3 commands · read 2 files" for a group, in the
// Web UI's category order, with the failure count.
func GroupSummary(events []*Part, T func(string, ...any) string) (summary string, failures int, live *Tool) {
	counts := map[Category]int{}
	for _, e := range events {
		if e.Kind == PThinking {
			counts[CatThought]++
			continue
		}
		counts[MetaOf(e.Tool).Category]++
		if e.Tool.Failed() {
			failures++
		}
		if e.Tool.Live() {
			live = e.Tool
		}
	}
	var parts []string
	for c := CatCommands; c <= CatThought; c++ {
		if n := counts[c]; n > 0 {
			parts = append(parts, T("chat.activity."+categoryKeys[c], "count", n))
		}
	}
	return strings.Join(parts, " · "), failures, live
}

// ToolIcon is the icon of a call's status mark.
func statusIcon(t *Tool) string {
	switch {
	case t.Failed():
		return kit.IconError
	case t.Status == "completed":
		return kit.IconCheck
	}
	return ""
}

// ProgressLabel is "bash $ command" style text for the live call of a group.
func (m ToolMeta) ProgressLabel() string {
	return strings.TrimSpace(fmt.Sprintf("%s %s", m.Label, m.Summary))
}
