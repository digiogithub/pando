// Package setup is the first-run setup assistant of the native desktop (the
// Web UI's SetupWizard): scope, provider, models, Remembrances and a summary.
// It is a hidden area (/setup) that opens by itself at start-up when
// GET /api/v1/setup/status says the setup is needed.
package setup

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
)

func init() {
	app.Register(app.Area{
		ID: "setup", Path: "/setup", TitleKey: "setup.title", Icon: kit.IconSetup, Section: app.SectionHidden,
		New: func(a *app.Context) app.View { return newWizard(a) },
	})
	app.OnStart(func(a *app.Context) {
		// Child project instances never run the assistant (like the web).
		a.Go(func(ctx context.Context) {
			st, err := fetchStatusRetry(ctx, a.Client)
			if err != nil || !st.Needed {
				return
			}
			a.Update(func() {
				if w := wizardOf(a); w != nil {
					w.setStatus(st)
					if !w.cancelled && a.Router.Path() != "/setup" {
						w.prevPath = a.Router.Path()
						a.Router.Push("/setup")
					}
				}
			})
		})
	})
}

// fetchStatusRetry loads the status, retrying with backoff while the API is
// not ready (about 30 s, as the web does).
func fetchStatusRetry(ctx context.Context, c *client.Client) (client.SetupStatus, error) {
	delays := []time.Duration{0, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second}
	var last error
	for _, d := range delays {
		select {
		case <-ctx.Done():
			return client.SetupStatus{}, ctx.Err()
		case <-time.After(d):
		}
		st, err := c.SetupStatus(ctx)
		if err == nil {
			return st, nil
		}
		last = err
	}
	return client.SetupStatus{}, last
}

var instances = map[*app.Context]*wizard{}

// wizardOf returns the wizard of a context, creating it on first use (UI
// thread only): the start hook may run before the area is ever shown.
func wizardOf(a *app.Context) *wizard {
	if w, ok := instances[a]; ok {
		return w
	}
	w := &wizard{app: a, scope: "global", provType: "copilot", jobs: map[string]*client.SetupJob{}}
	instances[a] = w
	return w
}

type step int

const (
	stepScope step = iota
	stepProvider
	stepModels
	stepRemembrances
	stepDone
)

var stepKeys = []string{"scope", "provider", "models", "remembrances", "done"}

type summary struct {
	scope, configPath, provider, mainModel, fastModel string
	docModel, codeModel                               string
	remembrances                                      bool
}

type wizard struct {
	app       *app.Context
	status    *client.SetupStatus
	cancelled bool
	prevPath  string

	step   step
	inited bool
	sum    summary
	busy   bool // an action is running (disables navigation)
	err    string
	scope  string

	// provider step
	accounts     []client.SetupProviderAccount
	accountsLoad bool
	addingNew    bool
	provType     string
	apiKey       string
	baseURL      string
	copilot      *client.SetupCopilotCode
	copied       bool
	copilotStop  context.CancelFunc

	// models step
	mainModel, fastModel string
	modelOptions         []string
	modelsLoading        bool
	modelsFor            string
	modelsNotice         string

	// remembrances step
	oll         *client.SetupOllamaStatus
	ollChecking bool
	docModel    string
	codeModel   string
	confirmOpt  string
	installLog  []string
	jobs        map[string]*client.SetupJob // by target ("install", model)
	remLoaded   bool
	loading     bool
}

func newWizard(a *app.Context) *wizard {
	return wizardOf(a)
}

// Close stops background polling.
func (w *wizard) Close() {
	if w.copilotStop != nil {
		w.copilotStop()
	}
}

func (w *wizard) setStatus(st client.SetupStatus) {
	w.status = &st
	w.app.Invalidate()
}

func (w *wizard) t(key string, args ...any) string { return w.app.T("setup."+key, args...) }

// reset starts a run from the first step that applies.
func (w *wizard) reset() {
	w.inited = true
	w.busy, w.err = false, ""
	w.sum = summary{}
	if w.status != nil && w.status.HasLocalConfig {
		w.sum.scope, w.sum.configPath = "project", w.status.LocalConfigPath
		w.step = stepProvider
	} else {
		w.scope = "global"
		w.step = stepScope
	}
	w.enterStep()
}

func (w *wizard) enterStep() {
	w.err = ""
	switch w.step {
	case stepProvider:
		if !w.accountsLoad {
			w.loadAccounts()
		}
	case stepModels:
		w.suggest(true)
	case stepRemembrances:
		if !w.remLoaded {
			w.remLoaded = true
			w.refreshOllama()
		}
	}
}

