package settings

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

var providerTypes = []Opt{
	{"anthropic", "Anthropic"}, {"openai", "OpenAI"}, {"openai-compatible", "OpenAI Compatible (custom)"},
	{"ollama", "Ollama"}, {"gemini", "Google Gemini"}, {"groq", "Groq"}, {"openrouter", "OpenRouter"},
	{"xai", "xAI (Grok)"}, {"azure", "Azure OpenAI"}, {"bedrock", "AWS Bedrock"}, {"vertexai", "Google Vertex AI"},
	{"copilot", "GitHub Copilot"},
}

func inStrs(l []string, s string) bool { return slices.Contains(l, s) }

var (
	typesWithBaseURL = []string{"openai-compatible", "azure", "ollama", "openai"}
	typesWithHeaders = []string{"openai-compatible", "azure", "openai", "anthropic", "openrouter"}
	typesNoAPIKey    = []string{"copilot", "vertexai"}
	accountIDRe      = regexp.MustCompile(`^[a-z0-9-]+$`)
)

func typeLabel(v string) string {
	for _, o := range providerTypes {
		if o.V == v {
			return o.L
		}
	}
	return v
}

func ostr(o client.Obj, k string) string { s, _ := o[k].(string); return s }
func obool(o client.Obj, k string) bool  { b, _ := o[k].(bool); return b }

type accountForm struct {
	editID, id, name, typeLabel, apiKey, baseURL, headers string
	disabled                                              bool
}

// providersSection lists the provider accounts (ProviderAccountsSettings.tsx).
type providersSection struct {
	status
	s        *Settings
	accounts []client.Obj
	dialog   bool
	form     accountForm
	saving   bool
	del      string
	delOpen  bool
	tests    map[string]string // id -> testing|ok|fail
}

func newProviders(s *Settings) section {
	return &providersSection{s: s, tests: map[string]string{}}
}

func (p *providersSection) SelfManaged()         {}
func (p *providersSection) Dirty() bool          { return false }
func (p *providersSection) Save(done func(bool)) { done(true) }
func (p *providersSection) Discard()             {}

func (p *providersSection) Load() {
	if p.loading {
		return
	}
	p.loading = true
	a := p.s.app
	a.Go(func(ctx context.Context) {
		list, err := a.Client.ProviderAccounts(ctx)
		a.Update(func() {
			p.loading, p.loaded = false, true
			if err != nil {
				p.err = err.Error()
				a.Toasts.Error("Failed to load provider accounts", err.Error())
				return
			}
			p.err = ""
			p.accounts = list
		})
	})
}

func (p *providersSection) openForm(f accountForm) { p.form, p.dialog = f, true }

func (p *providersSection) selectedType() string {
	for _, o := range providerTypes {
		if o.L == p.form.typeLabel {
			return o.V
		}
	}
	return p.form.typeLabel
}

