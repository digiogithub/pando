package extensions

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/pkg/extension"
)

// CLI commands are built from *unprovisioned* instances (extension.Preview) so
// that `pando ext --help` costs nothing: printing help must not open databases
// or contact servers. The provisioned instance is only obtained when a command
// actually runs, which is why every RunE goes through commandRunner.

// commandRunner loads the manager at most once, on the first command that
// actually executes, and resolves the provisioned instance to run against.
type commandRunner struct {
	once sync.Once
	mgr  *extension.Manager
}

func (r *commandRunner) provider(ctx context.Context, id extension.ID) (extension.CommandProvider, error) {
	r.once.Do(func() {
		// Most commands under `pando ext` never go through a code path that
		// loads configuration, so load it here if nobody has. Without this an
		// enabled extension would look disabled.
		if config.Get() == nil {
			cwd, err := os.Getwd()
			if err == nil {
				if _, cfgErr := config.Load(cwd, false, ""); cfgErr != nil {
					logging.Warn("Could not load configuration for extension commands", "error", cfgErr)
				}
			}
		}
		// The manager is built here, not when the command tree was assembled:
		// the tree is built in init(), before any configuration has been read,
		// so a manager captured then would see no [Extensions] settings at all.
		r.mgr = NewManager(Options{})
		// Per-extension failures are recorded in the statuses rather than
		// returned, so there is nothing to abort on here.
		_ = r.mgr.Load(ctx)
		if err := r.mgr.Start(ctx); err != nil {
			logging.Warn("Some extensions failed to start for the CLI", "error", err)
		}
	})
	inst := r.mgr.Instance(id)
	if inst == nil {
		return nil, fmt.Errorf("extension %s is not loaded; check `pando extensions list`", id)
	}
	cp, ok := inst.(extension.CommandProvider)
	if !ok {
		return nil, fmt.Errorf("extension %s no longer provides commands", id)
	}
	return cp, nil
}

// stop releases whatever the runner loaded. Safe to call when nothing was.
func (r *commandRunner) stop(ctx context.Context) {
	if r.mgr == nil {
		return
	}
	if err := r.mgr.Stop(ctx); err != nil {
		logging.Warn("Extension shutdown reported errors", "error", err)
	}
	r.mgr.Cleanup()
}

// Commands builds the cobra tree for every command contributed by an extension
// compiled into this binary. It is safe to call from init(): nothing is
// provisioned and no configuration is required.
//
// Whether an extension is actually enabled is decided when a command runs, not
// here — see commandRunner.
//
// Name collisions are rejected rather than resolved: two extensions claiming
// `sync` is a build-time mistake, and silently dropping one would make the
// binary's behaviour depend on registration order.
func Commands() []*cobra.Command {
	providers := extension.Preview[extension.CommandProvider](extension.NewManager(extension.Options{}))
	if len(providers) == 0 {
		return nil
	}
	runner := &commandRunner{}

	var out []*cobra.Command
	taken := make(map[string]extension.ID)
	for _, p := range providers {
		id := p.ExtensionInfo().ID
		specs, ok := guardValue("CommandProvider.Commands", id, p.Commands)
		if !ok {
			continue
		}
		for _, spec := range specs {
			name := commandName(spec.Use)
			if name == "" {
				logging.Warn("Extension command without a name ignored", "extension", id)
				continue
			}
			if owner, dup := taken[name]; dup {
				logging.Error("Duplicate extension command ignored",
					"command", name, "extension", id, "already_provided_by", owner)
				continue
			}
			taken[name] = id
			out = append(out, buildCommand(runner, id, spec, nil))
		}
	}
	return out
}