func (w *wizard) leave() {
	if w.copilotStop != nil {
		w.copilotStop()
		w.copilotStop = nil
	}
	w.copilot = nil
	p := w.prevPath
	if p == "" || p == "/setup" {
		p = app.DefaultPath
	}
	w.inited = false
	w.app.Router.Push(p)
}

func (w *wizard) cancel() {
	w.cancelled = true
	w.leave()
}

func (w *wizard) title() string { return w.t("title") }

// Title implements app.Titled.
func (w *wizard) Title() string { return w.title() }

func errText(err error, fallback string) string {
	var ae *client.APIError
	if errors.As(err, &ae) {
		if ae.Code != "" {
			return ae.Code
		}
		if s := strings.TrimSpace(ae.Body); s != "" {
			return s
		}
	}
	if err != nil && err.Error() != "" {
		return err.Error()
	}
	return fallback
}

var homeRe = regexp.MustCompile(`^(/home/[^/]+|/Users/[^/]+)`)

func shorten(p string) string { return homeRe.ReplaceAllString(p, "~") }

// ---- actions -----------------------------------------------------------

func (w *wizard) loadAccounts() {
	w.accountsLoad = true
	w.app.Go(func(ctx context.Context) {
		accs, err := w.app.Client.SetupProviderAccounts(ctx)
		w.app.Update(func() {
			if err != nil {
				accs = nil
			}
			w.accounts = accs
			if w.accounts == nil {
				w.accounts = []client.SetupProviderAccount{}
			}
		})
	})
}

func (w *wizard) applyScope() {
	w.busy, w.err = true, ""
	scope := w.scope
	w.app.Go(func(ctx context.Context) {
		r, err := w.app.Client.SetupScope(ctx, scope)
		w.app.Update(func() {
			w.busy = false
			if err != nil {
				w.err = errText(err, w.t("scope.failed"))
				return
			}
			w.sum.scope, w.sum.configPath = scope, r.ConfigPath
			st := r.Status
			w.status = &st
			w.go_(stepProvider)
		})
	})
}

func (w *wizard) go_(s step) {
	w.step = s
	w.enterStep()
}

func (w *wizard) refreshStatus() {
	w.app.Go(func(ctx context.Context) {
		st, err := w.app.Client.SetupStatus(ctx)
		if err == nil {
			w.app.Update(func() { w.status = &st })
		}
	})
}

type guide struct {
	typ, label, keyURL     string
	needsKey, needsBaseURL bool
	defaultBase            string
	icon                   string
}

var providers = []guide{
	{typ: "copilot", label: "GitHub Copilot", icon: "code"},
	{typ: "anthropic", label: "Anthropic", needsKey: true, keyURL: "https://console.anthropic.com/settings/keys", icon: kit.IconBot},
	{typ: "openai", label: "OpenAI", needsKey: true, keyURL: "https://platform.openai.com/api-keys", icon: kit.IconThinking},
	{typ: "gemini", label: "Google Gemini", needsKey: true, keyURL: "https://aistudio.google.com/app/apikey", icon: kit.IconEvaluator},
	{typ: "openrouter", label: "OpenRouter", needsKey: true, keyURL: "https://openrouter.ai/keys", icon: kit.IconOrchestrate},
	{typ: "groq", label: "Groq", needsKey: true, keyURL: "https://console.groq.com/keys", icon: kit.IconInstances},
	{typ: "xai", label: "xAI (Grok)", needsKey: true, keyURL: "https://console.x.ai", icon: kit.IconSetup},
	{typ: "ollama", label: "Ollama", needsBaseURL: true, defaultBase: "http://localhost:11434", icon: kit.IconFolder},
	{typ: "openai-compatible", label: "OpenAI Compatible", needsKey: true, needsBaseURL: true, icon: kit.IconExtensions},
}

func (w *wizard) guide() guide {
	for _, g := range providers {
		if g.typ == w.provType {
			return g
		}
	}
	return providers[0]
}

func (w *wizard) selectProvider(typ string) {
	if w.provType == typ {
		return
	}
	w.provType = typ
	w.apiKey, w.err = "", ""
	w.baseURL = w.guide().defaultBase
}

