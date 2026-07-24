package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	osc52 "github.com/aymanbagabas/go-osc52/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/tui/components/dialog"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
	tuizone "github.com/digiogithub/pando/internal/tui/zone"
)

type cacheItem struct {
	width      int
	signature  string
	content    []uiMessage
	renderedAt time.Time
}

type copiedMsg struct{}

// copyToClipboard places text on the clipboard. It makes a best-effort call to
// the OS clipboard helper (xclip/xsel/wl-copy/pbcopy) AND always emits an OSC 52
// escape sequence to the terminal. OSC 52 needs no helper binary and works over
// SSH and on Wayland sessions where those helpers are frequently absent
// (WezTerm, COSMIC Terminal, ...), which is why the OS-helper path alone failed
// silently. Returns false only for empty input.
func copyToClipboard(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	// Best-effort; commonly errors on headless/Wayland with no helper installed.
	_ = clipboard.WriteAll(text)
	seq := osc52.New(text)
	if os.Getenv("TMUX") != "" {
		seq = seq.Tmux()
	}
	_, _ = seq.WriteTo(os.Stdout)
	return true
}

// copyAndFeedback copies text and, on success, flips the "copied" indicator and
// returns the tick command that clears it after a short delay.
func (m *messagesCmp) copyAndFeedback(text string) tea.Cmd {
	if !copyToClipboard(text) {
		return nil
	}
	m.copyFeedback = true
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return copiedMsg{}
	})
}

type messagesCmp struct {
	app           *app.App
	width, height int
	viewport      viewport.Model
	session       session.Session
	goal          *GoalState
	messages      []message.Message
	uiMessages    []uiMessage
	currentMsgID  string
	cachedContent map[string]cacheItem
	spinner       spinner.Model
	rendering     bool
	attachments   viewport.Model
	renderSeq     int

	// Mouse selection state
	mouseDown       bool
	selectionActive bool
	selectionStartY int
	selectionEndY   int
	selectionStartX int
	selectionEndX   int
	contentLines    []string // plain-text lines for copy
	copyFeedback    bool     // show "Copied!" notification
}

type renderFinishedMsg struct{}

type markdownDebounceMsg struct {
	sequence int
}

const markdownRenderDebounce = 150 * time.Millisecond

type MessageKeys struct {
	PageDown     key.Binding
	PageUp       key.Binding
	HalfPageUp   key.Binding
	HalfPageDown key.Binding
}

