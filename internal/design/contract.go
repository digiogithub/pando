package design

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/digiogithub/pando/internal/config"
)

// The design system is a contract, not a prompt. tokens.json is the machine
// half — the values the stylesheet and the constraint block are generated from.
// DESIGN.md is the human half: the rules a reviewer reads and the designer is
// held to. Both are committed, and the generated part of DESIGN.md is fenced by
// markers so regenerating it never destroys what a person wrote around it.
const (
	// SystemContractFile is the prose half of the design system.
	SystemContractFile = "DESIGN.md"

	contractTokensBegin = "<!-- pando:tokens:begin -->"
	contractTokensEnd   = "<!-- pando:tokens:end -->"
)

// LoadSystemAt reads a design system from a layout without a Service, which is
// what the prompt builder needs: it runs before any session exists.
func LoadSystemAt(layout Layout) (DesignSystem, bool, error) {
	path := filepath.Join(layout.SystemPath(), SystemTokensFile)
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultDesignSystem(), false, nil
	}
	if err != nil {
		return DesignSystem{}, false, fmt.Errorf("design: read design system: %w", err)
	}
	ds, err := decodeSystem(raw)
	if err != nil {
		return DesignSystem{}, false, err
	}
	return ds, true, nil
}

// ContractPath returns the absolute path of DESIGN.md.
func (s *Service) ContractPath() string {
	return filepath.Join(s.layout.SystemPath(), SystemContractFile)
}

// writeContract creates or refreshes DESIGN.md. Only the fenced token section is
// rewritten; prose above and below it is the user's and is preserved verbatim.
// A file with no markers is left untouched apart from an appended section, so
// hand-writing DESIGN.md first and running the tools afterwards is safe.
func (s *Service) writeContract(ds DesignSystem) (string, error) {
	path := s.ContractPath()
	generated := ds.TokenSection()

	existing, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		if err := os.WriteFile(path, []byte(ds.Contract()), 0o644); err != nil {
			return "", fmt.Errorf("design: write %s: %w", SystemContractFile, err)
		}
		return path, nil
	case err != nil:
		return "", fmt.Errorf("design: read %s: %w", SystemContractFile, err)
	}

	body := string(existing)
	start := strings.Index(body, contractTokensBegin)
	end := strings.Index(body, contractTokensEnd)
	var updated string
	if start >= 0 && end > start {
		updated = body[:start] + generated + body[end+len(contractTokensEnd):]
	} else {
		updated = strings.TrimRight(body, "\n") + "\n\n" + generated + "\n"
	}
	if updated == body {
		return path, nil
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return "", fmt.Errorf("design: write %s: %w", SystemContractFile, err)
	}
	return path, nil
}

// TokenSection renders the generated, fenced half of DESIGN.md.
func (ds DesignSystem) TokenSection() string {
	var b strings.Builder
	b.WriteString(contractTokensBegin)
	b.WriteString("\n\n## Tokens\n\nGenerated from `")
	b.WriteString(SystemTokensFile)
	b.WriteString("`. Edit the tokens, not this table.\n\n| Token | Value |\n| --- | --- |\n")
	for _, group := range sortedTokenGroups(ds.Tokens) {
		for _, name := range sortedTokenNames(ds.Tokens[group]) {
			fmt.Fprintf(&b, "| `--%s-%s` | `%s` |\n", group, name, ds.Tokens[group][name])
		}
	}
	if len(ds.Fonts) > 0 {
		b.WriteString("\nImported stylesheets:\n\n")
		for _, f := range ds.Fonts {
			fmt.Fprintf(&b, "- `%s`\n", f)
		}
	}
	b.WriteString("\n")
	b.WriteString(contractTokensEnd)
	return b.String()
}

