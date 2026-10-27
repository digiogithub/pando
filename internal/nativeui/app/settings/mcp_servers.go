package settings

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

var (
	mcpTypes     = [][2]string{{"stdio", "stdio"}, {"sse", "SSE"}, {"streamable-http", "Streamable HTTP"}}
	mcpAuthTypes = [][2]string{{"none", "None"}, {"bearer", "Bearer token"}, {"basic", "Basic (username/password)"},
		{"header", "Custom header"}, {"oauth", "OAuth 2.1 (authorization code)"}}
)

// mcpParseArgs splits a command line into arguments (quotes and backslash
// escapes), the same grammar as the Web UI's parseCommandLineArgs.
func mcpParseArgs(in string) []string {
	var args []string
	var cur strings.Builder
	has := false
	var quote rune
	rs := []rune(in)
	for i := 0; i < len(rs); i++ {
		ch := rs[i]
		switch {
		case quote == '\'':
			if ch == '\'' {
				quote = 0
			} else {
				cur.WriteRune(ch)
			}
		case quote == '"':
			if ch == '"' {
				quote = 0
			} else if ch == '\\' && i+1 < len(rs) && (rs[i+1] == '"' || rs[i+1] == '\\') {
				cur.WriteRune(rs[i+1])
				i++
			} else {
				cur.WriteRune(ch)
			}
		case unicode.IsSpace(ch):
			if has {
				args = append(args, cur.String())
				cur.Reset()
				has = false
			}
		case ch == '\'' || ch == '"':
			quote, has = ch, true
		case ch == '\\' && i+1 < len(rs):
			cur.WriteRune(rs[i+1])
			i++
			has = true
		default:
			cur.WriteRune(ch)
			has = true
		}
	}
	if has {
		args = append(args, cur.String())
	}
	return args
}

// mcpFormatArgs is the inverse of mcpParseArgs.
func mcpFormatArgs(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		switch {
		case a == "":
			out[i] = `""`
		case !strings.ContainsAny(a, " \t\n\"'\\"):
			out[i] = a
		default:
			out[i] = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(a) + `"`
		}
	}
	return strings.Join(out, " ")
}

type mcpForm struct {
	name, typ, command, args, url              string
	env, headers                               []crKV
	authType, token, username, password        string
	headerName, clientID, clientSecret, scopes string
	port                                       string
	hasToken, hasPassword, hasSecret           bool
}

type mcpSection struct {
	status
	s       *Settings
	servers []client.MCPServer
	busy    string // server with a request in flight

	editing  bool // dialog open
	editName string
	form     mcpForm

	confirm    bool
	delName    string
	toolsOpen  bool
	toolsOf    string
	openURL    string
	toolScroll ui.ScrollState
	formScroll ui.ScrollState
}

func newMCPServers(s *Settings) section { return &mcpSection{s: s} }

func (m *mcpSection) SelfManaged()         {}
func (m *mcpSection) Dirty() bool          { return false }
func (m *mcpSection) Save(done func(bool)) { done(true) }
func (m *mcpSection) Discard()             {}

func (m *mcpSection) Load() {
	if m.loading {
		return
	}
	m.loading = true
	a := m.s.app
	a.Go(func(ctx context.Context) {
		list, err := a.Client.MCPServers(ctx)
		a.Update(func() {
			m.loading, m.loaded = false, true
			if err != nil {
				m.err = err.Error()
				return
			}
			m.err, m.servers = "", list
		})
	})
}

func (m *mcpSection) toast(err error, ok string) {
	if err != nil {
		m.s.app.Toasts.Error(err.Error(), "")
		return
	}
	m.s.app.Toasts.Success(ok, "")
}

func (m *mcpSection) openForm(sv *client.MCPServer) {
	f := mcpForm{typ: "stdio", authType: "none"}
	m.editName = ""
	if sv != nil {
		m.editName = sv.Name
		f.name, f.command, f.url, f.args = sv.Name, sv.Command, sv.URL, mcpFormatArgs(sv.Args)
		if sv.Type != "" {
			f.typ = sv.Type
		}
		for _, e := range sv.Env {
			k, v, _ := strings.Cut(e, "=")
			f.env = append(f.env, crKV{k, v})
		}
		for k, v := range sv.Headers {
			f.headers = append(f.headers, crKV{k, v})
		}
		if a := sv.Auth; a != nil {
			f.authType, f.username, f.headerName = orStr(a.Type, "none"), a.Username, a.HeaderName
			f.hasToken, f.hasPassword = a.HasToken, a.HasPassword
			if o := a.OAuth; o != nil {
				f.clientID, f.hasSecret, f.scopes = o.ClientID, o.HasClientSecret, crJoinSpace(o.Scopes)
				if o.CallbackPort > 0 {
					f.port = strconv.Itoa(o.CallbackPort)
				}
			}
		}
	}
	m.form, m.editing = f, true
}

