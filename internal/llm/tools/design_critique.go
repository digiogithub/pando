package tools

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/digiogithub/pando/internal/design"
)

// DesignCritiqueToolName is the quality gate: what is wrong with this version,
// how badly, and whether it is finished.
const DesignCritiqueToolName = "design_critique"

// DesignCritiqueParams drives one critic pass.
type DesignCritiqueParams struct {
	ArtifactID string `json:"artifact_id"`
	Action     string `json:"action,omitempty"`
	Version    int    `json:"version,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	// Policy overrides the gate for this pass: none, standard or strict.
	Policy string `json:"policy,omitempty"`
	// Round is the 1-based iteration number; it defaults to the version.
	Round int `json:"round,omitempty"`
	// SkipRender audits without a browser: design-system checks only.
	SkipRender bool `json:"skip_render,omitempty"`
	// Record stores the pass in the artifact's critique history. Default true.
	Record *bool `json:"record,omitempty"`

	// Score, Summary and Issues carry the critic's own judgement.
	Score   float64        `json:"score,omitempty"`
	Summary string         `json:"summary,omitempty"`
	Issues  []design.Issue `json:"issues,omitempty"`
}

type designCritiqueTool struct{ designTool }

// NewDesignCritiqueTool returns the critique/quality-gate tool.
func NewDesignCritiqueTool() BaseTool { return &designCritiqueTool{} }

func (t *designCritiqueTool) Info() ToolInfo {
	return ToolInfo{
		Name: DesignCritiqueToolName,
		Description: `Score a design artifact against automated quality rules and decide whether it is finished.

WHAT IT DOES:
- Renders the artifact, then runs every deterministic rule over the result: accessibility (image alt text, control names, heading order, colour contrast, target size), runtime (console errors, failed requests), layout (horizontal overflow), deck print behaviour, and design-system adherence (stylesheet linked, values a token already covers).
- Scores it 0-10, folds in your own judgement when you pass one, records the pass, and answers the only question that matters: iterate, or stop.

HOW TO USE IT IN A LOOP:
1. Build or patch the artifact, then call this tool with just the artifact_id.
2. Read the findings. Each one names the node it is about and how to fix it. Fix them in the files with edit/write, not by arguing with the score.
3. Add what the rules cannot see — whether the layout reads well, whether the copy says anything, whether it looks generic — as "score" plus "issues". The rules count contrast failures; they cannot tell you the hero is boring.
4. Commit the fix with design_versions(op: "commit"), then critique again. Stop when the decision says so.

ACTIONS:
- "run" (default): a full pass.
- "show": read back the last recorded pass for a version without re-rendering.

THE GATE:
- "standard" stops at the configured score threshold. "strict" also refuses to pass while any error-level finding remains. "none" reports without ever blocking. The artifact's template may set this; "policy" overrides it for one call.
- The decision also stops the loop when the round budget runs out, so a design that will not converge does not iterate forever.

NOTES:
- The pass never edits the artifact: it is safe to run at any point.
- Without a browser it still runs the design-system checks and says so, rather than reporting a high score for a page nobody looked at.`,
		Parameters: map[string]any{
			"artifact_id": map[string]any{"type": "string", "description": "Artifact to critique"},
			"action":      map[string]any{"type": "string", "enum": []string{"run", "show"}, "description": "What to do (default \"run\")"},
			"version":     map[string]any{"type": "integer", "description": "Version to critique or read back (default: current)"},
			"width":       map[string]any{"type": "integer", "description": "Viewport width in CSS pixels"},
			"height":      map[string]any{"type": "integer", "description": "Viewport height in CSS pixels"},
			"policy":      map[string]any{"type": "string", "enum": []string{"none", "standard", "strict"}, "description": "Override the gate for this pass"},
			"round":       map[string]any{"type": "integer", "description": "1-based iteration number (default: the version number)"},
			"skip_render": map[string]any{"type": "boolean", "description": "Audit without a browser: design-system checks only"},
			"record":      map[string]any{"type": "boolean", "description": "Store the pass in the critique history (default true)"},
			"score":       map[string]any{"type": "number", "description": "Your own 0-10 judgement, blended with the automated score"},
			"summary":     map[string]any{"type": "string", "description": "Your own one-paragraph verdict"},
			"issues": map[string]any{
				"type":        "array",
				"description": "Your own findings, each with severity (info|warning|error|blocking), message, fix, and node_id when it is about one element",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"severity": map[string]any{"type": "string", "enum": []string{"info", "warning", "error", "blocking"}},
						"node_id":  map[string]any{"type": "string"},
						"slide":    map[string]any{"type": "integer"},
						"message":  map[string]any{"type": "string"},
						"fix":      map[string]any{"type": "string"},
					},
					"required": []string{"message"},
				},
			},
		},
		Required: []string{"artifact_id"},
	}
}

func (t *designCritiqueTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params DesignCritiqueParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("failed to parse parameters: " + err.Error()), nil
	}
	if params.ArtifactID == "" {
		return NewTextErrorResponse("artifact_id is required"), nil
	}
	svc, err := t.service(ctx)
	if err != nil {
		return designError(err), nil
	}

	switch strings.ToLower(strings.TrimSpace(params.Action)) {
	case "", "run":
		record := true
		if params.Record != nil {
			record = *params.Record
		}
		report, err := svc.Critique(ctx, params.ArtifactID, design.CritiqueOptions{
			Version:    params.Version,
			Render:     design.RenderOptions{Viewport: design.Viewport{W: params.Width, H: params.Height}},
			SkipRender: params.SkipRender,
			Round:      params.Round,
			Policy:     params.Policy,
			Score:      params.Score,
			Summary:    params.Summary,
			Issues:     params.Issues,
			Record:     record,
		})
		if err != nil {
			return designError(err), nil
		}
		return WithResponseMetadata(NewTextResponse(describeCritique(report)), report), nil

	case "show":
		critique, err := svc.LatestCritique(ctx, params.ArtifactID, params.Version)
		if err != nil {
			if errors.Is(err, design.ErrNotFound) {
				return NewTextErrorResponse("this version has not been critiqued yet; run design_critique on it first"), nil
			}
			return designError(err), nil
		}
		return WithResponseMetadata(NewTextResponse(describeStoredCritique(critique)), critique), nil

	default:
		return NewTextErrorResponse(fmt.Sprintf("unknown action %q (use run or show)", params.Action)), nil
	}
}

// describeCritique writes the pass the way it should be read: the verdict
// first, then the findings worst-first, then what to do next.
func describeCritique(report design.CritiqueReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s v%d scored %.1f/10 (automated %.1f)\n",
		report.Artifact.Title, report.Version, report.Critique.Score, report.Audit.Score)
	fmt.Fprintf(&b, "decision: %s — %s\n",
		decisionWord(report.Decision), report.Decision.Reason)
	fmt.Fprintf(&b, "policy %s, threshold %.1f\n", report.Decision.Policy, report.Decision.Threshold)
	if !report.Rendered {
		fmt.Fprintf(&b, "\nNOT RENDERED: %s\nAccessibility, runtime and layout rules did not run; this score covers the design system only.\n",
			report.RenderError)
	}

	if len(report.Critique.Issues) == 0 {
		b.WriteString("\nNo findings.\n")
	} else {
		fmt.Fprintf(&b, "\nfindings (%d):\n", len(report.Critique.Issues))
		for _, issue := range report.Critique.Issues {
			writeIssue(&b, issue)
		}
	}

	if folded := foldedRules(report.Audit); len(folded) > 0 {
		fmt.Fprintf(&b, "\nrepeated rules, only the first few shown: %s\n", strings.Join(folded, ", "))
	}

	b.WriteString("\n")
	if report.Decision.Iterate {
		fmt.Fprintf(&b, "Next: fix the findings above, commit with design_versions(op: \"commit\"), then critique again (round %d of %d).\n",
			report.Decision.Round+1, report.Decision.MaxRounds)
	} else if report.Decision.Pass {
		b.WriteString("Next: this version meets the bar. Export it, or present it with design_present.\n")
	} else {
		b.WriteString("Next: the round budget is spent. Report what is still outstanding rather than iterating further.\n")
	}
	return b.String()
}

func describeStoredCritique(critique design.Critique) string {
	var b strings.Builder
	fmt.Fprintf(&b, "v%d scored %.1f/10 on %s\n",
		critique.Version, critique.Score, critique.CreatedAt.Format("2006-01-02 15:04"))
	if critique.Summary != "" {
		fmt.Fprintf(&b, "%s\n", critique.Summary)
	}
	if len(critique.Issues) == 0 {
		b.WriteString("\nNo findings.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "\nfindings (%d):\n", len(critique.Issues))
	for _, issue := range critique.Issues {
		writeIssue(&b, issue)
	}
	return b.String()
}

func writeIssue(b *strings.Builder, issue design.Issue) {
	fmt.Fprintf(b, "  [%s]", issue.Severity)
	if issue.Code != "" {
		fmt.Fprintf(b, " %s", issue.Code)
	}
	if issue.NodeID != "" {
		fmt.Fprintf(b, " (%s)", issue.NodeID)
	}
	fmt.Fprintf(b, " %s\n", issue.Message)
	if issue.Fix != "" {
		fmt.Fprintf(b, "      fix: %s\n", issue.Fix)
	}
}

func decisionWord(decision design.GateDecision) string {
	switch {
	case decision.Pass:
		return "PASS"
	case decision.Iterate:
		return "ITERATE"
	default:
		return "STOP"
	}
}

// foldedRules names the rules whose findings were trimmed, so the reader knows
// the list is shorter than the problem.
func foldedRules(audit design.AuditResult) []string {
	var folded []string
	for code, count := range audit.Counts {
		shown := 0
		for _, issue := range audit.Issues {
			if issue.Code == code {
				shown++
			}
		}
		if count > shown {
			folded = append(folded, fmt.Sprintf("%s (%d total)", code, count))
		}
	}
	sort.Strings(folded)
	return folded
}