// Contract renders a complete DESIGN.md for a system that has none yet. The
// prose is a starting point on purpose: the rules that matter to a project are
// the ones its designers write down, and an empty file invites nobody to write
// them.
func (ds DesignSystem) Contract() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Design system — %s\n\n", ds.Name)
	b.WriteString(`This file is the contract every design artifact in this project is held to.
Pando reads it before designing and the reviewer reads it afterwards. Edit the
prose freely; the token table below is generated.

## Rules

- Use the tokens. Never write a raw colour, font stack, spacing value or radius
  that is not `)
	fmt.Fprintf(&b, "`var(--group-name)`.\n")
	b.WriteString(`- Link the generated stylesheet from every artifact entry document.
- Prefer an existing component pattern over inventing a new one.
- If a design genuinely needs a value the system does not have, add the token
  first, then use it.

## Voice

_Describe the tone this project's designs should carry._

`)
	b.WriteString(ds.TokenSection())
	b.WriteString("\n")
	return b.String()
}

// ConstraintBlock renders the system as the hard constraint injected into the
// designer's prompt. It is deliberately short: it is paid for on every request
// where design is enabled, so it carries the values and the rule, and leaves
// the reasoning to DESIGN.md, which the agent can read when it needs to.
func (ds DesignSystem) ConstraintBlock(stylesheetPath, contractPath string) string {
	var b strings.Builder
	b.WriteString("# Design system (hard constraint)\n\n")
	fmt.Fprintf(&b, "This project has a committed design system: %s. ", ds.Name)
	fmt.Fprintf(&b, "Its contract is %s and its tokens compile to %s.\n\n", contractPath, stylesheetPath)
	b.WriteString("When you create or edit a design artifact:\n")
	fmt.Fprintf(&b, "- Link `%s` from the entry document and style with `var(--group-name)`.\n", stylesheetPath)
	b.WriteString("- Never invent a colour, font stack, spacing value or radius. Use a token.\n")
	b.WriteString("- If the design needs a value the system lacks, add the token with `design_system` first, then use it.\n")
	b.WriteString("- Prefer reusing an existing component pattern over inventing one.\n\n")
	b.WriteString("Available tokens:\n")
	for _, group := range sortedTokenGroups(ds.Tokens) {
		names := sortedTokenNames(ds.Tokens[group])
		refs := make([]string, 0, len(names))
		for _, n := range names {
			refs = append(refs, fmt.Sprintf("--%s-%s: %s", group, n, ds.Tokens[group][n]))
		}
		fmt.Fprintf(&b, "- %s — %s\n", group, strings.Join(refs, "; "))
	}
	return b.String()
}

// PromptConstraints returns the constraint block for the current project, or an
// empty string when the project has not committed a system. That still matters:
// a default system nobody chose is not a constraint worth stating.
func PromptConstraints() string {
	cfg := config.Get()
	if cfg == nil {
		return ""
	}
	layout := NewLayout(cfg.WorkingDir, cfg.Design.OutputDir, cfg.Design.SystemDir)
	ds, exists, err := LoadSystemAt(layout)
	if err != nil || !exists {
		return ""
	}
	rel := func(file string) string {
		return filepath.ToSlash(filepath.Join(layout.OutputDir, layout.SystemDir, file))
	}
	return ds.ConstraintBlock(rel(SystemStylesheet), rel(SystemContractFile))
}

func sortedTokenGroups(tokens map[string]map[string]string) []string {
	groups := make([]string, 0, len(tokens))
	for g := range tokens {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	return groups
}

func sortedTokenNames(values map[string]string) []string {
	names := make([]string, 0, len(values))
	for n := range values {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// SortedTokenGroups and SortedTokenNames are the exported forms of the ordering
// every surface needs: a token table shown in a different order each time is
// unreadable, and the sort belongs here rather than in each renderer.
func SortedTokenGroups(tokens map[string]map[string]string) []string {
	return sortedTokenGroups(tokens)
}

// SortedTokenNames returns the token names of a group in stable order.
func SortedTokenNames(values map[string]string) []string {
	return sortedTokenNames(values)
}