// server converts the form to the PUT body.
func (f *mcpForm) server() client.MCPServer {
	sv := client.MCPServer{Name: strings.TrimSpace(f.name), Command: f.command, Args: mcpParseArgs(f.args),
		Type: f.typ, URL: f.url, Env: []string{}, Headers: map[string]string{}}
	if sv.Args == nil {
		sv.Args = []string{}
	}
	for _, p := range f.env {
		if strings.TrimSpace(p.k) != "" {
			sv.Env = append(sv.Env, p.k+"="+p.v)
		}
	}
	for _, p := range f.headers {
		if k := strings.TrimSpace(p.k); k != "" {
			sv.Headers[k] = p.v
		}
	}
	a := &client.MCPAuth{Type: f.authType, HasToken: f.hasToken, HasPassword: f.hasPassword}
	switch f.authType {
	case "bearer":
		a.Token = f.token
	case "header":
		a.Token, a.HeaderName = f.token, f.headerName
	case "basic":
		a.Username, a.Password = f.username, f.password
	case "oauth":
		o := &client.MCPOAuth{ClientID: f.clientID, ClientSecret: f.clientSecret, HasClientSecret: f.hasSecret, Scopes: strings.Fields(f.scopes)}
		o.CallbackPort, _ = strconv.Atoi(strings.TrimSpace(f.port))
		a.OAuth = o
	}
	sv.Auth = a
	return sv
}

func (m *mcpSection) save() {
	if strings.TrimSpace(m.form.name) == "" {
		m.s.app.Toasts.Error("Name is required", "")
		return
	}
	sv := m.form.server()
	m.saving = true
	a := m.s.app
	a.Go(func(ctx context.Context) {
		err := a.Client.SaveMCPServer(ctx, sv)
		var list []client.MCPServer
		if err == nil {
			list, _ = a.Client.MCPServers(ctx)
		}
		a.Update(func() {
			m.saving = false
			m.toast(err, fmt.Sprintf("MCP server %q saved", sv.Name))
			if err == nil {
				m.editing = false
				if list != nil {
					m.servers = list
				}
			}
		})
	})
}

func (m *mcpSection) refresh() {
	a := m.s.app
	a.Go(func(ctx context.Context) {
		list, err := a.Client.MCPServers(ctx)
		a.Update(func() {
			if err == nil {
				m.servers = list
			}
		})
	})
}

func (m *mcpSection) act(name string, fn func(ctx context.Context) (string, error)) {
	m.busy = name
	a := m.s.app
	a.Go(func(ctx context.Context) {
		msg, err := fn(ctx)
		a.Update(func() {
			m.busy = ""
			m.toast(err, msg)
			m.refresh()
		})
	})
}

func (m *mcpSection) Build(c *ui.Context) {
	s, t := m.s, kit.Current()
	if m.openURL != "" {
		c.OpenURL(m.openURL)
		m.openURL = ""
	}
	s.crHeader(c, s.app.T("settings.categories.mcpServers"), "Connect external tool servers over stdio, SSE or streamable HTTP.", "Add Server", func() { m.openForm(nil) })
	switch {
	case !m.loaded:
		s.loadingView(c, &m.status)
	case len(m.servers) == 0:
		kit.EmptyState(c, kit.IconTool, "No MCP servers configured", "Add one to get started.")
	default:
		crListCard(c, func() {
			for i := range m.servers {
				m.row(c.Key("mcp:"+m.servers[i].Name), &m.servers[i], t)
			}
		})
	}
	m.dialogs(c)
}

