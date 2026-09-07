package extension

import "context"

// Running a prompt from an extension.
//
// An extension that automates Pando (a scheduler, a webhook receiver, a batch
// runner) needs the one thing every surface of the host already does: send a
// prompt to the agent and wait for the answer. Without it such an extension can
// only prepare work and hand it to a human.
//
// The contract is deliberately the non-interactive one, the same shape as
// `pando -p`: one prompt in, one answer out, with progress along the way. It
// does not expose sessions, messages, models, providers or the tool loop. An
// extension cannot choose a model here, because model selection is
// configuration, and an extension that wants a different model imposes it
// through a configuration overlay. Everything in the request and the result is
// a string or a number, so core stays free to refactor its agent.

// PromptRunner runs one prompt through the host's agent and returns its
// answer. It is offered on HostServices.Prompts and is nil in a host with no
// agent (a CLI subcommand that never built one), so check before calling.
//
// Calls are not serialised by the host: an extension that must not run two
// prompts at once serialises them itself.
type PromptRunner interface {
	// RunPrompt sends req and blocks until the turn ends, the context is
	// cancelled, or the agent fails. A cancelled context ends the turn and
	// returns what was produced so far together with ctx.Err().
	RunPrompt(ctx context.Context, req PromptRequest) (PromptResult, error)
}

// PromptRequest is one turn to run.
type PromptRequest struct {
	// Prompt is the user message. Required.
	Prompt string

	// Title names the session created for the run, for the session list and
	// the logs. Defaults to a truncation of the prompt.
	Title string

	// SessionID continues an existing session instead of creating one. Empty
	// starts a fresh session, which is what a one-shot job wants.
	SessionID string

	// AutoApprove answers every permission request for this run with yes.
	//
	// It exists because nobody is watching: a run started by a schedule or a
	// pipeline has no human to answer a prompt, and a turn that blocks forever
	// on an unanswerable question is worse than one that was told in advance
	// what it may do. It applies to this run's session only, never globally,
	// and an extension that leaves it false gets a turn that stops at the
	// first permission request.
	AutoApprove bool

	// OnProgress, when set, is called as the turn produces output. It is
	// called from the host's event goroutine and must return promptly: slow
	// work belongs on a queue the extension owns. A panic in it is contained
	// and the run continues.
	OnProgress func(PromptProgress)
}

// PromptProgress is one update from a running turn. Exactly one of Delta and
// ToolName is set on any given update; the usage numbers are running totals
// and are zero until the host has confirmed any.
type PromptProgress struct {
	// SessionID is the session the turn runs in.
	SessionID string
	// Delta is newly produced assistant text.
	Delta string
	// ToolName names a tool the turn has just started running.
	ToolName string
	// PromptTokens and CompletionTokens are the turn's running token totals.
	PromptTokens     int64
	CompletionTokens int64
	// CostUSD is the run's cost so far, in US dollars, as the host computed it
	// from the model's published prices. Zero when the host cannot price the
	// model.
	CostUSD float64
}

// PromptResult is the outcome of a turn.
type PromptResult struct {
	// SessionID is the session the turn ran in, whether it was created for the
	// run or continued.
	SessionID string
	// Text is the assistant's final answer.
	Text string
	// FinishReason is the host's reason for ending the turn ("end_turn",
	// "max_tokens", "canceled", "permission_denied", ...). It is a string
	// rather than an enumeration because the set grows with the providers.
	FinishReason string
	// PromptTokens and CompletionTokens are the turn's token totals.
	PromptTokens     int64
	CompletionTokens int64
	// CostUSD is what the turn cost, in US dollars, or zero when the host
	// cannot price the model.
	CostUSD float64
}
