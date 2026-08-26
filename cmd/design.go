package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/design"
	"github.com/digiogithub/pando/internal/skills/catalog"
)

// The design CLI is the fourth surface of the Design Studio, alongside the TUI,
// the WebUI and ACP. It exists for the cases the others cannot cover: scripting
// an export, checking what a project contains without starting an agent, and
// opening a preview from a machine where the TUI is not running.
//
// Every subcommand takes the same artifact reference — an id, a slug, an id
// prefix or part of a title — resolved by design.Service.Resolve so the shell
// behaves the way a person expects rather than demanding dsg_ ids.

var (
	designJSON       bool
	designKind       string
	designSlug       string
	designSlide      int
	designFormat     string
	designOut        string
	designFullPage   bool
	designLandscape  bool
	designOpenNoWait bool
	designAllSession bool

	designExtractFrom   string
	designExtractTarget string
	designExtractDryRun bool
	designSystemName    string

	designSkill       string
	designSkillCraft  string
	designSkillGlobal bool
	designSkillForce  bool

	designCritiqueVersion  int
	designCritiquePolicy   string
	designCritiqueNoRender bool
	designCritiqueNoRecord bool
)

var designCmd = &cobra.Command{
	Use:   "design",
	Short: "Inspect, open and export design artifacts",
	Long: `Work with the Design Studio artifacts of this project from the shell.

Design artifacts live in the working tree (designer/<slug>/ by default), so they
are committable and editable with any tool. This command is the read/open/export
surface; artifacts are authored by asking the agent.`,
	Example: `
  # List every artifact in this project
  pando design list

  # Open the most recently updated artifact in a browser
  pando design open

  # Open slide 3 of a deck by slug
  pando design open quarterly-review --slide 3

  # Show the version history as JSON
  pando design versions landing --json

  # Export a self-contained HTML file
  pando design export landing --format html --out /tmp/landing.html`,
	// A missing artifact or a browser that will not start is a runtime failure,
	// not a usage error: printing the whole flag list after it buries the one
	// line that says what went wrong.
	SilenceUsage: true,
}

var designListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the design artifacts of this project",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWithDesignService(func(ctx context.Context, svc *design.Service) error {
			artifacts, err := svc.List(ctx, designAllSession)
			if err != nil {
				return err
			}
			if designJSON {
				return printDesignJSON(artifacts)
			}
			if len(artifacts) == 0 {
				fmt.Println("No design artifacts. Ask the agent to create one.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SLUG\tKIND\tVERSION\tUPDATED\tDIR\tID")
			for _, a := range artifacts {
				fmt.Fprintf(w, "%s\t%s\tv%d\t%s\t%s\t%s\n",
					a.Slug, a.Kind, a.CurrentVersion, humanAge(a.UpdatedAt), a.Dir, a.ID)
			}
			return w.Flush()
		})
	},
}