func uniqueID(typ string, existing []client.SetupProviderAccount) string {
	ids := map[string]bool{}
	for _, a := range existing {
		ids[a.ID] = true
	}
	if !ids[typ] {
		return typ
	}
	for i := 2; ; i++ {
		id := typ + "-" + itoa(i)
		if !ids[id] {
			return id
		}
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for ; i > 0; i /= 10 {
		s = string(rune('0'+i%10)) + s
	}
	return s
}

func (w *wizard) saveProvider() {
	g := w.guide()
	if g.needsKey && strings.TrimSpace(w.apiKey) == "" {
		w.err = w.t("provider.keyRequired")
		return
	}
	if g.needsBaseURL && strings.TrimSpace(w.baseURL) == "" {
		w.err = w.t("provider.baseUrlRequired")
		return
	}
	w.busy, w.err = true, ""
	existing := append([]client.SetupProviderAccount(nil), w.accounts...)
	key, base := strings.TrimSpace(w.apiKey), strings.TrimSpace(w.baseURL)
	w.app.Go(func(ctx context.Context) {
		fail := func(err error) {
			w.app.Update(func() { w.busy = false; w.err = errText(err, w.t("provider.saveFailed")) })
		}
		reuse := false
		if g.typ == "copilot" {
			for _, a := range existing {
				if a.Type == "copilot" {
					reuse = true
				}
			}
		}
		if !reuse {
			acc := client.SetupProviderAccount{ID: uniqueID(g.typ, existing), DisplayName: g.label, Type: g.typ}
			if g.needsKey {
				acc.APIKey = key
			}
			if g.needsBaseURL {
				acc.BaseURL = base
			}
			if err := w.app.Client.SetupAddProviderAccount(ctx, acc); err != nil {
				fail(err)
				return
			}
		}
		if g.typ == "copilot" {
			if ok, _ := w.app.Client.SetupCopilotStatus(ctx); ok {
				w.app.Update(func() { w.busy = false; w.providerDone("copilot") })
				return
			}
			code, err := w.app.Client.SetupCopilotLogin(ctx)
			if err != nil {
				w.app.Update(func() { w.busy = false; w.err = errText(err, w.t("provider.copilotStartFailed")) })
				return
			}
			w.app.Update(func() { w.busy = false; w.startCopilotPoll(code) })
			return
		}
		w.app.Update(func() { w.busy = false; w.providerDone(g.typ) })
	})
}

func (w *wizard) startCopilotPoll(code client.SetupCopilotCode) {
	w.copilot = &code
	w.copied = false
	if w.copilotStop != nil {
		w.copilotStop()
	}
	pctx, cancel := context.WithCancel(w.app.Ctx)
	w.copilotStop = cancel
	every := time.Duration(max(code.Interval, 3)) * time.Second
	deadline := time.Now().Add(time.Duration(max(code.ExpiresIn, 900)) * time.Second)
	go func() {
		tk := time.NewTicker(every)
		defer tk.Stop()
		for {
			select {
			case <-pctx.Done():
				return
			case <-tk.C:
			}
			if time.Now().After(deadline) {
				w.app.Update(func() {
					w.copilot = nil
					w.err = w.t("provider.copilotExpired")
				})
				return
			}
			if ok, _ := w.app.Client.SetupCopilotStatus(pctx); ok {
				w.app.Update(func() {
					w.copilot = nil
					w.providerDone("copilot")
				})
				return
			}
		}
	}()
}

func (w *wizard) providerDone(typ string) {
	w.sum.provider = typ
	w.accountsLoad = false
	w.addingNew = false
	w.refreshStatus()
	w.go_(stepModels)
}

func (w *wizard) suggest(refresh bool) {
	prov := w.sum.provider
	w.modelsLoading, w.modelsNotice = true, ""
	if w.mainModel == "" && w.status != nil && w.status.CoderModelValid {
		w.mainModel = w.status.CoderModel
	}
	w.app.Go(func(ctx context.Context) {
		var sm client.SuggestedModels
		var serr error
		if prov != "" {
			sm, serr = w.app.Client.SetupSuggestedModels(ctx, prov, refresh)
		}
		list, _ := w.app.Client.ListModels(ctx)
		w.app.Update(func() {
			w.modelsLoading = false
			if serr != nil {
				w.modelsNotice = errText(serr, w.t("models.suggestFailed"))
			} else if prov != "" && sm.MainModel == "" {
				w.modelsNotice = w.t("models.noModels")
			}
			if sm.MainModel != "" {
				w.mainModel = sm.MainModel
			}
			if sm.FastModel != "" {
				w.fastModel = sm.FastModel
			}
			w.modelOptions = w.modelOptions[:0]
			seen := map[string]bool{}
			for _, m := range list.Models {
				if !seen[m.ID] {
					seen[m.ID] = true
					w.modelOptions = append(w.modelOptions, m.ID)
				}
			}
			for _, id := range []string{w.mainModel, w.fastModel} {
				if id != "" && !seen[id] {
					seen[id] = true
					w.modelOptions = append(w.modelOptions, id)
				}
			}
		})
	})
}

func (w *wizard) saveModels() {
	if strings.TrimSpace(w.mainModel) == "" {
		w.err = w.t("models.mainRequired")
		return
	}
	w.busy, w.err = true, ""
	m, f := strings.TrimSpace(w.mainModel), strings.TrimSpace(w.fastModel)
	w.app.Go(func(ctx context.Context) {
		r, err := w.app.Client.SetupSaveModels(ctx, m, f)
		w.app.Update(func() {
			w.busy = false
			if err != nil {
				w.err = errText(err, w.t("models.saveFailed"))
				return
			}
			w.sum.mainModel, w.sum.fastModel = r.MainModel, r.FastModel
			w.refreshStatus()
			w.go_(stepRemembrances)
		})
	})
}

func (w *wizard) refreshOllama() {
	w.ollChecking = true
	w.app.Go(func(ctx context.Context) {
		s, err := w.app.Client.SetupOllamaStatus(ctx)
		w.app.Update(func() {
			w.ollChecking = false
			if err != nil {
				w.err = errText(err, w.t("remembrances.statusFailed"))
				return
			}
			w.oll = &s
			if w.docModel == "" {
				w.docModel = s.DocumentModel
			}
			if w.codeModel == "" {
				w.codeModel = s.CodeModel
			}
		})
	})
}

func hasModel(models []string, name string) bool {
	n := strings.ToLower(name)
	for _, m := range models {
		m = strings.ToLower(m)
		if m == n || m == n+":latest" {
			return true
		}
	}
	return false
}

// trackJob polls a job until it ends.
func (w *wizard) trackJob(slot string, j client.SetupJob) {
	w.jobs[slot] = &j
	id := j.ID
	w.app.Go(func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			nj, err := w.app.Client.SetupJob(ctx, id)
			if err != nil {
				continue
			}
			done := nj.State != "running"
			w.app.Update(func() {
				w.jobs[slot] = &nj
				if slot == "install" {
					w.installLog = nj.Output
				}
				if done {
					if nj.State == "error" {
						w.err = firstNonEmpty(nj.Error, w.t("remembrances.pullFailed"))
					}
					w.refreshOllama()
				}
			})
			if done {
				return
			}
		}
	})
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

