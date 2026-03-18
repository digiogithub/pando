package terminal

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"github.com/google/uuid"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/tui/layout"
)

// terminalTickMsg is sent periodically to poll the terminal for new output.
type terminalTickMsg struct {
	id   string
	rows []string
}

// TerminalComponent wraps a PTY + vt emulator as a Bubble Tea v1 model.
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
	id            string
	master        *os.File        // PTY master fd
	vtemu         *vt.SafeEmulator // thread-safe vt emulator
	rows          []string
	width         int
	height        int
	focused       bool
	keyMap        terminalKeyMap
	writeCh       chan []byte   // serialized input queue
	processExited atomic.Bool  // cached exit state
	stopCh        chan struct{} // signals goroutines to stop
}

// tickInterval controls the refresh rate of the embedded terminal.
const tickInterval = 33 * time.Millisecond // ~30 fps

func shellCommand(shellPath string, shellArgs []string) (string, []string) {
	shell := shellPath
	if shell == "" {
		shell = os.Getenv("SHELL")
	}
	if shell == "" {
		shell = "/bin/bash"
	}
	args := shellArgs
	// Force interactive mode when no custom args are provided.
	if len(args) == 0 {
		args = []string{"-i"}
	}
	return shell, args
}

// New creates a new TerminalComponent, launches the configured shell inside a PTY.
func New(width, height int, shellPath string, shellArgs []string) (TerminalComponent, error) {
	if width < 2 {
		width = 80
	}
	if height < 2 {
		height = 24
	}

	id := uuid.New().String()
	logging.Info("terminal.New: creating", "id", id, "width", width, "height", height)

	vtemu := vt.NewSafeEmulator(width, height)

	shell, args := shellCommand(shellPath, shellArgs)
	logging.Info("terminal.New: starting shell", "id", id, "shell", shell, "args", args)

	cmd := exec.Command(shell, args...)

	// Set up environment: inherit parent env and force TERM.
	cmd.Env = os.Environ()
	termSet := false
	for i, env := range cmd.Env {
		if len(env) >= 5 && env[:5] == "TERM=" {
			cmd.Env[i] = "TERM=xterm-256color"
			termSet = true
			break
		}
	}
	if !termSet {
		cmd.Env = append(cmd.Env, "TERM=xterm-256color")
	}

	sz := &pty.Winsize{
		Rows: uint16(height),
		Cols: uint16(width),
		X:    uint16(width * 8),
		Y:    uint16(height * 16),
	}
	master, err := pty.StartWithSize(cmd, sz)
	if err != nil {
		logging.Error("terminal.New: pty.StartWithSize failed", "id", id, "error", err)
		return nil, err
	}
	logging.Info("terminal.New: shell started", "id", id, "pid", cmd.Process.Pid)

	writeCh := make(chan []byte, 256)
	stopCh := make(chan struct{})

	m := &terminalModel{
		id:      id,
		master:  master,
		vtemu:   vtemu,
		rows:    make([]string, height),
		width:   width,
		height:  height,
		focused: true,
		keyMap:  defaultTerminalKeyMap(),
		writeCh: writeCh,
		stopCh:  stopCh,
	}

	// Goroutine: read PTY master output → write to vt emulator.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				dump := hex.EncodeToString(buf[:n])
				if len(dump) > 120 {
					dump = dump[:120] + "..."
				}
				logging.Debug("terminal: PTY data received", "id", id, "bytes", n, "hex", dump)
				if _, werr := vtemu.Write(buf[:n]); werr != nil {
					logging.Warn("terminal: vt write error", "id", id, "error", werr)
				}
			}
			if err != nil {
				logging.Info("terminal: PTY read ended", "id", id, "error", err)
				return
			}
		}
	}()

	// Goroutine: write keyboard input to PTY master.
	go func() {
		for data := range writeCh {
			if _, err := master.Write(data); err != nil {
				logging.Warn("terminal: PTY keyboard write error", "id", id, "error", err)
			}
		}
	}()

	// Goroutine: forward emulator responses (e.g. CPR \x1b[6n replies) back to PTY stdin.
	// Without this, programs like bash hang waiting for cursor-position responses.
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := vtemu.Read(buf)
			if n > 0 {
				logging.Debug("terminal: emulator response to PTY", "id", id, "bytes", n)
				if _, werr := master.Write(buf[:n]); werr != nil {
					logging.Warn("terminal: PTY response write error", "id", id, "error", werr)
				}
			}
			if err != nil {
				logging.Info("terminal: emulator read ended", "id", id, "error", err)
				return
			}
		}
	}()

	// Goroutine: monitor process exit.
	go func() {
		err := cmd.Wait()
		logging.Info("terminal: shell exited", "id", id, "error", err)
		m.processExited.Store(true)
	}()

	return m, nil
}