var designCreateCmd = &cobra.Command{
	Use:   "create <title>",
	Short: "Create an empty design artifact",
	Long: `Create an artifact directory, its manifest and version 1.

The artifact is seeded with a minimal renderable placeholder; the design itself
is authored by the agent. Use this when you want the directory to exist (and be
committed) before starting a session.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		title := strings.Join(args, " ")
		skill := strings.TrimSpace(designSkill)
		template, hasTemplate := design.BundledTemplate(skill)
		kind := design.Kind(strings.ToLower(strings.TrimSpace(designKind)))
		if kind == "" && hasTemplate {
			// The template declares the surface it builds, so --kind stays
			// empty by default: forcing web would drop a deck's print styles.
			kind = template.Kind
		}
		if kind == "" {
			kind = design.KindWeb
		}
		if !design.ValidKind(kind) {
			return fmt.Errorf("unsupported kind %q (v1 supports web, deck)", kind)
		}
		if skill != "" && !hasTemplate {
			fmt.Fprintf(os.Stderr, "note: %q is not a bundled template, creating without a scaffold\n", skill)
		}
		return runWithDesignService(func(ctx context.Context, svc *design.Service) error {
			artifact, err := svc.Create(ctx, design.CreateParams{
				Title:   title,
				Kind:    kind,
				Slug:    designSlug,
				SkillID: skill,
			})
			if err != nil {
				return err
			}
			if designJSON {
				return printDesignJSON(artifact)
			}
			fmt.Printf("Created %s (%s) in %s\n", artifact.Slug, artifact.Kind, artifact.Dir)
			fmt.Printf("Id: %s\n", artifact.ID)
			if hasTemplate {
				fmt.Printf("Scaffolded from template: %s\n", template.Name)
				// Only say it is missing when it actually is: a warning that
				// fires after the system was committed teaches the reader to
				// ignore it.
				if _, exists, err := svc.LoadSystem(); template.RequiresSystem && err == nil && !exists {
					fmt.Println("The template expects a design system; this project has none (pando design system init).")
				}
			}
			return nil
		})
	},
}

var designVersionsCmd = &cobra.Command{
	Use:   "versions [artifact]",
	Short: "Show the version history of an artifact",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWithDesignService(func(ctx context.Context, svc *design.Service) error {
			artifact, err := svc.Resolve(ctx, firstArg(args))
			if err != nil {
				return err
			}
			versions, err := svc.Versions(ctx, artifact.ID)
			if err != nil {
				return err
			}
			if designJSON {
				return printDesignJSON(map[string]any{"artifact": artifact, "versions": versions})
			}
			fmt.Printf("%s (%s) — current v%d\n\n", artifact.Title, artifact.Slug, artifact.CurrentVersion)
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "VERSION\tCREATED\tSCORE\tSUMMARY")
			for _, v := range versions {
				score := "-"
				if v.Critique != nil {
					score = fmt.Sprintf("%.1f", v.Critique.Score)
				}
				marker := ""
				if v.Number == artifact.CurrentVersion {
					marker = " *"
				}
				fmt.Fprintf(w, "v%d%s\t%s\t%s\t%s\n", v.Number, marker, humanAge(v.CreatedAt), score, v.Summary)
			}
			return w.Flush()
		})
	},
}

var designOpenCmd = &cobra.Command{
	Use:   "open [artifact]",
	Short: "Open an artifact preview in the browser",
	Long: `Serve an artifact over a loopback preview server and open it in a browser.

The preview lives as long as this command does, so it stays running until you
interrupt it. Use --no-wait to open the artifact's file:// address instead and
return immediately; that skips the server, so relative asset URLs and the
selection bridge are not available.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWithDesignService(func(ctx context.Context, svc *design.Service) error {
			artifact, err := svc.Resolve(ctx, firstArg(args))
			if err != nil {
				return err
			}

			var presentation design.Presentation
			if designOpenNoWait {
				presentation, err = svc.Presentation(ctx, artifact.ID, designSlide, "")
			} else {
				presentation, err = svc.LiveURL(ctx, artifact.ID, designSlide)
			}
			if err != nil {
				return err
			}
			target := presentation.URL
			if designOpenNoWait {
				target = presentation.FileURL
			}

			if designJSON {
				if err := printDesignJSON(presentation); err != nil {
					return err
				}
			} else {
				fmt.Printf("%s (%s) v%d\n%s\n", artifact.Title, artifact.Slug, artifact.CurrentVersion, target)
			}
			if err := auth.OpenBrowser(target); err != nil {
				fmt.Fprintf(os.Stderr, "could not open a browser (%v); the URL above still works\n", err)
			}
			if designOpenNoWait {
				return nil
			}

			fmt.Println("\nServing the preview. Press Ctrl+C to stop.")
			return waitForInterrupt()
		})
	},
}