func (m *mcpSection) row(c *ui.Context, sv *client.MCPServer, t *kit.Tokens) {
	typ := orStr(sv.Type, "stdio")
	target := sv.URL
	if typ == "stdio" {
		target = sv.Command
	}
	ui.Row(c).Padding(10, 14).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(4).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, sv.Name).FontWeight(600).FontSize(t.TextSM)
				kit.Badge(c, typ, kit.ToneNeutral)
				if sv.Running {
					kit.Badge(c, "Running", kit.ToneSuccess)
				} else {
					kit.Badge(c, "Stopped", kit.ToneNeutral)
				}
				if sv.Auth != nil && sv.Auth.Type == "oauth" {
					switch st := sv.AuthStatus; {
					case st != nil && st.HasTokens && st.Expired:
						kit.Badge(c, "OAuth expired", kit.ToneWarning)
					case st != nil && st.HasTokens:
						kit.Badge(c, "OAuth ok", kit.ToneSuccess)
					default:
						kit.Badge(c, "needs login", kit.ToneNeutral)
					}
				}
			})
			ui.Text(c, target).FontSize(t.TextXS).TextColor(t.FgMuted).SingleLine().Ellipsis("…")
		})
		if kit.Button(c.Key("tools"), kit.ButtonOpts{Label: fmt.Sprintf("%d tools", len(sv.Tools)), Variant: kit.Ghost, Size: kit.SizeSM, Tooltip: "Click to view tools"}).Clicked() {
			m.toolsOpen, m.toolsOf = true, sv.Name
		}
		dis := m.busy == sv.Name
		if kit.Button(c.Key("edit"), kit.ButtonOpts{Icon: kit.IconEdit, Tooltip: "Edit", Variant: kit.Ghost, Size: kit.SizeSM}).Clicked() {
			m.openForm(sv)
		}
		if kit.Button(c.Key("reload"), kit.ButtonOpts{Icon: kit.IconRefresh, Tooltip: "Reload", Variant: kit.Ghost, Size: kit.SizeSM, Disabled: dis}).Clicked() {
			name := sv.Name
			m.act(name, func(ctx context.Context) (string, error) {
				n, err := m.s.app.Client.ReloadMCPServer(ctx, name)
				return fmt.Sprintf("MCP server %q reloaded — %d tool(s) discovered", name, n), err
			})
		}
		if sv.Auth != nil && sv.Auth.Type == "oauth" {
			if kit.Button(c.Key("login"), kit.ButtonOpts{Icon: kit.IconExternal, Tooltip: "Opens the authorization URL in the browser", Variant: kit.Ghost, Size: kit.SizeSM, Disabled: dis}).Clicked() {
				name := sv.Name
				m.busy = name
				a := m.s.app
				a.Go(func(ctx context.Context) {
					u, msg, err := a.Client.MCPLogin(ctx, name)
					a.Update(func() {
						m.busy = ""
						if err != nil {
							m.toast(err, "")
							return
						}
						m.openURL = u
						a.Toasts.Info(orStr(msg, fmt.Sprintf("Authorization started for %q — complete it in the browser.", name)), "")
					})
					// The flow completes in the background; refresh after a pause.
					time.Sleep(5 * time.Second)
					a.Update(m.refresh)
				})
			}
			if sv.AuthStatus != nil && sv.AuthStatus.HasTokens {
				if kit.Button(c.Key("logout"), kit.ButtonOpts{Icon: kit.IconClose, Tooltip: "Logout", Variant: kit.Ghost, Size: kit.SizeSM, Disabled: dis}).Clicked() {
					name := sv.Name
					m.act(name, func(ctx context.Context) (string, error) {
						return fmt.Sprintf("Logged out of MCP server %q", name), m.s.app.Client.MCPLogout(ctx, name)
					})
				}
			}
		}
		if kit.Button(c.Key("del"), kit.ButtonOpts{Icon: kit.IconTrash, Tooltip: "Delete", Variant: kit.Ghost, Size: kit.SizeSM}).Clicked() {
			m.confirm, m.delName = true, sv.Name
		}
	})
}

