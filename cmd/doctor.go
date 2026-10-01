package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/modelrouter"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/systemone"
	"github.com/digiogithub/pando/internal/mesnada/persona"
	"github.com/digiogithub/pando/internal/mesnada/persona/builtin"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check the health of optional Pando features",
	Long: `Runs read-only diagnostics and prints actionable hints.

Currently checks model auto mode: whether the decision provider is reachable
and authorized, the Ollama version (0.35 or newer), whether the router model is
installed and decision-capable, and whether the models used by the routes are
known and usable.

Examples:
  pando doctor`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		cfg, err := config.Load(cwd, false, "")
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		problems := doctorModelAutoMode(ctx, cfg, cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout())
		problems += doctorPersonaAutoSelect(ctx, cfg, doctorPersonaManager(cfg), cmd.OutOrStdout())
		if problems > 0 {
			return fmt.Errorf("%d problem(s) found", problems)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

// doctorModelAutoMode prints the model auto mode diagnostics to w and returns
// the number of problems found (warnings are not problems).
func doctorModelAutoMode(ctx context.Context, cfg *config.Config, w io.Writer) int {
	problems := 0
	fail := func(format string, args ...any) {
		problems++
		fmt.Fprintf(w, "  [FAIL] "+format+"\n", args...)
	}
	ok := func(format string, args ...any) { fmt.Fprintf(w, "  [ OK ] "+format+"\n", args...) }
	warn := func(format string, args ...any) { fmt.Fprintf(w, "  [WARN] "+format+"\n", args...) }
	hint := func(format string, args ...any) { fmt.Fprintf(w, "         hint: "+format+"\n", args...) }

	fmt.Fprintln(w, "Model auto mode")
	m := cfg.ModelAutoMode
	if !m.Enabled {
		fmt.Fprintln(w, "  disabled (enable it in Settings > Model auto mode or set [ModelAutoMode] Enabled = true)")
		return 0
	}

	errs, warnings := config.ValidateModelAutoMode(m)
	for _, e := range errs {
		fail("config %s: %s", e.Field, e.Message)
	}
	for _, msg := range warnings {
		warn("%s", msg)
	}
	if len(m.EnabledRoutes()) == 0 {
		warn("no enabled routes: every prompt goes to the coder model")
	} else {
		ok("%d enabled route(s), threshold %.2f", len(m.EnabledRoutes()), m.EffectiveThreshold())
	}

	kind := m.Router.EffectiveProvider()
	fmt.Fprintf(w, "  router: %s at %s, model %q\n", kind, m.Router.EffectiveBaseURL(), m.Router.Model)
	p, err := modelrouter.ProviderFor(m.Router, 10*time.Second)
	if err != nil {
		fail("cannot build the decision provider: %v", err)
		return problems
	}
	hctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rep := p.Health(hctx, m.Router.Model)
	doctorReport(rep, kind, m.Router.Model, ok, fail, hint)

	// Routes: each model must be known and its provider usable.
	lookup := modelrouter.DefaultLookup(cfg)
	for _, r := range m.EnabledRoutes() {
		for i, id := range append([]string{string(r.Model)}, fallbackIDs(r)...) {
			role := "model"
			if i > 0 {
				role = "fallback"
			}
			info, found := lookup(models.ModelID(id))
			switch {
			case !found || !info.Known:
				fail("route %q %s %q is not a known model", r.ID, role, id)
				hint("pick a model from the selector; provider models are listed after the provider is configured")
			case !info.Enabled:
				fail("route %q %s %q belongs to a disabled or unconfigured provider", r.ID, role, id)
				hint("configure the provider's credentials or remove the route")
			default:
				ok("route %q %s %q", r.ID, role, id)
			}
		}
	}
	return problems
}

// doctorPersonaManager loads the personas the way the app does: built-ins plus
// the user persona path. It returns nil when they cannot be loaded.
func doctorPersonaManager(cfg *config.Config) *persona.Manager {
	path := cfg.PersonaAutoSelect.PersonaPath
	if path == "" {
		path = cfg.Mesnada.Orchestrator.PersonaPath
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	mgr, err := persona.NewManagerWithBuiltins(builtin.FS, path)
	if err != nil {
		return nil
	}
	return mgr
}

// doctorPersonaAutoSelect prints the persona auto-select diagnostics to w and
// returns the number of problems found (warnings are not problems).
func doctorPersonaAutoSelect(ctx context.Context, cfg *config.Config, mgr *persona.Manager, w io.Writer) int {
	problems := 0
	fail := func(format string, args ...any) {
		problems++
		fmt.Fprintf(w, "  [FAIL] "+format+"\n", args...)
	}
	ok := func(format string, args ...any) { fmt.Fprintf(w, "  [ OK ] "+format+"\n", args...) }
	warn := func(format string, args ...any) { fmt.Fprintf(w, "  [WARN] "+format+"\n", args...) }
	hint := func(format string, args ...any) { fmt.Fprintf(w, "         hint: "+format+"\n", args...) }

	fmt.Fprintln(w, "Persona auto-select")
	if !cfg.PersonaAutoSelect.Enabled {
		fmt.Fprintln(w, "  disabled (enable it in Settings > Agents or set [PersonaAutoSelect] Enabled = true)")
		return 0
	}
	ok("enabled")

	agentCfg := cfg.Agents[config.AgentPersonaSelector]
	useDecision := agentCfg.UseDecisionModel

	// Personas offered to the classifier.
	if mgr == nil {
		warn("personas could not be loaded")
	} else {
		total := len(mgr.Descriptions())
		offered, dropped := modelrouter.PersonaCap(total)
		if dropped > 0 {
			warn("%d personas available, %d offered to the decision model, %d dropped by the 25-persona cap (first 25 in name order)", total, offered, dropped)
		} else {
			ok("%d persona(s) offered", total)
		}
	}

	// Fallback (or main, when the option is off) selector model.
	lookup := modelrouter.DefaultLookup(cfg)
	role := "selector model"
	if useDecision {
		role = "fallback model"
	}
	if id := agentCfg.Model; id != "" {
		info, found := lookup(id)
		if modelOK := found && info.Known && info.Enabled; !modelOK {
			problemMsg := "is not a known model"
			if found && info.Known {
				problemMsg = "belongs to a disabled or unconfigured provider"
			}
			if useDecision {
				warn("persona-selector %s %q %s (the previous or default persona is used when the decision model fails)", role, id, problemMsg)
			} else {
				fail("persona-selector %s %q %s", role, id, problemMsg)
			}
		} else {
			ok("persona-selector %s %q", role, id)
		}
	}
	if agentCfg.Model == "" {
		if useDecision {
			warn("persona-selector agent has no fallback model: the previous or default persona is used when the decision model fails")
		} else {
			fail("persona-selector agent has no model: nothing selects the persona")
			hint("pick a model for the persona-selector agent in Settings > Agents")
		}
	}

	if !useDecision {
		fmt.Fprintln(w, "  decision model: off (persona-selector > Use decision model is disabled; the agent's LLM selects the persona)")
		return problems
	}
	ok("decision model option on")

	m := cfg.ModelAutoMode
	if strings.TrimSpace(m.Router.Model) == "" {
		fail("no router model configured under Model auto mode > Router")
		hint("set the router provider and model in Settings > Model auto mode")
		return problems
	}
	kind := m.Router.EffectiveProvider()
	fmt.Fprintf(w, "  router: %s at %s, model %q (shared with model auto mode)\n", kind, m.Router.EffectiveBaseURL(), m.Router.Model)
	p, err := modelrouter.ProviderFor(m.Router, 10*time.Second)
	if err != nil {
		fail("cannot build the decision provider: %v", err)
		return problems
	}
	hctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	doctorReport(p.Health(hctx, m.Router.Model), kind, m.Router.Model, ok, fail, hint)
	return problems
}

func doctorReport(rep systemone.HealthReport, kind config.DecisionProviderKind, model string,
	ok func(string, ...any), fail func(string, ...any), hint func(string, ...any)) {

	if !rep.Reachable {
		fail("decision provider is not reachable")
		if kind == config.DecisionProviderOllama {
			hint("start Ollama (`ollama serve`) or set Router.BaseURL")
		} else {
			hint("check Router.BaseURL and your network")
		}
		return
	}
	ok("decision provider reachable (%d ms)", rep.LatencyMs)
	if !rep.Authorized {
		fail("decision provider rejected the credentials")
		hint("set Router.APIKey (or $%s for typesafe)", config.TypeSafeAPIKeyEnv)
		return
	}
	ok("credentials accepted")
	if kind == config.DecisionProviderOllama {
		if !rep.VersionOK {
			fail("Ollama %q is too old for System One", rep.Version)
			hint("upgrade Ollama to 0.35 or newer")
			return
		}
		ok("Ollama version %s (>= 0.35)", rep.Version)
		if !rep.ModelFound {
			fail("router model %q is not installed", model)
			hint("%s", fmt.Sprintf("ollama pull %s   (suggested: %s)", model, systemone.OllamaPullHint))
			return
		}
		ok("router model %q installed", model)
		if !rep.IsDecision {
			fail("router model %q does not have the \"decision\" capability", model)
			hint("use a decision model such as tev1:0.8b (%s)", systemone.OllamaPullHint)
			return
		}
		ok("router model is decision-capable")
	}
	if len(rep.Problems) > 0 {
		for _, pr := range rep.Problems {
			fail("%s", pr)
		}
		return
	}
	ok("router answered a probe request")
}

func fallbackIDs(r config.ModelAutoRoute) []string {
	out := make([]string, 0, len(r.Fallbacks))
	for _, f := range r.Fallbacks {
		out = append(out, strings.TrimSpace(string(f)))
	}
	return out
}