func slugify(s string) string {
	s = strings.ToLower(s)
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func (p *providersSection) payload() client.Obj {
	f, typ := p.form, p.selectedType()
	hdr := client.Obj{}
	for _, ln := range strings.Split(f.headers, "\n") {
		if k, v, ok := strings.Cut(ln, ":"); ok && strings.TrimSpace(k) != "" {
			hdr[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	out := client.Obj{"displayName": f.name, "type": typ, "baseUrl": f.baseURL, "disabled": f.disabled, "extraHeaders": hdr}
	if f.editID == "" {
		out["id"] = f.id
	}
	if typ != "copilot" && f.apiKey != "" {
		out["apiKey"] = f.apiKey
	}
	return out
}

func (p *providersSection) saveForm() {
	f, a := p.form, p.s.app
	switch {
	case strings.TrimSpace(f.name) == "":
		a.Toasts.Error("Display Name is required", "")
		return
	case f.editID == "" && f.id == "":
		a.Toasts.Error("Account ID is required", "")
		return
	case f.editID == "" && !accountIDRe.MatchString(f.id):
		a.Toasts.Error("Account ID must match /^[a-z0-9-]+$/", "")
		return
	}
	body, typ := p.payload(), p.selectedType()
	p.saving = true
	a.Go(func(ctx context.Context) {
		err := a.Client.SaveProviderAccount(ctx, f.editID, body)
		a.Update(func() {
			p.saving = false
			if err != nil {
				a.Toasts.Error("Save failed", err.Error())
				return
			}
			p.dialog = false
			if f.editID == "" {
				a.Toasts.Success("Account created", "")
				if typ == "copilot" {
					p.copilotLogin()
				}
			} else {
				a.Toasts.Success("Account updated", "")
			}
			p.loading = false
			p.Load()
		})
	})
}

func (p *providersSection) copilotLogin() {
	a := p.s.app
	a.Go(func(ctx context.Context) {
		r, err := a.Client.CopilotLogin(ctx)
		a.Update(func() {
			if err != nil {
				a.Toasts.Error("Failed to start Copilot login", err.Error())
				return
			}
			a.Toasts.Info("GitHub Copilot", fmt.Sprintf("Enter code: %s at %s", r.UserCode, r.VerificationURI))
		})
	})
}

func (p *providersSection) test(id string) {
	a := p.s.app
	p.tests[id] = "testing"
	a.Go(func(ctx context.Context) {
		r, err := a.Client.TestProviderAccount(ctx, id)
		a.Update(func() {
			switch {
			case err != nil:
				p.tests[id] = "fail"
				a.Toasts.Error("Test failed", err.Error())
			case r.OK:
				p.tests[id] = "ok"
				msg := "Connection OK"
				if r.ModelCount > 0 {
					msg += fmt.Sprintf(" (%d models)", r.ModelCount)
				}
				a.Toasts.Success(msg, "")
			default:
				p.tests[id] = "fail"
				a.Toasts.Error(orStr(r.Error, "Test failed"), "")
			}
		})
	})
}

func (p *providersSection) remove(id string) {
	a := p.s.app
	a.Go(func(ctx context.Context) {
		err := a.Client.DeleteProviderAccount(ctx, id)
		a.Update(func() {
			if err != nil {
				a.Toasts.Error("Delete failed", err.Error())
				return
			}
			a.Toasts.Success("Account deleted", "")
			p.loading = false
			p.Load()
		})
	})
}

func (p *providersSection) Build(c *ui.Context) {
	s, t := p.s, kit.Current()
	s.pageHeader(c, s.app.T("settings.categories.providers"), "Manage AI provider accounts. Each account has its own credentials and can be assigned to agents independently.")
	if !p.loaded {
		s.loadingView(c, &p.status)
		return
	}
	ui.Row(c).Gap(8).Children(func() {
		labels := make([]string, len(providerTypes))
		for i, o := range providerTypes {
			labels[i] = o.L
		}
		add := p.form.typeLabel
		if add == "" {
			add = providerTypes[0].L
		}
		ui.Text(c, "Add provider:").FontSize(t.TextSM).TextColor(t.FgMuted)
		kit.Select(c, &p.form.typeLabel, labels).Width(240)
		if kit.Button(c.Key("add"), kit.ButtonOpts{Label: "Add provider", Variant: kit.Primary, Icon: kit.IconPlus}).Clicked() {
			lbl := p.form.typeLabel
			if lbl == "" {
				lbl = providerTypes[0].L
			}
			p.openForm(accountForm{typeLabel: lbl})
		}
	})
	if len(p.accounts) == 0 {
		kit.EmptyState(c, kit.IconBot, "No providers configured", "Use the \"Add provider\" button above to configure your first provider.")
	}
	for _, acc := range p.accounts {
		p.card(c, acc)
	}
	p.formDialog(c)
	kit.Dialog(c, &p.delOpen, kit.DialogOpts{
		Title:       "Delete Provider Account",
		Description: fmt.Sprintf("Are you sure you want to delete account %q? This action cannot be undone.", p.del),
		Footer: func() {
			if kit.SecondaryButton(c.Key("del-no"), "Cancel").Clicked() {
				p.delOpen = false
			}
			if kit.Button(c.Key("del-yes"), kit.ButtonOpts{Label: "Delete", Variant: kit.Danger}).Clicked() {
				p.delOpen = false
				p.remove(p.del)
			}
		},
	})
}

func (p *providersSection) card(c *ui.Context, acc client.Obj) {
	t := kit.Current()
	id := ostr(acc, "id")
	cc := c.Key("acc:" + id)
	ui.Row(cc).Gap(12).Padding(12, 16).AlignItems(ui.Center).Radius(t.RadiusMD).Background(t.BgCard).Border(1, t.Border).Children(func() {
		ui.Column(cc).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Row(cc).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(cc, ostr(acc, "displayName")).FontWeight(600).FontSize(t.TextMD)
				ui.Text(cc, id).FontSize(t.TextXS).TextColor(t.FgMuted)
				if obool(acc, "disabled") {
					kit.Badge(cc, "Disabled", kit.ToneNeutral)
				} else {
					kit.Badge(cc, "Enabled", kit.ToneSuccess)
				}
			})
			sub := typeLabel(ostr(acc, "type"))
			if k := ostr(acc, "apiKey"); k != "" {
				sub += " · " + k
			}
			ui.Text(cc, sub).FontSize(t.TextXS).TextColor(t.FgMuted)
		})
		if ostr(acc, "type") == "copilot" && kit.SecondaryButton(cc.Key("login"), "Login with GitHub").Clicked() {
			p.copilotLogin()
		}
		if kit.SecondaryButton(cc.Key("edit"), "Edit").Clicked() {
			hdr := []string{}
			m, _ := acc["extraHeaders"].(map[string]any)
			keys := make([]string, 0, len(m))
			for k := range m {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				hdr = append(hdr, k+": "+fmt.Sprint(m[k]))
			}
			p.openForm(accountForm{editID: id, id: id, name: ostr(acc, "displayName"), typeLabel: typeLabel(ostr(acc, "type")),
				baseURL: ostr(acc, "baseUrl"), headers: strings.Join(hdr, "\n"), disabled: obool(acc, "disabled")})
		}
		label := "Test"
		if p.tests[id] == "testing" {
			label = "Testing…"
		}
		if kit.Button(cc.Key("test"), kit.ButtonOpts{Label: label, Variant: kit.Secondary, Disabled: p.tests[id] == "testing"}).Clicked() {
			p.test(id)
		}
		switch p.tests[id] {
		case "ok":
			kit.Icon(cc, kit.IconSuccess).TextColor(t.Success)
		case "fail":
			kit.Icon(cc, kit.IconError).TextColor(t.Danger)
		}
		if kit.Button(cc.Key("del"), kit.ButtonOpts{Label: "Delete", Variant: kit.Danger}).Clicked() {
			p.del, p.delOpen = id, true
		}
	})
}

