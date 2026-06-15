package page

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/tui/components/chat"
	"github.com/digiogithub/pando/internal/tui/layout"
)

// stubModel is an inert tea.Model used to stand in for the chat content when
// exercising layout sizing in isolation.
type stubModel struct{}

func (stubModel) Init() tea.Cmd                       { return nil }
func (stubModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return stubModel{}, nil }
func (stubModel) View() string                        { return "" }

// newTestChatPage builds a minimal ChatPageModel sufficient for exercising the
// info-sidebar visibility/ratio/layout helpers without standing up a full
// app.App.
func newTestChatPage() *ChatPageModel {
	sb := chat.NewChatInfoSidebar(session.Session{}, nil)
	return &ChatPageModel{
		layoutMode:           ChatOnly,
		infoSidebar:          sb,
		infoSidebarContainer: layout.NewContainer(sb),
		chatContainer:        layout.NewContainer(stubModel{}),
	}
}

func TestChatSidebarVisible(t *testing.T) {
	tests := []struct {
		name  string
		mode  ChatLayoutMode
		width int
		nilSB bool
		want  bool
	}{
		{"chat-wide", ChatOnly, config.ChatSidebarMinWidth(), false, true},
		{"chat-wider", ChatOnly, config.ChatSidebarMinWidth() + 50, false, true},
		{"chat-narrow", ChatOnly, config.ChatSidebarMinWidth() - 1, false, false},
		{"editor-wide", SidebarEditor, config.ChatSidebarMinWidth() + 50, false, false},
		{"split-wide", EditorChatSplit, config.ChatSidebarMinWidth() + 50, false, false},
		{"chat-wide-nil-sidebar", ChatOnly, config.ChatSidebarMinWidth() + 50, true, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestChatPage()
			p.layoutMode = tc.mode
			p.width = tc.width
			if tc.nilSB {
				p.infoSidebar = nil
			}
			if got := p.chatSidebarVisible(); got != tc.want {
				t.Fatalf("chatSidebarVisible() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestChatSidebarRatioReservesWidth checks the left/chat panel ratio reserves
// roughly chatInfoSidebarWidth columns for the sidebar on wide terminals.
func TestChatSidebarRatioReservesWidth(t *testing.T) {
	p := newTestChatPage()

	for _, width := range []int{121, 160, 200, 400} {
		p.width = width
		ratio := p.chatSidebarRatio()
		leftWidth := int(float64(width) * ratio)
		rightWidth := width - leftWidth
		// Allow 1 column of rounding slack.
		if diff := rightWidth - chatInfoSidebarWidth; diff < -1 || diff > 1 {
			t.Fatalf("width=%d: sidebar width = %d, want ~%d (ratio=%.4f)",
				width, rightWidth, chatInfoSidebarWidth, ratio)
		}
	}

	// Degenerate small width must not panic or over-reserve.
	p.width = chatInfoSidebarWidth - 5
	if r := p.chatSidebarRatio(); r <= 0 || r > 1 {
		t.Fatalf("small width ratio out of range: %.4f", r)
	}
}

// TestChatSidebarRebuildTogglesRightPanel verifies rebuildLayout adds the info
// column only when visible (ChatOnly + wide) and drops it otherwise. It infers
// the presence of the right panel from the chat (left) panel width: with the
// sidebar, the chat panel is narrower than the full width.
func TestChatSidebarRebuildTogglesRightPanel(t *testing.T) {
	p := newTestChatPage()
	p.height = 40

	// Wide + ChatOnly: sidebar present -> chat panel narrower than full width.
	p.width = 200
	p.rebuildLayout()
	p.layout.SetSize(p.width, p.height)
	chatW, _ := p.chatContainer.GetSize()
	if chatW >= p.width {
		t.Fatalf("expected chat panel narrower than full width when sidebar visible, got %d (full %d)", chatW, p.width)
	}

	// Narrow: sidebar hidden -> chat panel takes the full width.
	p.width = 100
	p.rebuildLayout()
	p.layout.SetSize(p.width, p.height)
	chatW, _ = p.chatContainer.GetSize()
	if chatW != p.width {
		t.Fatalf("expected chat panel to take full width when sidebar hidden, got %d (full %d)", chatW, p.width)
	}
}

func TestChatSidebarValue(t *testing.T) {
	cases := map[string]string{
		"":      "auto",
		"auto":  "auto",
		"off":   "off",
		"OFF":   "off",
		"Off":   "off",
		" off ": "off",
		"weird": "auto",
	}
	for in, want := range cases {
		if got := chatSidebarValue(in); got != want {
			t.Fatalf("chatSidebarValue(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestChatSidebarConfigChangedRebuilds verifies the chat page rebuilds its
// layout when a ChatSidebarConfigChangedMsg arrives, showing the sidebar while
// wide and in the Chat tab (config defaults: enabled, min width 120).
func TestChatSidebarConfigChangedRebuilds(t *testing.T) {
	p := newTestChatPage()
	p.width = 200
	p.height = 40
	p.rebuildLayout()
	p.layout.SetSize(p.width, p.layoutHeight())

	model, _ := p.Update(chat.ChatSidebarConfigChangedMsg{})
	pp := model.(*ChatPageModel)

	chatW, _ := pp.chatContainer.GetSize()
	if chatW >= pp.width {
		t.Fatalf("expected sidebar present after config change, chat width %d (full %d)", chatW, pp.width)
	}
}
