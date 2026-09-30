package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/modelrouter"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/systemone"
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
		if n := doctorModelAutoMode(ctx, cfg, cmd.OutOrStdout()); n > 0 {
			return fmt.Errorf("%d problem(s) found", n)
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