// commandName is the first word of a cobra-style Use string.
func commandName(use string) string {
	fields := strings.Fields(use)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// buildCommand converts one contract command, recursing into subcommands. path
// is the chain of names above this command, used to find it again on the
// provisioned instance at run time.
func buildCommand(runner *commandRunner, id extension.ID, spec extension.Command, path []string) *cobra.Command {
	here := append(append([]string{}, path...), commandName(spec.Use))

	cmd := &cobra.Command{
		Use:     spec.Use,
		Short:   spec.Short,
		Long:    spec.Long,
		Aliases: spec.Aliases,
	}
	declared := declareFlags(cmd, id, spec.Flags)

	if spec.Run != nil {
		cmd.RunE = func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			provider, err := runner.provider(ctx, id)
			if err != nil {
				return err
			}
			defer runner.stop(ctx)

			specs, ok := guardValue("CommandProvider.Commands", id, provider.Commands)
			if !ok {
				return fmt.Errorf("extension %s failed while listing its commands", id)
			}
			live, err := findCommand(specs, here)
			if err != nil {
				return err
			}
			if live.Run == nil {
				return fmt.Errorf("command %q does nothing", strings.Join(here, " "))
			}
			// A panicking command becomes a normal non-zero exit with a
			// message. cobra would print a Go stack trace otherwise, which
			// tells a user nothing and looks like Pando itself crashed.
			return guardErr("Command.Run", id, func() error {
				return live.Run(ctx, args, collectFlags(c, declared))
			})
		}
	}

	for _, sub := range spec.Subcommands {
		cmd.AddCommand(buildCommand(runner, id, sub, here))
	}
	return cmd
}

// findCommand walks the provisioned instance's command tree along path. The
// tree is re-read at run time because Commands() may legitimately return a
// different set once the extension is provisioned; the names must still match.
func findCommand(cmds []extension.Command, path []string) (extension.Command, error) {
	for i, want := range path {
		found := false
		for _, c := range cmds {
			if commandName(c.Use) != want {
				continue
			}
			if i == len(path)-1 {
				return c, nil
			}
			cmds = c.Subcommands
			found = true
			break
		}
		if !found {
			break
		}
	}
	return extension.Command{}, fmt.Errorf("command %q is no longer provided", strings.Join(path, " "))
}

// declareFlags registers the contract flags on a cobra command and returns the
// names that were accepted, so collectFlags reads back exactly those.
func declareFlags(cmd *cobra.Command, id extension.ID, flags []extension.Flag) []string {
	names := make([]string, 0, len(flags))
	for _, f := range flags {
		if f.Name == "" {
			continue
		}
		switch v := f.Value.(type) {
		case bool:
			cmd.Flags().BoolP(f.Name, f.Shorthand, v, f.Usage)
		case string:
			cmd.Flags().StringP(f.Name, f.Shorthand, v, f.Usage)
		case int:
			cmd.Flags().IntP(f.Name, f.Shorthand, v, f.Usage)
		case []string:
			cmd.Flags().StringSliceP(f.Name, f.Shorthand, v, f.Usage)
		default:
			// An unsupported type is a programming error in the extension, and
			// a flag that silently does not exist is worse than a loud skip.
			logging.Error("Unsupported extension flag type, flag skipped",
				"extension", id, "flag", f.Name, "type", fmt.Sprintf("%T", f.Value))
			continue
		}
		names = append(names, f.Name)
	}
	return names
}

// collectFlags reads the declared flags back into the contract's Flags map.
func collectFlags(cmd *cobra.Command, declared []string) extension.Flags {
	out := make(extension.Flags, len(declared))
	for _, name := range declared {
		f := cmd.Flags().Lookup(name)
		if f == nil {
			continue
		}
		switch f.Value.Type() {
		case "bool":
			v, _ := cmd.Flags().GetBool(name)
			out[name] = v
		case "string":
			v, _ := cmd.Flags().GetString(name)
			out[name] = v
		case "int":
			v, _ := cmd.Flags().GetInt(name)
			out[name] = v
		case "stringSlice":
			v, _ := cmd.Flags().GetStringSlice(name)
			out[name] = v
		}
	}
	return out
}