var designExportCmd = &cobra.Command{
	Use:   "export [artifact]",
	Short: "Export an artifact as HTML, PNG or PDF",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format := strings.ToLower(strings.TrimSpace(designFormat))
		switch format {
		case design.ExportHTML, design.ExportPNG, design.ExportPDF:
		case "":
			format = design.ExportHTML
		default:
			return fmt.Errorf("unsupported format %q (html, png, pdf)", format)
		}
		return runWithDesignService(func(ctx context.Context, svc *design.Service) error {
			artifact, err := svc.Resolve(ctx, firstArg(args))
			if err != nil {
				return err
			}
			slide := designSlide
			if slide == 0 {
				slide = -1
			}
			result, err := svc.Export(ctx, artifact.ID, design.ExportOptions{
				Format:    format,
				Dest:      designOut,
				Slide:     slide,
				FullPage:  designFullPage,
				Landscape: designLandscape,
			})
			if err != nil {
				return err
			}
			if designJSON {
				return printDesignJSON(result)
			}
			fmt.Printf("%s (%d bytes)\n", result.Path, result.Bytes)
			if result.Note != "" {
				fmt.Fprintf(os.Stderr, "note: %s\n", result.Note)
			}
			return nil
		})
	},
}

var designSystemCmd = &cobra.Command{
	Use:   "system",
	Short: "Inspect the shared design system of this project",
	Long: `Read, extract and apply the design tokens every artifact of this project links.

The system is three committed files: tokens.json (the source of truth), the
system.css generated from it, and DESIGN.md, the written contract. Extraction
builds the tokens from something that already looks right — a codebase, a live
page, a screenshot or a written style guide.`,
}

var designSystemShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the design system tokens",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWithDesignService(func(ctx context.Context, svc *design.Service) error {
			ds, exists, err := svc.LoadSystem()
			if err != nil {
				return err
			}
			if designJSON {
				return printDesignJSON(map[string]any{"exists": exists, "system": ds})
			}
			if !exists {
				fmt.Println("No design system committed yet; showing the default that `pando design system init` would write.")
			}
			fmt.Printf("Name: %s\nTokens: %s\nStylesheet: %s\n\n",
				ds.Name, svc.SystemRelPath(design.SystemTokensFile), svc.SystemRelPath(design.SystemStylesheet))
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "TOKEN\tVALUE")
			for _, group := range sortedGroups(ds.Tokens) {
				for _, name := range sortedKeys(ds.Tokens[group]) {
					fmt.Fprintf(w, "--%s-%s\t%s\n", group, name, ds.Tokens[group][name])
				}
			}
			return w.Flush()
		})
	},
}

var designSystemInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Write the default design system if none exists",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWithDesignService(func(ctx context.Context, svc *design.Service) error {
			_, exists, err := svc.LoadSystem()
			if err != nil {
				return err
			}
			if exists {
				return fmt.Errorf("a design system already exists at %s", svc.SystemRelPath(design.SystemTokensFile))
			}
			tokens, css, err := svc.SaveSystem(design.DefaultDesignSystem())
			if err != nil {
				return err
			}
			fmt.Printf("Wrote %s and %s\n", tokens, css)
			return nil
		})
	},
}

var designSystemExamplesCmd = &cobra.Command{
	Use:   "examples",
	Short: "List the bundled style guides",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		names := design.ExampleSystemNames()
		if designJSON {
			out := make([]map[string]string, 0, len(names))
			for _, name := range names {
				out = append(out, map[string]string{"name": name, "title": design.ExampleSystemTitle(name)})
			}
			return printDesignJSON(out)
		}
		if len(names) == 0 {
			fmt.Println("No bundled style guides in this build.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tGUIDE")
		for _, name := range names {
			fmt.Fprintf(w, "%s\t%s\n", name, design.ExampleSystemTitle(name))
		}
		if err := w.Flush(); err != nil {
			return err
		}
		fmt.Printf("\nUse one with: pando design system extract --from text --target <name>\n")
		return nil
	},
}