var messageKeys = MessageKeys{
	PageDown: key.NewBinding(
		key.WithKeys("pgdown"),
		key.WithHelp("f/pgdn", "page down"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("pgup"),
		key.WithHelp("b/pgup", "page up"),
	),
	HalfPageUp: key.NewBinding(
		key.WithKeys("ctrl+u"),
		key.WithHelp("ctrl+u", "½ page up"),
	),
	HalfPageDown: key.NewBinding(
		key.WithKeys("ctrl+d", "ctrl+d"),
		key.WithHelp("ctrl+d", "½ page down"),
	),
}

func (m *messagesCmp) Init() tea.Cmd {
	return tea.Batch(m.viewport.Init(), m.spinner.Tick)
}

func (m *messagesCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case dialog.ThemeChangedMsg:
		m.rerender()
		return m, nil
	case SessionSelectedMsg:
		if msg.ID != m.session.ID {
			m.goal = nil
			cmd := m.SetSession(msg)
			return m, cmd
		}
		return m, nil
	case SessionClearedMsg:
		m.session = session.Session{}
		m.goal = nil
		m.messages = make([]message.Message, 0)
		m.currentMsgID = ""
		m.rendering = false
		return m, nil

	case copiedMsg:
		m.copyFeedback = false

	case tea.MouseMsg:
		// The floating "jump to bottom" affordance and the per-message "copy"
		// buttons are UI controls, not selectable text. Resolve them on BOTH press
		// and release (terminals are inconsistent about which event a click
		// delivers) and return immediately so a click on a control never starts or
		// finalizes a text selection. The copy button copies the block's stored
		// response text (message.Content), never rendered/visible characters.
		if msg.Button == tea.MouseButtonLeft &&
			(msg.Action == tea.MouseActionPress || msg.Action == tea.MouseActionRelease) {
			if z := tuizone.Manager.Get(tuizone.ChatScrollBottom); z != nil && z.InBounds(msg) {
				if msg.Action == tea.MouseActionRelease {
					m.viewport.GotoBottom()
				}
				m.mouseDown = false
				return m, nil
			}
			for _, mm := range m.messages {
				z := tuizone.Manager.Get(tuizone.ChatCopyID(mm.ID))
				if z == nil || !z.InBounds(msg) {
					continue
				}
				// Copy only on release, so a single click yields exactly one copy.
				if msg.Action == tea.MouseActionRelease {
					if text := strings.TrimSpace(mm.Content().String()); text != "" {
						if cmd := m.copyAndFeedback(text); cmd != nil {
							cmds = append(cmds, cmd)
						}
					}
				}
				// A click on the button is not the start of a selection.
				m.mouseDown = false
				m.selectionActive = false
				return m, tea.Batch(cmds...)
			}
		}
		// Right-click copies the current selection (explicit, like Ctrl+Shift+C).
		if msg.Button == tea.MouseButtonRight && msg.Action == tea.MouseActionPress && m.selectionActive {
			if text := m.selectedText(); text != "" {
				if cmd := m.copyAndFeedback(text); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			return m, tea.Batch(cmds...)
		}
		if msg.Button == tea.MouseButtonLeft {
			zone := tuizone.Manager.Get(tuizone.ChatViewport)
			if zone != nil && zone.InBounds(msg) {
				relX, relY := zone.Pos(msg)
				contentLine := m.viewport.YOffset + relY
				switch msg.Action {
				case tea.MouseActionPress:
					m.mouseDown = true
					m.selectionActive = false
					m.selectionStartY = contentLine
					m.selectionEndY = contentLine
					m.selectionStartX = relX
					m.selectionEndX = relX
				case tea.MouseActionMotion:
					if m.mouseDown {
						m.selectionEndY = contentLine
						m.selectionEndX = relX
						if m.selectionStartY != m.selectionEndY || m.selectionStartX != m.selectionEndX {
							m.selectionActive = true
						}
					}
				case tea.MouseActionRelease:
					// Finalize the selection on the end of a genuine drag and copy
					// it. Only a real drag (mouseDown, with movement) copies, so a
					// plain click — or a release after a button press — never
					// overwrites the clipboard, and only the column-sliced selection
					// (never the whole rendered chat) is copied. Ctrl+Shift+C is
					// unreliable here (WezTerm/COSMIC reserve it for their own native
					// copy), so copy-on-release is the dependable path.
					if m.mouseDown {
						m.selectionEndY = contentLine
						m.selectionEndX = relX
						if m.selectionStartY != m.selectionEndY || m.selectionStartX != m.selectionEndX {
							m.selectionActive = true
							if text := m.selectedText(); text != "" {
								if cmd := m.copyAndFeedback(text); cmd != nil {
									cmds = append(cmds, cmd)
								}
							}
						} else {
							m.selectionActive = false
						}
					}
					m.mouseDown = false
				}
			} else if msg.Action == tea.MouseActionRelease {
				m.mouseDown = false
			}
		}
		u, cmd := m.viewport.Update(msg)
		m.viewport = u
		cmds = append(cmds, cmd)

	case tea.KeyMsg:
		if msg.String() == "ctrl+end" {
			m.viewport.GotoBottom()
			return m, nil
		}
		// ctrl+c is reserved for the app-level quit dialog. Ctrl+Shift+C copies the
		// selection where the terminal delivers it (WezTerm/COSMIC reserve it for
		// their own native copy, so copy-on-release and right-click are the
		// dependable paths). Use the same column-precise selectedText as those.
		if msg.String() == "ctrl+shift+c" && m.selectionActive {
			if text := m.selectedText(); text != "" {
				if cmd := m.copyAndFeedback(text); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			m.selectionActive = false
			return m, tea.Batch(cmds...)
		}
		if key.Matches(msg, messageKeys.PageUp) || key.Matches(msg, messageKeys.PageDown) ||
			key.Matches(msg, messageKeys.HalfPageUp) || key.Matches(msg, messageKeys.HalfPageDown) {
			u, cmd := m.viewport.Update(msg)
			m.viewport = u
			cmds = append(cmds, cmd)
		}

	case renderFinishedMsg:
		m.rendering = false
		m.viewport.GotoBottom()
	case markdownDebounceMsg:
		if msg.sequence == m.renderSeq {
			m.renderView()
			m.viewport.GotoBottom()
		}
	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent && msg.Payload.ID == m.session.ID {
			m.session = msg.Payload
			if m.session.SummaryMessageID == m.currentMsgID {
				delete(m.cachedContent, m.currentMsgID)
				m.renderView()
			}
		}
	case GoalUpdatedMsg:
		if msg.SessionID == m.session.ID {
			m.goal = msg.Goal
		}
	case pubsub.Event[message.Message]:
		needsRerender := false
		if msg.Type == pubsub.CreatedEvent {
			if msg.Payload.SessionID == m.session.ID {

				messageExists := false
				for _, v := range m.messages {
					if v.ID == msg.Payload.ID {
						messageExists = true
						break
					}
				}

				if !messageExists {
					if len(m.messages) > 0 {
						lastMsgID := m.messages[len(m.messages)-1].ID
						delete(m.cachedContent, lastMsgID)
					}

					m.messages = append(m.messages, msg.Payload)
					delete(m.cachedContent, m.currentMsgID)
					m.currentMsgID = msg.Payload.ID
					needsRerender = true
				}
			}
			// There are tool calls from the child task
			for _, v := range m.messages {
				for _, c := range v.ToolCalls() {
					if c.ID == msg.Payload.SessionID {
						delete(m.cachedContent, v.ID)
						needsRerender = true
					}
				}
			}
		} else if msg.Type == pubsub.UpdatedEvent && msg.Payload.SessionID == m.session.ID {
			for i, v := range m.messages {
				if v.ID == msg.Payload.ID {
					previous := m.messages[i]
					m.messages[i] = msg.Payload
					if m.shouldDebounceRender(previous, msg.Payload) {
						cmds = append(cmds, m.queueMarkdownRerender(msg.Payload.ID))
					} else {
						delete(m.cachedContent, msg.Payload.ID)
						needsRerender = true
					}
					break
				}
			}
		}
		if needsRerender {
			m.renderView()
			if len(m.messages) > 0 {
				if (msg.Type == pubsub.CreatedEvent) ||
					(msg.Type == pubsub.UpdatedEvent && msg.Payload.ID == m.messages[len(m.messages)-1].ID) {
					m.viewport.GotoBottom()
				}
			}
		}
	}

	spinner, cmd := m.spinner.Update(msg)
	m.spinner = spinner
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m *messagesCmp) IsAgentWorking() bool {
	return m.app.CoderAgent.IsSessionBusy(m.session.ID)
}

func formatTimeDifference(unixTime1, unixTime2 int64) string {
	diffSeconds := float64(math.Abs(float64(unixTime2 - unixTime1)))

	if diffSeconds < 60 {
		return fmt.Sprintf("%.1fs", diffSeconds)
	}

	minutes := int(diffSeconds / 60)
	seconds := int(diffSeconds) % 60
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}

func messageCacheSignature(msg message.Message, isSummary bool) string {
	parts, err := json.Marshal(msg.Parts)
	if err != nil {
		return fmt.Sprintf("%s:%t:%d", msg.ID, isSummary, msg.UpdatedAt)
	}

	return fmt.Sprintf("%t:%s", isSummary, parts)
}

func cachedHeight(cache cacheItem) int {
	height := 0
	for _, item := range cache.content {
		height += item.height + 1
	}
	return height
}

func (m *messagesCmp) shouldDebounceRender(previous, updated message.Message) bool {
	if updated.Role != message.Assistant || updated.IsFinished() {
		return false
	}

	if previous.Content().Text == updated.Content().Text &&
		previous.ReasoningContent().Thinking == updated.ReasoningContent().Thinking {
		return false
	}

	cache, ok := m.cachedContent[updated.ID]
	if !ok || cache.renderedAt.IsZero() {
		return false
	}

	return time.Since(cache.renderedAt) < markdownRenderDebounce
}

func (m *messagesCmp) queueMarkdownRerender(messageID string) tea.Cmd {
	cache, ok := m.cachedContent[messageID]
	if !ok {
		return nil
	}

	delay := markdownRenderDebounce - time.Since(cache.renderedAt)
	if delay < 0 {
		delay = 0
	}

	m.renderSeq++
	sequence := m.renderSeq
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return markdownDebounceMsg{sequence: sequence}
	})
}

