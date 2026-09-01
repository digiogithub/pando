package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/design"
	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/skills/catalog"
)

// DesignSkillsToolName is the gallery tool: the design templates and craft
// references a brief can be built from.
const DesignSkillsToolName = "design_skills"

// DesignSkillsParams drives the gallery tool.
type DesignSkillsParams struct {
	Action string `json:"action,omitempty"`
	// Name is the template ("show", "install").
	Name string `json:"name,omitempty"`
	// Craft names a craft reference to read instead of a template ("show").
	Craft string `json:"craft,omitempty"`
	// Scope is "project" (default) or "global" ("install").
	Scope string `json:"scope,omitempty"`
	// Force replaces an already installed copy ("install").
	Force bool `json:"force,omitempty"`
}

type designSkillsTool struct{ designTool }

// NewDesignSkillsTool returns the design template gallery tool.
func NewDesignSkillsTool(permissions permission.Service) BaseTool {
	return &designSkillsTool{designTool{permissions: permissions}}
}

func (t *designSkillsTool) Info() ToolInfo {
	return ToolInfo{
		Name: DesignSkillsToolName,
		Description: `List and read the design templates and craft references bundled with Pando.

A template is a design skill: it says what to build, in what order, and what to avoid. Pass its name to design_create(skill: "...") and the artifact is scaffolded from it.

ACTIONS:
- "list" (default): every template, with the kind it builds, whether it needs a design system, and the starter brief it suggests.
- "show": the full instructions of one template ("name"), or of one craft reference ("craft"). The craft references are: process (how to run a design job, from brief to presentation), content (copy, and never inventing facts), typography, color, layout, interaction (states, prototypes, motion), print (paper, PDF and fixed-size canvases), anti-ai-slop (the tells of a generated design). Read them before building, not after; start with process.
- "install": copy a template plus its craft references into a skills directory so it loads as an ordinary skill in later sessions. Not needed to build from it now. It refuses to overwrite an installed copy unless "force" is set, because that copy is the user's to edit.

Templates whose mode is not a surface (a workflow, a craft reference) are listed but cannot scaffold an artifact.`,
		Parameters: map[string]any{
			"action": map[string]any{"type": "string", "enum": []string{"list", "show", "install"}, "description": "What to do (default \"list\")"},
			"name":   map[string]any{"type": "string", "description": "Template name (\"show\", \"install\")"},
			"craft":  map[string]any{"type": "string", "description": "Craft reference to read instead of a template (\"show\"): process, content, typography, color, layout, interaction, print, anti-ai-slop"},
			"scope":  map[string]any{"type": "string", "enum": []string{"project", "global"}, "description": "Where to install (\"install\", default \"project\")"},
			"force":  map[string]any{"type": "boolean", "description": "Replace an already installed copy, discarding edits to it (\"install\")"},
		},
		Required: []string{},
	}
}

func (t *designSkillsTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesignSkillsParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	action := strings.ToLower(strings.TrimSpace(params.Action))
	if action == "" {
		action = "list"
	}

	// The bundles are embedded in the binary, so listing and reading them needs
	// no project, no database and no design subsystem. Only installing writes.
	switch action {
	case "list":
		return NewTextResponse(describeDesignGallery()), nil

	case "show":
		if craft := strings.TrimSpace(params.Craft); craft != "" {
			body, ok := design.CraftReference(craft)
			if !ok {
				return NewTextErrorResponse(fmt.Sprintf("unknown craft reference %q (have %s)",
					craft, strings.Join(design.CraftReferenceNames(), ", "))), nil
			}
			return NewTextResponse(body), nil
		}
		name := strings.TrimSpace(params.Name)
		if name == "" {
			return NewTextErrorResponse("show needs a template \"name\" or a \"craft\" reference"), nil
		}
		body, ok := design.BundledTemplateContent(name)
		if !ok {
			return NewTextErrorResponse(fmt.Sprintf("unknown design template %q (have %s)",
				name, strings.Join(bundledTemplateNames(), ", "))), nil
		}
		return NewTextResponse(body), nil

	case "install":
		name := strings.TrimSpace(params.Name)
		if name == "" {
			return NewTextErrorResponse("install needs a template \"name\""), nil
		}
		targetDir := designSkillsInstallDir(params.Scope)
		if err := t.requireWrite(ctx, DesignSkillsToolName, "install", targetDir,
			fmt.Sprintf("Install the design template %q into %s", name, targetDir),
			map[string]any{"name": name, "dir": targetDir}); err != nil {
			return ToolResponse{}, err
		}
		written, err := design.InstallBundle(name, targetDir, params.Force)
		if err != nil {
			return designError(err), nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Installed %s into %s\n", name, targetDir)
		for _, p := range written {
			fmt.Fprintf(&b, "  %s\n", p)
		}
		b.WriteString("\nIt loads as an ordinary skill from the next session on.")
		return NewTextResponse(b.String()), nil

	default:
		return NewTextErrorResponse("unknown action " + action), nil
	}
}

// designSkillsInstallDir resolves the skills root an install writes to. Project
// scope is the default: a design bundle belongs to the project it designs.
func designSkillsInstallDir(scope string) string {
	projectLocal := !strings.EqualFold(strings.TrimSpace(scope), "global")
	dir := catalog.ResolveSkillsDir(projectLocal)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(config.WorkingDirectory(), dir)
	}
	return filepath.Join(dir, "design")
}

func bundledTemplateNames() []string {
	templates, err := design.BundledTemplates()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(templates))
	for _, t := range templates {
		names = append(names, t.Name)
	}
	sort.Strings(names)
	return names
}

// describeDesignGallery renders the bundled gallery as the model reads it:
// what each template builds, and the brief it is expecting.
func describeDesignGallery() string {
	templates, err := design.BundledTemplates()
	if err != nil || len(templates) == 0 {
		return "No design templates are bundled with this build."
	}

	var b strings.Builder
	b.WriteString("Design templates (pass the name to design_create(skill: …)):\n\n")
	for _, tpl := range templates {
		if !tpl.Startable {
			continue
		}
		fmt.Fprintf(&b, "- %s [%s] — %s\n", tpl.Name, tpl.Kind, tpl.Description)
		if tpl.Scenario != "" {
			fmt.Fprintf(&b, "    when: %s\n", tpl.Scenario)
		}
		if tpl.RequiresSystem {
			b.WriteString("    needs a committed design system\n")
		}
		if tpl.ExamplePrompt != "" {
			fmt.Fprintf(&b, "    example brief: %s\n", tpl.ExamplePrompt)
		}
	}

	workflows := make([]design.Template, 0, len(templates))
	for _, tpl := range templates {
		if !tpl.Startable {
			workflows = append(workflows, tpl)
		}
	}
	if len(workflows) > 0 {
		b.WriteString("\nWorkflows (guidance, not artifact scaffolds):\n")
		for _, tpl := range workflows {
			fmt.Fprintf(&b, "- %s — %s\n", tpl.Name, tpl.Description)
		}
	}

	if craft := design.CraftReferenceNames(); len(craft) > 0 {
		fmt.Fprintf(&b, "\nCraft references (design_skills action \"show\", craft: …): %s\n", strings.Join(craft, ", "))
	}
	return b.String()
}
