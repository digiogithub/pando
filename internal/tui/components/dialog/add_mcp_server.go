package dialog

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
)

const (
	addMCPServerDialogWidth = 68
	// AddMCPServerDialogWidth is the exported width for callers that need to centre the overlay.
	AddMCPServerDialogWidth = addMCPServerDialogWidth
)

// MCPServerCreatedMsg is sent when a new MCP server is configured.
type MCPServerCreatedMsg struct {
	Name   string
	Server config.MCPServer
}

// CloseAddMCPServerDialogMsg is sent when the add-MCP-server dialog is dismissed.
type CloseAddMCPServerDialogMsg struct{}

// AddMCPServerDialog is the interface for the add-MCP-server dialog.
type AddMCPServerDialog interface {
	tea.Model
}

type addMCPServerDialogCmp struct {
	width  int
	height int

	types     []config.MCPType
	typeIdx   int
	activePos int
	inputs    [5]textinput.Model // 0=name 1=command 2=args 3=env-or-headers 4=url
	errMsg    string
}

var addMCPServerSlugRe = regexp.MustCompile(`[^a-z0-9-]`)

func slugifyMCPServerName(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else if b.Len() > 0 {
			b.WriteByte('-')
		}
	}
	return strings.TrimRight(b.String(), "-")
}

func newAddMCPServerInputs() [5]textinput.Model {
	var inputs [5]textinput.Model
	for i := range inputs {
		inputs[i] = textinput.New()
		inputs[i].Prompt = ""
	}
	inputs[0].Placeholder = "my-server"
	inputs[0].Focus()
	inputs[1].Placeholder = "npx"
	inputs[2].Placeholder = "-y @modelcontextprotocol/server-filesystem /path"
	inputs[3].Placeholder = "API_KEY=secret DEBUG=1"
	inputs[4].Placeholder = "https://example.com/mcp OR Authorization:Bearer token"
	return inputs
}

// NewAddMCPServerDialog creates a new add-MCP-server dialog.
func NewAddMCPServerDialog(width, height int) AddMCPServerDialog {
	return &addMCPServerDialogCmp{
		width:  width,
		height: height,
		types:  []config.MCPType{config.MCPStdio, config.MCPSse, config.MCPStreamableHTTP},
		inputs: newAddMCPServerInputs(),
	}
}

func (d *addMCPServerDialogCmp) Init() tea.Cmd { return nil }

func (d *addMCPServerDialogCmp) selectedType() config.MCPType {
	if len(d.types) == 0 {
		return config.MCPStdio
	}
	return d.types[d.typeIdx]
}

func (d *addMCPServerDialogCmp) visibleFields() []int {
	fields := []int{0, -1}
	if d.selectedType() == config.MCPStdio {
		fields = append(fields, 1, 2, 3)
	} else {
		fields = append(fields, 4, 3)
	}
	return fields
}

func (d *addMCPServerDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		fieldIdx := d.visibleFields()[d.activePos]
		if fieldIdx < 0 {
			return d, nil
		}
		var cmd tea.Cmd
		d.inputs[fieldIdx], cmd = d.inputs[fieldIdx].Update(msg)
		return d, cmd
	}

	visible := d.visibleFields()
	currentIdx := visible[d.activePos]

	switch keyMsg.String() {
	case "esc":
		return d, func() tea.Msg { return CloseAddMCPServerDialogMsg{} }
	case "tab", "down":
		if currentIdx >= 0 {
			d.inputs[currentIdx].Blur()
		}
		d.activePos = (d.activePos + 1) % len(visible)
		if nextIdx := visible[d.activePos]; nextIdx >= 0 {
			d.inputs[nextIdx].Focus()
		}
		return d, nil
	case "shift+tab", "up":
		if currentIdx >= 0 {
			d.inputs[currentIdx].Blur()
		}
		d.activePos = (d.activePos - 1 + len(visible)) % len(visible)
		if nextIdx := visible[d.activePos]; nextIdx >= 0 {
			d.inputs[nextIdx].Focus()
		}
		return d, nil
	case "left", "h":
		if currentIdx == -1 && d.typeIdx > 0 {
			d.typeIdx--
		}
		return d, nil
	case "right", "l":
		if currentIdx == -1 && d.typeIdx < len(d.types)-1 {
			d.typeIdx++
		}
		return d, nil
	case "enter":
		if d.activePos < len(visible)-1 {
			if currentIdx >= 0 {
				d.inputs[currentIdx].Blur()
			}
			d.activePos++
			if nextIdx := visible[d.activePos]; nextIdx >= 0 {
				d.inputs[nextIdx].Focus()
			}
			return d, nil
		}
		return d, d.saveServer()
	case "ctrl+s":
		return d, d.saveServer()
	}

	if currentIdx < 0 {
		return d, nil
	}

	d.errMsg = ""
	var cmd tea.Cmd
	d.inputs[currentIdx], cmd = d.inputs[currentIdx].Update(msg)
	return d, cmd
}