func (m *messagesCmp) renderView() {
	m.uiMessages = make([]uiMessage, 0)
	pos := 0
	baseStyle := styles.BaseStyle()

	if m.width == 0 {
		return
	}
	for inx, msg := range m.messages {
		switch msg.Role {
		case message.User:
			signature := messageCacheSignature(msg, false)
			if cache, ok := m.cachedContent[msg.ID]; ok && cache.width == m.width && cache.signature == signature {
				m.uiMessages = append(m.uiMessages, cache.content...)
				pos += cachedHeight(cache)
				continue
			}
			userMsg := renderUserMessage(
				msg,
				msg.ID == m.currentMsgID,
				m.width,
				pos,
			)
			m.uiMessages = append(m.uiMessages, userMsg)
			m.cachedContent[msg.ID] = cacheItem{
				width:      m.width,
				signature:  signature,
				content:    []uiMessage{userMsg},
				renderedAt: time.Now(),
			}
			pos += userMsg.height + 1 // + 1 for spacing
		case message.Assistant:
			isSummary := m.session.SummaryMessageID == msg.ID
			signature := messageCacheSignature(msg, isSummary)
			if cache, ok := m.cachedContent[msg.ID]; ok && cache.width == m.width && cache.signature == signature {
				m.uiMessages = append(m.uiMessages, cache.content...)
				pos += cachedHeight(cache)
				continue
			}

			assistantMessages := renderAssistantMessage(
				msg,
				inx,
				m.messages,
				m.app.Messages,
				m.currentMsgID,
				isSummary,
				m.width,
				pos,
			)
			for _, msg := range assistantMessages {
				m.uiMessages = append(m.uiMessages, msg)
				pos += msg.height + 1 // + 1 for spacing
			}
			m.cachedContent[msg.ID] = cacheItem{
				width:      m.width,
				signature:  signature,
				content:    assistantMessages,
				renderedAt: time.Now(),
			}
		}
	}

	messages := make([]string, 0)
	for _, v := range m.uiMessages {
		messages = append(messages, lipgloss.JoinVertical(lipgloss.Left, v.content),
			baseStyle.
				Width(m.width).
				Render(
					"",
				),
		)
	}

	fullContent := baseStyle.
		Width(m.width).
		Render(
			lipgloss.JoinVertical(
				lipgloss.Top,
				messages...,
			),
		)

	// Build plain-text lines for mouse selection/copy
	rawLines := strings.Split(fullContent, "\n")
	m.contentLines = make([]string, len(rawLines))
	for i, line := range rawLines {
		m.contentLines[i] = ansi.Strip(line)
	}

	m.viewport.SetContent(fullContent)
}