func (m *mcpSection) dialogs(c *ui.Context) {
	s := m.s
	f := &m.form
	title := "Add MCP Server"
	if m.editName != "" {
		title = "Edit: " + m.editName
	}
	kit.Dialog(c, &m.editing, kit.DialogOpts{Title: title, Width: 620,
		Body: func() {
			ui.Scroll(c).TrackScroll(&m.formScroll).Height(440).Children(func() {
				ui.Column(c).Gap(12).Padding(0, 8, 0, 0).Children(func() { m.formBody(c, f) })
			})
		},
		Footer: func() {
			if kit.SecondaryButton(c.Key("mcp-cancel"), "Cancel").Clicked() {
				m.editing = false
			}
			label := s.app.T("common.save")
			if m.saving {
				label = s.app.T("common.saving")
			}
			if kit.Button(c.Key("mcp-save"), kit.ButtonOpts{Label: label, Variant: kit.Primary, Disabled: m.saving}).Clicked() {
				m.save()
			}
		}})
	crConfirm(c, &m.confirm, "Delete MCP Server", fmt.Sprintf("Are you sure you want to delete %q? This action cannot be undone.", m.delName), "Delete", func() {
		name := m.delName
		m.act(name, func(ctx context.Context) (string, error) {
			return fmt.Sprintf("MCP server %q deleted", name), s.app.Client.DeleteMCPServer(ctx, name)
		})
	})
	var tools []client.MCPTool
	for _, sv := range m.servers {
		if sv.Name == m.toolsOf {
			tools = sv.Tools
		}
	}
	kit.Dialog(c, &m.toolsOpen, kit.DialogOpts{Title: fmt.Sprintf("Tools — %s (%d)", m.toolsOf, len(tools)), Width: 560,
		Body: func() {
			t := kit.Current()
			if len(tools) == 0 {
				ui.Text(c, "No tools discovered yet. Try reloading the server.").TextColor(t.FgMuted).FontSize(t.TextSM)
				return
			}
			ui.Scroll(c).TrackScroll(&m.toolScroll).Height(360).Children(func() {
				ui.Column(c).Gap(8).Children(func() {
					for _, tl := range tools {
						ui.Column(c.Key("tool:"+tl.Name)).Gap(2).Padding(8).Radius(t.RadiusSM).Background(t.BgCard).Border(1, t.Border).Children(func() {
							ui.Text(c, tl.Name).FontWeight(600).FontSize(t.TextSM)
							if tl.Description != "" {
								ui.Text(c, tl.Description).FontSize(t.TextXS).TextColor(t.FgMuted)
							}
						})
					}
				})
			})
		},
		Footer: func() {
			if kit.SecondaryButton(c.Key("tools-close"), "Close").Clicked() {
				m.toolsOpen = false
			}
		}})
}

func (m *mcpSection) formBody(c *ui.Context, f *mcpForm) {
	t := kit.Current()
	crField(c, "Name", func() { kit.Input(c, &f.name, "my-server").Disabled(m.editName != "") })
	crField(c, "Type", func() { crSelect(c.Key("type"), &f.typ, mcpTypes) })
	remote := f.typ == "sse" || f.typ == "streamable-http"
	if !remote {
		crField(c, "Command", func() { kit.Input(c, &f.command, "npx @modelcontextprotocol/server-filesystem") })
		crField(c, "Arguments", func() {
			ui.TextArea(c, &f.args).Lines(2, 4).Placeholder(`--port 3000 --workspace "/path with spaces"`)
			ui.Text(c, "Enter arguments as you would in a command line. They will be saved as an array of strings.").FontSize(t.TextXS).TextColor(t.FgMuted)
		})
	} else {
		crField(c, "URL", func() { kit.Input(c, &f.url, "http://localhost:3000/mcp") })
	}
	crKVEditor(c, "Environment Variables", &f.env, "ENV_VAR", "value")
	if !remote {
		return
	}
	crKVEditor(c, "Headers", &f.headers, "Header-Name", "Header value")
	ui.Text(c, "Authentication").FontWeight(600).FontSize(t.TextSM)
	crField(c, "Auth Type", func() { crSelect(c.Key("auth"), &f.authType, mcpAuthTypes) })
	secret := func(label, ph string, v *string, has bool, keep string) {
		crField(c, label, func() {
			if has {
				ph = keep
			}
			kit.Input(c, v, ph).Password()
		})
	}
	switch f.authType {
	case "bearer", "header":
		l := "Bearer Token"
		if f.authType == "header" {
			l = "Header Value"
		}
		secret(l, "Enter a token", &f.token, f.hasToken, "Leave blank to keep the stored token")
		if f.authType == "header" {
			crField(c, "Header Name", func() { kit.Input(c, &f.headerName, "Authorization") })
		}
	case "basic":
		crField(c, "Username", func() { kit.Input(c, &f.username, "") })
		secret("Password", "Enter a password", &f.password, f.hasPassword, "Leave blank to keep the stored password")
	case "oauth":
		ui.Text(c, "OAuth 2.1 authorization-code flow with PKCE. Leave Client ID empty to rely on dynamic client registration (RFC 7591) if the server supports it. Save this server first, then use the Login button in the list to authorize it.").
			FontSize(t.TextXS).TextColor(t.FgMuted)
		crField(c, "Client ID", func() { kit.Input(c, &f.clientID, "(optional — dynamic registration if empty)") })
		secret("Client Secret", "Only for confidential clients", &f.clientSecret, f.hasSecret, "Leave blank to keep the stored secret")
		crField(c, "Scopes", func() { kit.Input(c, &f.scopes, "scope1 scope2") })
		crField(c, "Callback Port", func() { kit.Input(c, &f.port, "19876").Width(140) })
	}
}
