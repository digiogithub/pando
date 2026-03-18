package terminal

import (
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/taigrr/bubbleterm/emulator"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/tui/layout"
)

// terminalTickMsg is sent periodically to poll the terminal for new output.
// GetScreen() is called inside the tick goroutine (not in Update) to avoid blocking the event loop.
type terminalTickMsg struct {
	id   string
	rows []string
}

// TerminalComponent wraps bubbleterm/emulator as a Bubble Tea v1 model.
type TerminalComponent interface {
	tea.Model
	layout.Sizeable
	layout.Bindings
	IsRunning() bool
	Close() error
}

type terminalKeyMap struct {
	FocusToggle key.Binding
}

// terminalModel is the concrete implementation.
type terminalModel struct {
	id      string
	emu     *emulator.Emulator
	rows    []string
	width   int
	height  int
	focused bool
	keyMap  terminalKeyMap
}

// tickInterval controls the refresh rate of the embedded terminal.
const tickInterval = 33 * time.Millisecond // ~30 fps

func shellCommand(shellPath string, shellArgs []string) *exec.Cmd {
	shell := shellPath
	if shell == "" {
		shell = os.Getenv("SHELL")
	}
	if shell == "" {
		shell = "/bin/bash"
	}
	if len(shellArgs) > 0 {
		return exec.Command(shell, shellArgs...)
	}
	return exec.Command(shell)
}

// New creates a new TerminalComponent, launches the configured shell inside a PTY.
// shellPath and shellArgs come from config.Shell; empty values fall back to $SHELL / /bin/bash.
func New(width, height int, shellPath string, shellArgs []string) (TerminalComponent, error) {
	if width < 2 {
		width = 80
	}
	if height < 2 {
		height = 24
	}

	logging.Info("terminal.New: creating emulator", "width", width, "height", height)

	emu, err := emulator.New(width, height)
	if err != nil {
		logging.Error("terminal.New: emulator.New failed", "error", err)
		return nil, err
	}
	logging.Info("terminal.New: emulator created", "id", emu.ID())

	cmd := shellCommand(shellPath, shellArgs)
	logging.Info("terminal.New: starting shell command", "shell", cmd.Path, "args", shellArgs)
	if err := emu.StartCommand(cmd); err != nil {
		logging.Error("terminal.New: StartCommand failed", "error", err)
		_ = emu.Close()
		return nil, err
	}
	logging.Info("terminal.New: shell started successfully", "id", emu.ID())

	m := &terminalModel{
		id:      emu.ID(),
		emu:     emu,
		rows:    make([]string, height),
		width:   width,
		height:  height,
		focused: true,
		keyMap:  defaultTerminalKeyMap(),
	}
	return m, nil
}

func defaultTerminalKeyMap() terminalKeyMap {
	return terminalKeyMap{
		FocusToggle: key.NewBinding(
			key.WithKeys("ctrl+`"),
			key.WithHelp("ctrl+`", "toggle terminal focus"),
		),
	}
}

func (m *terminalModel) Init() tea.Cmd {
	logging.Debug("terminal.Init: scheduling first tick", "id", m.id, "width", m.width, "height", m.height)
	return m.tick()
}

func (m *terminalModel) tick() tea.Cmd {
	id := m.id
	emu := m.emu
	// GetScreen() is called inside the goroutine to avoid blocking the Bubble Tea event loop.
	return tea.Tick(tickInterval, func(_ time.Time) tea.Msg {
		logging.Debug("terminal.tick: calling GetScreen", "id", id)
		frame := emu.GetScreen()
		firstRow := ""
		if len(frame.Rows) > 0 {
			firstRow = frame.Rows[0]
		}
		logging.Debug("terminal.tick: GetScreen returned", "id", id, "rows", len(frame.Rows), "first_row_len", len(firstRow), "first_row", firstRow)
		return terminalTickMsg{id: id, rows: frame.Rows}
	})
}