func (m *messagesCmp) selectedText() string {
	if len(m.contentLines) == 0 {
		return ""
	}

	startY, endY := m.selectionStartY, m.selectionEndY
	startX, endX := m.selectionStartX, m.selectionEndX
	if startY > endY || (startY == endY && startX > endX) {
		startY, endY = endY, startY
		startX, endX = endX, startX
	}
	if startY < 0 {
		startY = 0
	}
	if endY >= len(m.contentLines) {
		endY = len(m.contentLines) - 1
	}
	if startY > endY {
		return ""
	}

	selected := make([]string, 0, endY-startY+1)
	for lineIdx := startY; lineIdx <= endY; lineIdx++ {
		line := []rune(m.contentLines[lineIdx])
		lineStart := 0
		lineEnd := len(line)

		switch {
		case startY == endY:
			lineStart = clampSelectionColumn(startX, len(line))
			lineEnd = clampSelectionColumn(endX, len(line))
		case lineIdx == startY:
			lineStart = clampSelectionColumn(startX, len(line))
		case lineIdx == endY:
			lineEnd = clampSelectionColumn(endX, len(line))
		}

		if lineStart > lineEnd {
			lineStart, lineEnd = lineEnd, lineStart
		}
		// contentLines are padded to the viewport width, so a selection that
		// reaches a line's end drags in trailing spaces; trim them so the copied
		// text matches the visible characters, not the padding.
		selected = append(selected, strings.TrimRight(string(line[lineStart:lineEnd]), " "))
	}

	return strings.TrimRight(strings.Join(selected, "\n"), "\n")
}