func (w *wizard) pull(model string) {
	w.err = ""
	w.app.Go(func(ctx context.Context) {
		j, err := w.app.Client.SetupOllamaPull(ctx, model)
		w.app.Update(func() {
			if err != nil {
				w.err = errText(err, w.t("remembrances.pullFailed"))
				return
			}
			w.trackJob(model, j)
		})
	})
}

func (w *wizard) install(opt string) {
	w.err, w.confirmOpt = "", ""
	w.app.Go(func(ctx context.Context) {
		j, err := w.app.Client.SetupOllamaInstall(ctx, opt)
		w.app.Update(func() {
			if err != nil {
				w.err = errText(err, w.t("remembrances.installFailed"))
				return
			}
			w.trackJob("install", j)
		})
	})
}

func (w *wizard) startOllama() {
	w.busy, w.err = true, ""
	w.app.Go(func(ctx context.Context) {
		running, err := w.app.Client.SetupOllamaStart(ctx)
		w.app.Update(func() {
			w.busy = false
			switch {
			case err != nil:
				w.err = errText(err, w.t("remembrances.startFailed"))
			case !running:
				w.err = w.t("remembrances.startSlow")
			}
			w.refreshOllama()
		})
	})
}

func (w *wizard) enableRemembrances() {
	w.busy, w.err = true, ""
	d, cm := strings.TrimSpace(w.docModel), strings.TrimSpace(w.codeModel)
	w.app.Go(func(ctx context.Context) {
		err := w.app.Client.SetupRemembrances(ctx, d, cm)
		w.app.Update(func() {
			w.busy = false
			if err != nil {
				w.err = errText(err, w.t("remembrances.enableFailed"))
				return
			}
			w.sum.docModel, w.sum.codeModel, w.sum.remembrances = d, cm, true
			w.go_(stepDone)
		})
	})
}

func (w *wizard) finish() {
	w.busy, w.err = true, ""
	w.app.Go(func(ctx context.Context) {
		_, err := w.app.Client.SetupComplete(ctx)
		w.app.Update(func() {
			w.busy = false
			if err != nil {
				w.err = w.t("done.completeFailed")
				return
			}
			w.status.Needed, w.status.Completed = false, true
			w.leave()
		})
	})
}

func openExternal(u string) { _ = mygo.Shell.OpenExternal(u) }

func copyText(s string) bool { return mygo.Clipboard.Write(transfer.TextData(s)) == nil }