var designSystemExtractCmd = &cobra.Command{
	Use:   "extract [target]",
	Short: "Build the design system from code, a URL, an image or a style guide",
	Long: `Extract design tokens from something that already looks the way this project should.

Sources:
  code   scan a directory of stylesheets and component files (default: the project root)
  url    render an http(s) page and read its computed styles
  image  read a palette out of a screenshot or a logo (colours only)
  text   read a written style guide: a file path, or a bundled example name

The result replaces tokens.json and system.css, and refreshes the generated
table in DESIGN.md. Prose written in DESIGN.md is preserved.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		source := design.ExtractSource(strings.ToLower(strings.TrimSpace(designExtractFrom)))
		if source == "" {
			source = design.SourceCode
		}
		target := firstArg(args)
		if target == "" {
			target = designExtractTarget
		}
		return runWithDesignService(func(ctx context.Context, svc *design.Service) error {
			result, err := svc.ExtractSystem(ctx, design.ExtractOptions{
				Source: source,
				Target: target,
				Name:   designSystemName,
			})
			if err != nil {
				return err
			}
			if designExtractDryRun {
				if designJSON {
					return printDesignJSON(result)
				}
				fmt.Println("Dry run: nothing written.")
				printExtraction(result, "")
				return nil
			}
			tokens, css, err := svc.SaveSystem(result.System)
			if err != nil {
				return err
			}
			mirrored, mirrorErr := svc.MirrorSystem(ctx, result.System, result.Source, result.Target)
			if mirrorErr != nil {
				// A knowledge base that is unavailable must not turn a written
				// design system into a failed command.
				fmt.Fprintf(os.Stderr, "warning: %v\n", mirrorErr)
			}
			if designJSON {
				return printDesignJSON(map[string]any{
					"result": result, "tokens": tokens, "stylesheet": css, "mirrored": mirrored,
				})
			}
			printExtraction(result, mirrored)
			fmt.Printf("\nWrote %s, %s and %s\n", tokens, css, svc.ContractPath())
			return nil
		})
	},
}

// printExtraction shows what an extraction read and produced.
func printExtraction(result design.ExtractResult, mirrored string) {
	fmt.Printf("Source: %s", result.Source)
	if result.Target != "" {
		fmt.Printf(" %s", result.Target)
	}
	fmt.Printf("\nName: %s\n", result.System.Name)
	if len(result.Scanned) > 0 {
		limit := len(result.Scanned)
		if limit > 8 {
			limit = 8
		}
		fmt.Printf("Read %d source(s): %s", len(result.Scanned), strings.Join(result.Scanned[:limit], ", "))
		if len(result.Scanned) > limit {
			fmt.Printf(", +%d more", len(result.Scanned)-limit)
		}
		fmt.Println()
	}
	for _, note := range result.Notes {
		fmt.Printf("Note: %s\n", note)
	}
	if mirrored != "" {
		fmt.Printf("Mirrored to the knowledge base as %s\n", mirrored)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "\nTOKEN\tVALUE")
	for _, group := range sortedGroups(result.System.Tokens) {
		for _, name := range sortedKeys(result.System.Tokens[group]) {
			fmt.Fprintf(w, "--%s-%s\t%s\n", group, name, result.System.Tokens[group][name])
		}
	}
	_ = w.Flush()
}

var designSystemApplyCmd = &cobra.Command{
	Use:   "apply [artifact]",
	Short: "Link the design system into an artifact and audit its hardcoded values",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWithDesignService(func(ctx context.Context, svc *design.Service) error {
			artifact, err := svc.Resolve(ctx, firstArg(args))
			if err != nil {
				return err
			}
			result, err := svc.ApplySystem(ctx, artifact.ID)
			if err != nil {
				return err
			}
			if designJSON {
				return printDesignJSON(result)
			}
			if result.Linked {
				fmt.Printf("Linked %s from %s\n", result.Stylesheet, result.Entry)
			} else {
				fmt.Printf("%s already links %s\n", result.Entry, result.Stylesheet)
			}
			fmt.Printf("Design system: %s\nAudited %d file(s)\n", result.System, result.Scanned)
			if len(result.Findings) == 0 {
				fmt.Println("\nNo hardcoded values that a token already covers.")
				return nil
			}
			fmt.Println("\nHardcoded values a token already covers:")
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "WHERE\tPROPERTY\tVALUE\tUSE")
			for _, f := range result.Findings {
				fmt.Fprintf(w, "%s:%d\t%s\t%s\tvar(%s)\n", f.File, f.Line, f.Property, f.Value, f.Token)
			}
			if err := w.Flush(); err != nil {
				return err
			}
			if result.Truncated {
				fmt.Println("... more findings not listed")
			}
			return nil
		})
	},
}

func init() {
	designCmd.PersistentFlags().BoolVar(&designJSON, "json", false, "Emit JSON instead of a table")

	designListCmd.Flags().BoolVar(&designAllSession, "session", false, "Only artifacts created by the current session")

	designCreateCmd.Flags().StringVar(&designKind, "kind", "", "Artifact kind: web or deck (default: the template's, else web)")
	designCreateCmd.Flags().StringVar(&designSkill, "skill", "", "Design template to scaffold from (see: pando design skills)")
	designCreateCmd.Flags().StringVar(&designSlug, "slug", "", "Directory slug (derived from the title when empty)")

	designOpenCmd.Flags().IntVar(&designSlide, "slide", 0, "Open at this deck slide (1-based)")
	designOpenCmd.Flags().BoolVar(&designOpenNoWait, "no-wait", false, "Open the file:// address and return instead of serving")

	designExportCmd.Flags().StringVar(&designFormat, "format", "html", "Export format: html, png or pdf")
	designExportCmd.Flags().StringVar(&designOut, "out", "", "Output file (defaults to exports/ inside the artifact)")
	designExportCmd.Flags().IntVar(&designSlide, "slide", 0, "Export a single deck slide (PNG only)")
	designExportCmd.Flags().BoolVar(&designFullPage, "full-page", false, "Capture beyond the viewport (PNG only)")
	designExportCmd.Flags().BoolVar(&designLandscape, "landscape", false, "Print landscape (PDF only)")

	designSystemExtractCmd.Flags().StringVar(&designExtractFrom, "from", "code", "Extraction source: code, url, image or text")
	designSystemExtractCmd.Flags().StringVar(&designExtractTarget, "target", "", "Directory, URL, image path, style-guide path or bundled example name")
	designSystemExtractCmd.Flags().StringVar(&designSystemName, "name", "", "Name for the extracted system")
	designSystemExtractCmd.Flags().BoolVar(&designExtractDryRun, "dry-run", false, "Show what would be extracted without writing it")

	designCritiqueCmd.Flags().IntVar(&designCritiqueVersion, "version", 0, "Version to critique (default: the current one)")
	designCritiqueCmd.Flags().StringVar(&designCritiquePolicy, "policy", "", "Override the gate: none, standard or strict")
	designCritiqueCmd.Flags().BoolVar(&designCritiqueNoRender, "no-render", false, "Skip the browser: run the design-system checks only")
	designCritiqueCmd.Flags().BoolVar(&designCritiqueNoRecord, "no-record", false, "Do not store the pass in the artifact's critique history")

	designSkillsShowCmd.Flags().StringVar(&designSkillCraft, "craft", "", "Read a craft reference instead of a template")
	designSkillsInstallCmd.Flags().BoolVar(&designSkillGlobal, "global", false, "Install into the global skills directory instead of the project one")
	designSkillsInstallCmd.Flags().BoolVar(&designSkillForce, "force", false, "Replace an already installed copy, discarding any edits to it")

	designSkillsCmd.AddCommand(designSkillsShowCmd, designSkillsInstallCmd)

	designSystemCmd.AddCommand(designSystemShowCmd, designSystemInitCmd,
		designSystemExtractCmd, designSystemApplyCmd, designSystemExamplesCmd)
	designCmd.AddCommand(designListCmd, designCreateCmd, designVersionsCmd, designOpenCmd,
		designExportCmd, designCritiqueCmd, designSystemCmd, designSkillsCmd)
	// Cobra reads SilenceUsage off the command that failed, not off its parent,
	// so setting it on the tree is what actually keeps a "not found" from
	// printing the whole flag list under it.
	silenceUsage(designCmd)
	rootCmd.AddCommand(designCmd)
}

// silenceUsage marks a command and everything under it as reporting runtime
// errors rather than usage errors.
func silenceUsage(cmd *cobra.Command) {
	cmd.SilenceUsage = true
	for _, sub := range cmd.Commands() {
		silenceUsage(sub)
	}
}

// runWithDesignService boots the minimum a design command needs: the project
// config, the database and a provider. The provider is installed as the process
// default because the preview server resolves artifacts through it.
func runWithDesignService(fn func(ctx context.Context, svc *design.Service) error) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	if _, err := config.Load(cwd, false, ""); err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	conn, err := db.Connect()
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer conn.Close()

	provider, err := design.NewProvider(conn)
	if err != nil {
		return fmt.Errorf("start the design subsystem: %w", err)
	}
	design.SetDefaultProvider(provider)
	defer func() {
		design.ClosePreviewServer()
		design.CloseDefaultProvider()
	}()

	return fn(context.Background(), provider.Service(""))
}

// waitForInterrupt blocks until the user stops the command. `design open`
// serves the preview from this process, so returning would take the URL that
// was just handed to a browser down with it.
func waitForInterrupt() error {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	fmt.Println("\nPreview stopped.")
	return nil
}

// sortedKeys keeps token output stable: a map iteration order that changes
// between runs would make `pando design system show` useless in a diff.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedGroups(m map[string]map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func printDesignJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// humanAge keeps the tables narrow: an exact timestamp is available with --json
// and is noise in a list whose point is "which one did I touch last".
func humanAge(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}

var designCritiqueCmd = &cobra.Command{
	Use:   "critique [artifact]",
	Short: "Score an artifact against the automated quality rules",
	Long: `Run the quality gate over a design artifact.

The pass renders the artifact and checks accessibility (alt text, control names,
heading order, colour contrast, target size), runtime errors, layout overflow,
deck print behaviour and design-system adherence. It scores the result and says
whether the artifact is finished, needs another round, or has spent its rounds.

Nothing is edited: the pass is safe to run at any point. On a machine with no
Chromium it still runs the design-system checks and says which rules did not.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if policy := designCritiquePolicy; policy != "" && design.NormalizePolicy(policy) == "" {
			return fmt.Errorf("unknown critique policy %q (none, standard, strict)", policy)
		}
		return runWithDesignService(func(ctx context.Context, svc *design.Service) error {
			artifact, err := svc.Resolve(ctx, firstArg(args))
			if err != nil {
				return err
			}
			report, err := svc.Critique(ctx, artifact.ID, design.CritiqueOptions{
				Version:    designCritiqueVersion,
				SkipRender: designCritiqueNoRender,
				Policy:     designCritiquePolicy,
				Record:     !designCritiqueNoRecord,
			})
			if err != nil {
				return err
			}
			if designJSON {
				return printDesignJSON(report)
			}
			printCritiqueReport(report)
			return nil
		})
	},
}