func clampSelectionColumn(col, lineLen int) int {
	if col < 0 {
		return 0
	}
	if col > lineLen {
		return lineLen
	}
	return col
}

func (m *messagesCmp) View() string {
	baseStyle := styles.BaseStyle()

	if m.rendering {
		return baseStyle.
			Width(m.width).
			Render(
				lipgloss.JoinVertical(
					lipgloss.Top,
					"Loading...",
					m.working(),
					m.help(),
				),
			)
	}
	if len(m.messages) == 0 {
		// Reserve 2 lines: 1 for empty separator, 1 for help text
		content := baseStyle.
			Width(m.width).
			Height(m.height - 2).
			MaxHeight(m.height - 2).
			Render(
				m.initialScreen(),
			)

		return baseStyle.
			Width(m.width).
			MaxHeight(m.height).
			Render(
				lipgloss.JoinVertical(
					lipgloss.Top,
					content,
					"",
					m.help(),
				),
			)
	}

	return baseStyle.
		Width(m.width).
		Render(
			lipgloss.JoinVertical(
				lipgloss.Top,
				m.chatViewportView(),
				m.working(),
				m.help(),
			),
		)
}

// chatViewportView renders the messages viewport, marks it as the chat-viewport
// zone (so mouse selection and clicks resolve to content coordinates), and
// overlays a centered "jump to bottom" affordance whenever the view is scrolled
// up. The zone mark is applied last so it always wraps the final composed string.
func (m *messagesCmp) chatViewportView() string {
	composed := m.viewport.View()
	composed = m.applySelectionHighlight(composed)

	if !m.viewport.AtBottom() {
		t := theme.CurrentTheme()
		label := "▼ jump to bottom (ctrl+end)"
		if m.copyFeedback {
			label = "✓ copied"
		}
		btn := tuizone.MarkChatScrollBottom(
			styles.BaseStyle().
				Background(t.Primary()).
				Foreground(t.BadgeText()).
				Padding(0, 1).
				Render(label),
		)
		btnWidth := lipgloss.Width(btn)
		x := (m.width - btnWidth) / 2
		if x < 0 {
			x = 0
		}
		y := m.viewport.Height - 2
		if y < 0 {
			y = 0
		}
		composed = layout.PlaceOverlay(x, y, btn, composed, false)
	} else if m.copyFeedback {
		// At the bottom there is no floating button to reuse, so surface the copy
		// confirmation as a small centered badge on the last row.
		t := theme.CurrentTheme()
		badge := styles.BaseStyle().
			Background(t.Primary()).
			Foreground(t.BadgeText()).
			Padding(0, 1).
			Render("✓ copied")
		x := (m.width - lipgloss.Width(badge)) / 2
		if x < 0 {
			x = 0
		}
		y := m.viewport.Height - 2
		if y < 0 {
			y = 0
		}
		composed = layout.PlaceOverlay(x, y, badge, composed, false)
	}

	return tuizone.MarkChatViewport(composed)
}