func defaultTerminalKeyMap() terminalKeyMap {
	return terminalKeyMap{
		FocusToggle: key.NewBinding(
			key.WithKeys("ctrl+alt+t"),
			key.WithHelp("ctrl+alt+t", "toggle terminal focus"),
		),
	}
}

func (m *terminalModel) Init() tea.Cmd {
	logging.Debug("terminal.Init: scheduling first tick", "id", m.id)
	return m.tick()
}

func (m *terminalModel) tick() tea.Cmd {
	id := m.id
	vtemu := m.vtemu
	width := m.width
	height := m.height
	return tea.Tick(tickInterval, func(_ time.Time) tea.Msg {
		rendered := vtemu.Render()
		rows := splitIntoRows(rendered, height, width)
		// Find non-empty rows to diagnose rendering issues.
		nonEmpty := ""
		for i, r := range rows {
			if trimmed := strings.TrimSpace(r); trimmed != "" {
				nonEmpty += fmt.Sprintf("row%d:%q ", i, trimmed)
				if len(nonEmpty) > 200 {
					break
				}
			}
		}
		logging.Debug("terminal.tick: rendered", "id", id, "rendered_len", len(rendered), "rows", len(rows), "non_empty_rows", nonEmpty)
		return terminalTickMsg{id: id, rows: rows}
	})
}

func (m *terminalModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case terminalTickMsg:
		if msg.id != m.id {
			return m, nil
		}
		logging.Debug("terminal.Update: tick received", "id", m.id, "rows", len(msg.rows), "process_exited", m.processExited.Load())
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
			select {
			case m.writeCh <- []byte(input):
			default:
				logging.Warn("terminal.Update: write buffer full, dropping input", "id", m.id)
			}
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

	// Resize PTY and vt emulator.
	if err := pty.Setsize(m.master, &pty.Winsize{
		Rows: uint16(height),
		Cols: uint16(width),
		X:    uint16(width * 8),
		Y:    uint16(height * 16),
	}); err != nil {
		logging.Error("terminal.SetSize: pty.Setsize failed", "id", m.id, "error", err)
	}
	m.vtemu.Resize(width, height)
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
	return !m.processExited.Load()
}

// Close shuts down the terminal emulator.
func (m *terminalModel) Close() error {
	close(m.stopCh)
	close(m.writeCh)
	return m.master.Close()
}

// splitIntoRows splits the rendered output into individual rows and pads to width.
func splitIntoRows(rendered string, height, width int) []string {
	rows := make([]string, height)
	lines := strings.Split(rendered, "\n")
	for i := 0; i < height; i++ {
		if i < len(lines) {
			rows[i] = padRow(lines[i], width)
		} else {
			rows[i] = strings.Repeat(" ", width)
		}
	}
	return rows
}

// padRow pads a row to the specified width, accounting for ANSI escape codes.
func padRow(row string, width int) string {
	visibleLen := 0
	inEscape := false
	for _, r := range row {
		if r == '\033' {
			inEscape = true
		} else if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '~' {
				inEscape = false
			}
		} else {
			visibleLen++
		}
	}
	if visibleLen < width {
		return row + strings.Repeat(" ", width-visibleLen)
	}
	return row
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