// printCritiqueReport writes the verdict first and the findings under it, worst
// first: the reader wants to know whether to keep going before they read why.
func printCritiqueReport(report design.CritiqueReport) {
	verdict := "STOP"
	switch {
	case report.Decision.Pass:
		verdict = "PASS"
	case report.Decision.Iterate:
		verdict = "ITERATE"
	}
	fmt.Printf("%s v%d — %.1f/10 (automated %.1f) — %s\n",
		report.Artifact.Title, report.Version, report.Critique.Score, report.Audit.Score, verdict)
	fmt.Printf("%s\n", report.Decision.Reason)
	fmt.Printf("policy %s, threshold %.1f, round %d of %d\n",
		report.Decision.Policy, report.Decision.Threshold, report.Decision.Round, report.Decision.MaxRounds)
	if !report.Rendered {
		fmt.Fprintf(os.Stderr, "\nnote: not rendered (%s); accessibility, runtime and layout rules did not run\n",
			report.RenderError)
	}

	if len(report.Critique.Issues) == 0 {
		fmt.Println("\nNo findings.")
		return
	}
	fmt.Printf("\nFindings (%d):\n", len(report.Critique.Issues))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SEVERITY\tRULE\tNODE\tFINDING")
	for _, issue := range report.Critique.Issues {
		rule := issue.Code
		if rule == "" {
			rule = "-"
		}
		node := issue.NodeID
		if node == "" {
			node = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", issue.Severity, rule, node, issue.Message)
	}
	if err := w.Flush(); err != nil {
		fmt.Fprintf(os.Stderr, "note: %v\n", err)
	}
	for code, count := range report.Audit.Counts {
		shown := 0
		for _, issue := range report.Audit.Issues {
			if issue.Code == code {
				shown++
			}
		}
		if count > shown {
			fmt.Printf("... %s fired %d time(s), only the first %d listed\n", code, count, shown)
		}
	}
}

var designSkillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "List the bundled design templates and craft references",
	Long: `List the design templates artifacts can be scaffolded from.

A template is a design skill: it says what to build, in what order and what to
avoid. Pass its name to "pando design create --skill <name>", or ask the agent
for it by name. Templates are embedded in the binary; installing one only makes
it load as an ordinary skill in later sessions.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		templates, err := design.BundledTemplates()
		if err != nil {
			return err
		}
		if designJSON {
			return printDesignJSON(map[string]any{
				"skills": templates,
				"craft":  design.CraftReferenceNames(),
			})
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tKIND\tCATEGORY\tSYSTEM\tDESCRIPTION")
		for _, t := range templates {
			kind := string(t.Kind)
			if !t.Startable {
				kind = "-"
			}
			system := "optional"
			if t.RequiresSystem {
				system = "required"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", t.Name, kind, t.Category, system, t.Description)
		}
		if err := w.Flush(); err != nil {
			return err
		}
		if craft := design.CraftReferenceNames(); len(craft) > 0 {
			fmt.Printf("\nCraft references: %s\n", strings.Join(craft, ", "))
		}
		return nil
	},
}

var designSkillsShowCmd = &cobra.Command{
	Use:   "show [template]",
	Short: "Print a design template or craft reference",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if craft := strings.TrimSpace(designSkillCraft); craft != "" {
			body, ok := design.CraftReference(craft)
			if !ok {
				return fmt.Errorf("unknown craft reference %q (have %s)", craft,
					strings.Join(design.CraftReferenceNames(), ", "))
			}
			fmt.Print(body)
			return nil
		}
		name := strings.TrimSpace(firstArg(args))
		if name == "" {
			return errors.New("give a template name, or --craft to read a craft reference")
		}
		body, ok := design.BundledTemplateContent(name)
		if !ok {
			return fmt.Errorf("unknown design template %q (see: pando design skills)", name)
		}
		fmt.Print(body)
		return nil
	},
}

var designSkillsInstallCmd = &cobra.Command{
	Use:   "install <template>",
	Short: "Copy a bundled design template into a skills directory",
	Long: `Write a bundled template, plus the craft references it declares, into a
skills directory so it loads as an ordinary skill.

The craft references are copied into the skill directory rather than shared: a
skill that reads a file outside its own directory breaks the moment it is copied
somewhere else, and the skill manager refuses to load one.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := strings.TrimSpace(args[0])
		targetDir := catalog.ResolveSkillsDir(!designSkillGlobal)
		if !filepath.IsAbs(targetDir) {
			// Installing copies embedded files into a directory. It needs
			// neither the configuration nor the database, so it resolves the
			// project root itself rather than starting the subsystem.
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("get working directory: %w", err)
			}
			targetDir = filepath.Join(cwd, targetDir)
		}
		targetDir = filepath.Join(targetDir, "design")

		written, err := design.InstallBundle(name, targetDir, designSkillForce)
		if err != nil {
			return err
		}
		if designJSON {
			return printDesignJSON(map[string]any{"name": name, "dir": targetDir, "files": written})
		}
		fmt.Printf("Installed %s into %s\n", name, targetDir)
		for _, path := range written {
			fmt.Printf("  %s\n", path)
		}
		return nil
	},
}