func (m *terminalModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case terminalTickMsg:
		if msg.id != m.id {
			logging.Debug("terminal.Update: tick ignored (id mismatch)", "my_id", m.id, "msg_id", msg.id)
			return m, nil
		}
		logging.Debug("terminal.Update: tick received", "id", m.id, "rows", len(msg.rows), "process_exited", m.emu.IsProcessExited())
		if len(msg.rows) > 0 {
			m.rows = msg.rows
		}
		return m, m.tick()

	case tea.KeyMsg:
		if !m.focused {
			logging.Debug("terminal.Update: key ignored (not focused)", "id", m.id, "key", msg.String())
			return m, nil
		}
		input := keyMsgToInput(msg)
		logging.Debug("terminal.Update: key input", "id", m.id, "key", msg.String(), "input_bytes", len(input))
		if input != "" {
			_, _ = m.emu.Write([]byte(input))
		}
		return m, nil
	}

	return m, nil
}

func (m *terminalModel) View() string {
	if len(m.rows) == 0 {
		return strings.Repeat("\n", m.height-1)
	}
	return strings.Join(m.rows, "\n")
}

// SetSize implements layout.Sizeable.
func (m *terminalModel) SetSize(width, height int) tea.Cmd {
	if width < 2 {
		width = 2
	}
	if height < 2 {
		height = 2
	}
	logging.Debug("terminal.SetSize", "id", m.id, "width", width, "height", height)
	m.width = width
	m.height = height
	if err := m.emu.Resize(width, height); err != nil {
		logging.Error("terminal.SetSize: Resize failed", "id", m.id, "error", err)
	}
	return nil
}

// GetSize implements layout.Sizeable.
func (m *terminalModel) GetSize() (int, int) {
	return m.width, m.height
}

// BindingKeys implements layout.Bindings.
func (m *terminalModel) BindingKeys() []key.Binding {
	return []key.Binding{m.keyMap.FocusToggle}
}

// IsRunning returns true if the shell process is still alive.
func (m *terminalModel) IsRunning() bool {
	return !m.emu.IsProcessExited()
}

// Close shuts down the terminal emulator.
func (m *terminalModel) Close() error {
	return m.emu.Close()
}

// keyMsgToInput converts a Bubble Tea v1 key message to a byte sequence
// suitable for sending to the PTY.
func keyMsgToInput(msg tea.KeyMsg) string {
	switch msg.Type {
	case tea.KeyEnter:
		return "\r"
	case tea.KeyBackspace:
		return "\x7f"
	case tea.KeyDelete:
		return "\x1b[3~"
	case tea.KeyTab:
		return "\t"
	case tea.KeySpace:
		return " "
	case tea.KeyEscape:
		return "\x1b"
	case tea.KeyUp:
		return "\x1b[A"
	case tea.KeyDown:
		return "\x1b[B"
	case tea.KeyRight:
		return "\x1b[C"
	case tea.KeyLeft:
		return "\x1b[D"
	case tea.KeyHome:
		return "\x1b[H"
	case tea.KeyEnd:
		return "\x1b[F"
	case tea.KeyPgUp:
		return "\x1b[5~"
	case tea.KeyPgDown:
		return "\x1b[6~"
	case tea.KeyCtrlA:
		return "\x01"
	case tea.KeyCtrlB:
		return "\x02"
	case tea.KeyCtrlC:
		return "\x03"
	case tea.KeyCtrlD:
		return "\x04"
	case tea.KeyCtrlE:
		return "\x05"
	case tea.KeyCtrlF:
		return "\x06"
	case tea.KeyCtrlG:
		return "\x07"
	case tea.KeyCtrlH:
		return "\x08"
	case tea.KeyCtrlJ:
		return "\x0a"
	case tea.KeyCtrlK:
		return "\x0b"
	case tea.KeyCtrlL:
		return "\x0c"
	case tea.KeyCtrlN:
		return "\x0e"
	case tea.KeyCtrlO:
		return "\x0f"
	case tea.KeyCtrlP:
		return "\x10"
	case tea.KeyCtrlQ:
		return "\x11"
	case tea.KeyCtrlR:
		return "\x12"
	case tea.KeyCtrlS:
		return "\x13"
	case tea.KeyCtrlT:
		return "\x14"
	case tea.KeyCtrlU:
		return "\x15"
	case tea.KeyCtrlV:
		return "\x16"
	case tea.KeyCtrlW:
		return "\x17"
	case tea.KeyCtrlX:
		return "\x18"
	case tea.KeyCtrlY:
		return "\x19"
	case tea.KeyCtrlZ:
		return "\x1a"
	case tea.KeyRunes:
		return string(msg.Runes)
	}
	return ""
}