// applySelectionHighlight paints the currently selected character range with the
// theme's selection colors by overlaying plain highlighted segments over the
// styled viewport lines. It runs while dragging (mouseDown) and after a
// selection is finalized, giving the user visual feedback of exactly what will
// be copied with Ctrl+Shift+C / right-click.
func (m *messagesCmp) applySelectionHighlight(composed string) string {
	if !(m.selectionActive || m.mouseDown) || len(m.contentLines) == 0 {
		return composed
	}

	startY, endY := m.selectionStartY, m.selectionEndY
	startX, endX := m.selectionStartX, m.selectionEndX
	if startY > endY || (startY == endY && startX > endX) {
		startY, endY = endY, startY
		startX, endX = endX, startX
	}

	t := theme.CurrentTheme()
	hl := lipgloss.NewStyle().
		Background(t.SelectionBackground()).
		Foreground(t.SelectionForeground())

	top := m.viewport.YOffset
	bottom := top + m.viewport.Height
	for idx := startY; idx <= endY; idx++ {
		if idx < 0 || idx >= len(m.contentLines) || idx < top || idx >= bottom {
			continue
		}
		line := []rune(m.contentLines[idx])
		x0, x1 := 0, len(line)
		switch {
		case startY == endY:
			x0 = clampSelectionColumn(startX, len(line))
			x1 = clampSelectionColumn(endX, len(line))
		case idx == startY:
			x0 = clampSelectionColumn(startX, len(line))
		case idx == endY:
			x1 = clampSelectionColumn(endX, len(line))
		}
		if x0 > x1 {
			x0, x1 = x1, x0
		}
		if x0 >= x1 {
			continue
		}
		seg := string(line[x0:x1])
		screenY := idx - top
		composed = layout.PlaceOverlay(x0, screenY, hl.Render(seg), composed, false)
	}
	return composed
}

func hasToolsWithoutResponse(messages []message.Message) bool {
	toolCalls := make([]message.ToolCall, 0)
	toolResults := make([]message.ToolResult, 0)
	for _, m := range messages {
		toolCalls = append(toolCalls, m.ToolCalls()...)
		toolResults = append(toolResults, m.ToolResults()...)
	}

	for _, v := range toolCalls {
		found := false
		for _, r := range toolResults {
			if v.ID == r.ToolCallID {
				found = true
				break
			}
		}
		if !found && v.Finished {
			return true
		}
	}
	return false
}

func hasUnfinishedToolCalls(messages []message.Message) bool {
	toolCalls := make([]message.ToolCall, 0)
	for _, m := range messages {
		toolCalls = append(toolCalls, m.ToolCalls()...)
	}
	for _, v := range toolCalls {
		if !v.Finished {
			return true
		}
	}
	return false
}

func (m *messagesCmp) working() string {
	text := ""
	if m.IsAgentWorking() && len(m.messages) > 0 {
		t := theme.CurrentTheme()
		baseStyle := styles.BaseStyle()

		task := "Thinking..."
		if m.goal != nil && m.goal.IsRunning() {
			task = "Working toward goal..."
		}
		lastMessage := m.messages[len(m.messages)-1]
		if hasToolsWithoutResponse(m.messages) {
			task = "Waiting for tool response..."
		} else if hasUnfinishedToolCalls(m.messages) {
			task = "Building tool call..."
		} else if !lastMessage.IsFinished() {
			if lastMessage.IsThinking() {
				task = "Thinking..."
			} else {
				task = "Generating..."
			}
		}
		// Surface any queued steering feedback so the user knows their mid-run input
		// was accepted and will be injected at the next safe boundary.
		if m.app.CoderAgent != nil && m.session.ID != "" {
			if pending := m.app.CoderAgent.PendingSteering(m.session.ID); pending > 0 {
				task = fmt.Sprintf("%s (%d feedback queued)", task, pending)
			}
		}
		if task != "" {
			text += baseStyle.
				Width(m.width).
				Foreground(t.Primary()).
				Bold(true).
				Render(fmt.Sprintf("%s %s ", m.spinner.View(), task))
		}
	}
	return text
}