func (d *addMCPServerDialogCmp) saveServer() tea.Cmd {
	name := strings.TrimSpace(d.inputs[0].Value())
	if name == "" {
		d.errMsg = "Name is required"
		return nil
	}
	name = addMCPServerSlugRe.ReplaceAllString(strings.ToLower(name), "-")
	if name == "" {
		d.errMsg = "Name must contain letters or numbers"
		return nil
	}

	serverType := d.selectedType()
	server := config.MCPServer{Type: serverType}
	if serverType == config.MCPStdio {
		server.Command = strings.TrimSpace(d.inputs[1].Value())
		if server.Command == "" {
			d.errMsg = "Command is required for stdio servers"
			return nil
		}
		server.Args = strings.Fields(d.inputs[2].Value())
		server.Env = strings.Fields(d.inputs[3].Value())
	} else {
		server.URL = strings.TrimSpace(d.inputs[4].Value())
		if server.URL == "" {
			d.errMsg = "URL is required for SSE/HTTP servers"
			return nil
		}
		if headers := strings.TrimSpace(d.inputs[3].Value()); headers != "" {
			parsed, err := parseHeaderPairs(headers)
			if err != nil {
				d.errMsg = err.Error()
				return nil
			}
			server.Headers = parsed
		}
	}
	if _, exists := config.Get().MCPServers[name]; exists {
		d.errMsg = "An MCP server with that name already exists"
		return nil
	}

	d.errMsg = ""
	return func() tea.Msg {
		return MCPServerCreatedMsg{Name: name, Server: server}
	}
}

func parseHeaderPairs(s string) (map[string]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}

	parsed := make(map[string]string)
	for _, pair := range strings.Fields(s) {
		idx := strings.IndexByte(pair, ':')
		if idx <= 0 {
			return nil, configErr("invalid header pair %q: expected Key:Value format", pair)
		}
		key := strings.TrimSpace(pair[:idx])
		value := strings.TrimSpace(pair[idx+1:])
		if key == "" {
			return nil, configErr("invalid header pair %q: key cannot be empty", pair)
		}
		parsed[key] = value
	}
	return parsed, nil
}

func configErr(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

func (d *addMCPServerDialogCmp) View() string {
	t := theme.CurrentTheme()
	base := styles.BaseStyle()

	var sb strings.Builder
	titleStyle := lipgloss.NewStyle().Foreground(t.Primary()).Bold(true).MarginBottom(1)
	labelStyle := lipgloss.NewStyle().Width(16).Foreground(t.TextMuted())
	inputWidth := addMCPServerDialogWidth - 24

	sb.WriteString(titleStyle.Render("Add MCP Server"))
	sb.WriteByte('\n')
	sb.WriteString(lipgloss.NewStyle().Foreground(t.TextMuted()).Render("Configure a stdio, SSE, or HTTP MCP server."))
	sb.WriteString("\n\n")

	typeStyle := lipgloss.NewStyle().Foreground(t.TextMuted())
	if d.visibleFields()[d.activePos] == -1 {
		typeStyle = typeStyle.Foreground(t.Primary()).Bold(true)
	}
	typeParts := make([]string, 0, len(d.types))
	for i, tp := range d.types {
		partStyle := lipgloss.NewStyle().Foreground(t.TextMuted())
		if i == d.typeIdx {
			partStyle = partStyle.Foreground(t.Primary()).Bold(true)
		}
		typeParts = append(typeParts, partStyle.Render(string(tp)))
	}
	sb.WriteString(typeStyle.Render("Type            : ") + strings.Join(typeParts, "  "))
	sb.WriteString("\n\n")

	type row struct {
		label string
		idx   int
		hint  string
	}
	rows := []row{{"Name", 0, "Unique server key used in config."}}
	if d.selectedType() == config.MCPStdio {
		rows = append(rows,
			row{"Command", 1, "Executable to launch."},
			row{"Arguments", 2, "Space-separated command arguments."},
			row{"Env Vars", 3, "Space-separated KEY=VALUE pairs."},
		)
	} else {
		rows = append(rows,
			row{"URL", 4, "Remote MCP endpoint URL."},
			row{"Headers", 3, "Space-separated Header:Value pairs."},
		)
	}

	visible := d.visibleFields()
	for _, r := range rows {
		input := d.inputs[r.idx]
		input.Width = inputWidth
		selected := visible[d.activePos] == r.idx
		rowStyle := lipgloss.NewStyle().Foreground(t.Text())
		if selected {
			rowStyle = rowStyle.Foreground(t.Primary())
		}
		sb.WriteString(rowStyle.Render(lipgloss.JoinHorizontal(lipgloss.Center, labelStyle.Render(r.label+":"), input.View())))
		sb.WriteByte('\n')
		sb.WriteString(lipgloss.NewStyle().Foreground(t.TextMuted()).PaddingLeft(18).Render(r.hint))
		sb.WriteByte('\n')
	}

	if strings.TrimSpace(d.errMsg) != "" {
		sb.WriteByte('\n')
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true).Render(d.errMsg))
	}

	sb.WriteByte('\n')
	sb.WriteString(lipgloss.NewStyle().Foreground(t.TextMuted()).Render("Tab/↑↓ move  ←→ change type  Enter/Ctrl+S save  Esc cancel"))

	return base.
		Width(addMCPServerDialogWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary()).
		Padding(1, 2).
		Render(sb.String())
}

var _ tea.Model = (*addMCPServerDialogCmp)(nil)

type addMCPServerDialogKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Left   key.Binding
	Right  key.Binding
	Enter  key.Binding
	Escape key.Binding
}

var addMCPServerKeys = addMCPServerDialogKeyMap{
	Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "previous")),
	Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "next")),
	Left:   key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "prev type")),
	Right:  key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "next type")),
	Enter:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
	Escape: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}