func (p *providersSection) formDialog(c *ui.Context) {
	t := kit.Current()
	f := &p.form
	typ := p.selectedType()
	title := "Add provider account"
	if f.editID != "" {
		title = "Edit provider account"
	}
	labels := make([]string, len(providerTypes))
	for i, o := range providerTypes {
		labels[i] = o.L
	}
	kit.Dialog(c, &p.dialog, kit.DialogOpts{
		Title: title, Width: 520,
		Body: func() {
			field := func(label string, ctl func()) {
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, label).FontSize(t.TextXS).TextColor(t.FgMuted)
					ctl()
				})
			}
			field("Display name", func() {
				if kit.Input(c, &f.name, "My account").Changed() && f.editID == "" && f.id == slugBefore(f) {
					f.id = slugify(f.name)
				}
			})
			if f.editID == "" {
				field("Account ID", func() {
					if kit.Input(c, &f.id, "my-account").Changed() {
						f.id = strings.ToLower(regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(f.id, "-"))
					}
				})
				field("Type", func() { kit.Select(c, &f.typeLabel, labels) })
			}
			if !inStrs(typesNoAPIKey, typ) {
				field("API key", func() {
					ph := "Enter API key…"
					if f.editID != "" {
						ph = "Leave empty to keep the current key"
					}
					kit.Input(c, &f.apiKey, ph).Password()
				})
			}
			if inStrs(typesWithBaseURL, typ) {
				field("Base URL", func() { kit.Input(c, &f.baseURL, "https://…") })
			}
			if inStrs(typesWithHeaders, typ) {
				field("Extra headers (one \"Name: value\" per line)", func() { ui.TextArea(c, &f.headers).Lines(2, 5) })
			}
			kit.Switch(c, &f.disabled, "Disabled")
		},
		Footer: func() {
			if kit.SecondaryButton(c.Key("f-cancel"), "Cancel").Clicked() {
				p.dialog = false
			}
			if kit.Button(c.Key("f-save"), kit.ButtonOpts{Label: "Save", Variant: kit.Primary, Disabled: p.saving}).Clicked() {
				p.saveForm()
			}
		},
	})
}

// slugBefore is the slug the ID would have had before the last keystroke:
// the ID follows the name until the user edits it by hand.
func slugBefore(f *accountForm) string {
	n := f.name
	if n == "" {
		return f.id
	}
	prev := slugify(n[:len(n)-1])
	return prev
}