func (m *messagesCmp) help() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	text := ""

	if m.goal != nil && m.goal.IsRunning() {
		text += lipgloss.JoinHorizontal(
			lipgloss.Left,
			baseStyle.Foreground(t.TextMuted()).Bold(true).Render("press "),
			baseStyle.Foreground(t.Text()).Bold(true).Render("ctrl+c"),
			baseStyle.Foreground(t.TextMuted()).Bold(true).Render(" to cancel the goal"),
		)
	} else if m.app.CoderAgent.IsBusy() {
		text += lipgloss.JoinHorizontal(
			lipgloss.Left,
			baseStyle.Foreground(t.TextMuted()).Bold(true).Render("press "),
			baseStyle.Foreground(t.Text()).Bold(true).Render("enter"),
			baseStyle.Foreground(t.TextMuted()).Bold(true).Render(" to queue feedback, "),
			baseStyle.Foreground(t.Text()).Bold(true).Render("esc"),
			baseStyle.Foreground(t.TextMuted()).Bold(true).Render(" to cancel"),
		)
	} else {
		text += lipgloss.JoinHorizontal(
			lipgloss.Left,
			baseStyle.Foreground(t.TextMuted()).Bold(true).Render("press "),
			baseStyle.Foreground(t.Text()).Bold(true).Render("enter"),
			baseStyle.Foreground(t.TextMuted()).Bold(true).Render(" to send the message,"),
			baseStyle.Foreground(t.Text()).Bold(true).Render(" ctrl+j"),
			baseStyle.Foreground(t.TextMuted()).Bold(true).Render(" to add a new line"),
		)
	}
	return baseStyle.
		Width(m.width).
		Render(text)
}

func (m *messagesCmp) initialScreen() string {
	baseStyle := styles.BaseStyle()

	return baseStyle.Width(m.width).Render(
		lipgloss.JoinVertical(
			lipgloss.Top,
			cwd(m.width),
			"",
			lspsConfigured(m.width),
		),
	)
}

func (m *messagesCmp) rerender() {
	for _, msg := range m.messages {
		delete(m.cachedContent, msg.ID)
	}
	m.renderView()
}

func (m *messagesCmp) SetSize(width, height int) tea.Cmd {
	if m.width == width && m.height == height {
		return nil
	}
	m.width = width
	m.height = height
	m.viewport.Width = width
	m.viewport.Height = height - 2
	m.attachments.Width = width + 40
	m.attachments.Height = 3
	m.rerender()
	return nil
}

func (m *messagesCmp) GetSize() (int, int) {
	return m.width, m.height
}

func (m *messagesCmp) SetSession(session session.Session) tea.Cmd {
	if m.session.ID == session.ID {
		return nil
	}
	m.session = session
	messages, err := m.app.Messages.List(context.Background(), session.ID)
	if err != nil {
		return util.ReportError(err)
	}
	m.messages = messages
	if len(m.messages) > 0 {
		m.currentMsgID = m.messages[len(m.messages)-1].ID
	}
	delete(m.cachedContent, m.currentMsgID)
	m.rendering = true
	return func() tea.Msg {
		m.renderView()
		return renderFinishedMsg{}
	}
}

func (m *messagesCmp) BindingKeys() []key.Binding {
	return []key.Binding{
		m.viewport.KeyMap.PageDown,
		m.viewport.KeyMap.PageUp,
		m.viewport.KeyMap.HalfPageUp,
		m.viewport.KeyMap.HalfPageDown,
	}
}

func NewMessagesCmp(app *app.App) tea.Model {
	s := spinner.New()
	s.Spinner = spinner.Pulse
	vp := viewport.New(0, 0)
	attachmets := viewport.New(0, 0)
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 2
	vp.KeyMap.PageUp = messageKeys.PageUp
	vp.KeyMap.PageDown = messageKeys.PageDown
	vp.KeyMap.HalfPageUp = messageKeys.HalfPageUp
	vp.KeyMap.HalfPageDown = messageKeys.HalfPageDown
	return &messagesCmp{
		app:           app,
		cachedContent: make(map[string]cacheItem),
		viewport:      vp,
		spinner:       s,
		attachments:   attachmets,
	}
}
