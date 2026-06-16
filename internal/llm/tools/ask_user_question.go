package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/userinput"
)

const AskUserQuestionToolName = "AskUserQuestion"

const askUserQuestionDescription = `Ask the user one or more questions with selectable options and wait for the answer.

Use this tool when you need the user to make a decision mid-task that you cannot resolve
on your own — for example choosing between approaches, confirming requirements, or picking
which feature to build. Do NOT use it for choices with an obvious default or facts you can
verify yourself.

Each question presents a short menu of options. The user can select one option (or several
when multiSelect is true), or type free text via an automatic "Other" choice. In TUI and
Web UI the tool blocks with an overlay until the user responds. In ACP clients it formats the
questions as text and ends the turn; the user answers in writing.

Guidelines:
- Provide 1-4 questions, each with 2-4 options.
- Keep "header" to a short label (<= 12 chars), e.g. "Approach", "Library".
- Give each option a clear "label" and a concise "description" of what it means.
- Set "multiSelect": true only when several options can be chosen together.`

type askUserQuestionTool struct {
	userInput userinput.Service
}

// AskUserQuestionParams mirrors the Claude Code AskUserQuestion schema.
type AskUserQuestionParams struct {
	Questions []AskUserQuestionParamQuestion `json:"questions"`
}

type AskUserQuestionParamQuestion struct {
	Question    string                       `json:"question"`
	Header      string                       `json:"header"`
	MultiSelect bool                         `json:"multiSelect"`
	Options     []AskUserQuestionParamOption `json:"options"`
}

type AskUserQuestionParamOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// AskUserQuestionResponseMetadata is attached to the tool response as structured metadata.
type AskUserQuestionResponseMetadata struct {
	Answers []userinput.Answer `json:"answers"`
}

func NewAskUserQuestionTool(userInput userinput.Service) BaseTool {
	return &askUserQuestionTool{userInput: userInput}
}

func (t *askUserQuestionTool) Info() ToolInfo {
	return ToolInfo{
		Name:        AskUserQuestionToolName,
		Description: askUserQuestionDescription,
		Parameters: map[string]any{
			"questions": map[string]any{
				"type":        "array",
				"description": "1-4 questions to ask the user.",
				"minItems":    1,
				"maxItems":    4,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"question": map[string]any{
							"type":        "string",
							"description": "The complete question to ask the user.",
						},
						"header": map[string]any{
							"type":        "string",
							"description": "A very short label (<= 12 chars) shown as a chip, e.g. \"Approach\".",
						},
						"multiSelect": map[string]any{
							"type":        "boolean",
							"description": "Allow selecting multiple options. Defaults to false.",
						},
						"options": map[string]any{
							"type":        "array",
							"description": "2-4 selectable options.",
							"minItems":    2,
							"maxItems":    4,
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"label": map[string]any{
										"type":        "string",
										"description": "Short display text for the option.",
									},
									"description": map[string]any{
										"type":        "string",
										"description": "Explanation of what this option means.",
									},
								},
								"required": []string{"label", "description"},
							},
						},
					},
					"required": []string{"question", "header", "options"},
				},
			},
		},
		Required: []string{"questions"},
	}
}

func (t *askUserQuestionTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params AskUserQuestionParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}

	if len(params.Questions) == 0 {
		return NewTextErrorResponse("at least one question is required"), nil
	}
	if len(params.Questions) > 4 {
		return NewTextErrorResponse("at most 4 questions are allowed"), nil
	}
	for i, q := range params.Questions {
		if strings.TrimSpace(q.Question) == "" {
			return NewTextErrorResponse(fmt.Sprintf("question %d: 'question' is required", i+1)), nil
		}
		if strings.TrimSpace(q.Header) == "" {
			return NewTextErrorResponse(fmt.Sprintf("question %d: 'header' is required", i+1)), nil
		}
		if len(q.Options) < 2 || len(q.Options) > 4 {
			return NewTextErrorResponse(fmt.Sprintf("question %d: must have between 2 and 4 options", i+1)), nil
		}
	}

	sessionID, _ := GetContextValues(ctx)

	// Build the userinput.Question slice with stable per-question IDs.
	questions := make([]userinput.Question, len(params.Questions))
	for i, q := range params.Questions {
		opts := make([]userinput.Option, len(q.Options))
		for j, o := range q.Options {
			opts[j] = userinput.Option{Label: o.Label, Description: o.Description}
		}
		questions[i] = userinput.Question{
			ID:          fmt.Sprintf("q%d", i+1),
			Header:      q.Header,
			Question:    q.Question,
			MultiSelect: q.MultiSelect,
			Options:     opts,
		}
	}

	// ACP mode: no blocking selection UI available. Format the questions as text
	// and end the turn; the user replies in writing.
	if acpConn := ctx.Value(ACPClientConnContextKey); acpConn != nil {
		return NewTextResponse(formatQuestionsAsText(questions)), nil
	}

	// UI mode (TUI / Web): block until the user responds.
	resp := t.userInput.Ask(userinput.CreateAskRequest{
		SessionID: sessionID,
		Questions: questions,
	})
	if resp.Cancelled {
		return NewTextErrorResponse("user cancelled the question"), nil
	}

	content := formatAnswersAsText(questions, resp.Answers)
	return WithResponseMetadata(
		NewTextResponse(content),
		AskUserQuestionResponseMetadata{Answers: resp.Answers},
	), nil
}

// formatQuestionsAsText renders the questions as numbered markdown for ACP clients.
func formatQuestionsAsText(questions []userinput.Question) string {
	var b strings.Builder
	b.WriteString("I need your input. Please answer the following question(s) in your next message:\n\n")
	for i, q := range questions {
		fmt.Fprintf(&b, "**%d. %s** [%s]\n", i+1, q.Question, q.Header)
		if q.MultiSelect {
			b.WriteString("_(you may choose multiple)_\n")
		}
		for j, o := range q.Options {
			letter := string(rune('a' + j))
			fmt.Fprintf(&b, "  %s) %s — %s\n", letter, o.Label, o.Description)
		}
		b.WriteString("  or provide your own answer (Other).\n\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatAnswersAsText renders the user's selections back to the model.
func formatAnswersAsText(questions []userinput.Question, answers []userinput.Answer) string {
	byID := make(map[string]userinput.Question, len(questions))
	for _, q := range questions {
		byID[q.ID] = q
	}
	var b strings.Builder
	b.WriteString("The user answered:\n")
	for _, a := range answers {
		q := byID[a.QuestionID]
		selections := append([]string{}, a.Selected...)
		if strings.TrimSpace(a.OtherText) != "" {
			selections = append(selections, fmt.Sprintf("Other: %s", a.OtherText))
		}
		if len(selections) == 0 {
			selections = []string{"(no selection)"}
		}
		fmt.Fprintf(&b, "- %s: %s\n", q.Question, strings.Join(selections, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}
