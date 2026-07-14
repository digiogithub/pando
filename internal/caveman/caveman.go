// Package caveman implements an output-brevity policy that can be injected into
// the agent's system prompt before each turn, at four levels (lite/full/ultra/
// wenyan) plus an off switch.
//
// It is a Pando-owned re-implementation of the idea behind the caveman skill by
// Julius Brussee (https://github.com/juliusbrussee/caveman, MIT licensed): the
// upstream project compresses the assistant's prose to cut output tokens while
// keeping code, commands and errors exact. Pando does not vendor its assets,
// hooks or telemetry; only the output policy is reproduced here, in a neutral,
// safety-preserving form.
//
// The policy constrains expression only. It never relaxes reasoning, tool use,
// testing or verification requirements, and it never reduces input or reasoning
// tokens — only the words the model writes back.
package caveman

import "strings"

// Mode is the caveman output-brevity level for a session.
type Mode string

const (
	// ModeOff disables caveman; nothing is injected.
	ModeOff Mode = ""
	// ModeLite trims filler but keeps normal sentences.
	ModeLite Mode = "lite"
	// ModeFull is the default: terse fragments, no unrequested prose.
	ModeFull Mode = "full"
	// ModeUltra is maximally terse: answer only, near-telegraphic.
	ModeUltra Mode = "ultra"
	// ModeWenyan renders natural-language prose in Classical Chinese.
	ModeWenyan Mode = "wenyan"
)

// IsActive reports whether the mode injects anything.
func (m Mode) IsActive() bool { return m != ModeOff }

// String returns a human-readable label for the mode.
func (m Mode) String() string {
	if m == ModeOff {
		return "off"
	}
	return string(m)
}

// ParseMode normalizes user input into a Mode. It accepts the level names plus a
// few natural-language off synonyms ("off", "stop", "normal", "none", "disable").
// The second return value is false when the input is not a recognized token.
func ParseMode(input string) (Mode, bool) {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "lite":
		return ModeLite, true
	case "full":
		return ModeFull, true
	case "ultra":
		return ModeUltra, true
	case "wenyan":
		return ModeWenyan, true
	case "off", "stop", "normal", "none", "disable", "disabled":
		return ModeOff, true
	default:
		return ModeOff, false
	}
}

// Description returns a one-line summary of a mode for confirmation messages.
func Description(m Mode) string {
	switch m {
	case ModeLite:
		return "Drop filler and restatement. Normal sentences, fewer of them."
	case ModeFull:
		return "Terse by default. Conclusions and fragments, no unrequested explanation."
	case ModeUltra:
		return "Maximum brevity. The answer and nothing around it."
	case ModeWenyan:
		return "Prose in Classical Chinese (文言文). Code, commands, paths and errors stay verbatim."
	default:
		return "Disabled."
	}
}

// SettingDescription is the help text for the global default in the settings
// surfaces (TUI and Web UI). It must state the output-only caveat: the mode
// cannot reduce input or reasoning tokens, and the savings depend on the task.
const SettingDescription = "Reduces output-token usage by giving shorter explanations and removing filler. " +
	"It keeps code, commands, errors, reasoning quality, tool use, and verification intact. " +
	"Output savings vary; input and reasoning tokens are not reduced."

// SettingOptions lists the selectable values for the global default, in the
// order the settings surfaces present them.
func SettingOptions() []string {
	return []string{ModeOff.String(), string(ModeLite), string(ModeFull), string(ModeUltra), string(ModeWenyan)}
}

// Usage is the one-liner shown when /caveman gets an unsupported level.
const Usage = "Usage: /caveman [lite|full|ultra|wenyan]\nNo argument defaults to full. Use /caveman-finish to disable."

// FinishUsage is shown when /caveman-finish is given an argument: the command
// takes no level, and silently ignoring one would hide the user's mistake.
const FinishUsage = "Usage: /caveman-finish\nIt takes no level. To change level, use /caveman [lite|full|ultra|wenyan]."

// DisabledMessage confirms /caveman-finish.
const DisabledMessage = "Caveman output brevity disabled. Back to normal output."

// ActivationMessage is the confirmation for /caveman. It states what changes
// (fewer words) and, just as importantly, what does not: reasoning, tool use,
// testing and verification are untouched, and the savings are output-only.
func ActivationMessage(m Mode) string {
	if !m.IsActive() {
		return DisabledMessage
	}
	return "Caveman output brevity: " + m.String() + ".\n" + Description(m) +
		"\nFewer words and less filler to cut output tokens. Reasoning, tool use, testing and verification are unchanged; code, commands and errors stay exact. Ask for detail any time and you get it in full."
}

// common is the always-injected core policy, independent of level.
const common = `# Output brevity mode

Write less. Say the same thing. This constrains how you WRITE, never how you
think, search, plan, use tools, test or verify. Do the full job; report it short.

ACTIVE EVERY RESPONSE, including follow-ups. Off only with ` + "`/caveman-finish`" + `.
Switch level: ` + "`/caveman lite|full|ultra|wenyan`" + `.

## Cut
- Greetings, sign-offs, self-narration ("I'll now...", "Let me...", "Great question").
- Restating the request back to the user before answering.
- Preamble before the answer and summaries that repeat what was just said.
- Generic transitions, hedging, apologies, praise, filler adjectives.
- Explanation nobody asked for. Options you are not going to take.

## Keep exact, never compress
Code, command lines, file paths, URLs, JSON/YAML/TOML, error text, stack traces,
API signatures, test output, approval questions, security caveats, and any detail
the user explicitly asked for. Never truncate or paraphrase these to save words.

## Keep doing, never skip
Root-cause evidence, the tests you ran and their result, permission and safety
warnings, and anything the project rules require. Being brief is never a reason
to skip work, skip verification, or claim something you did not check.

## Language
Answer in the user's language. Lead with the conclusion or the result. Prefer
fragments and bullets over paragraphs. One idea per line.

## Override
A direct request for detail ("explain", "walk me through", "write the report")
overrides brevity for that reply. Give it in full.`

// level holds the mode-specific snippet appended to the common policy.
var level = map[Mode]string{
	ModeLite: `## Level: lite
Normal sentences, filler removed. Answer first, then at most a short paragraph of
context. No preamble, no closing summary.`,
	ModeFull: `## Level: full
Fragments over sentences. Answer, then at most three short lines of what matters
(what changed, what broke, what to run). No prose paragraphs unless asked.

Example: "Fixed. Nil deref in parseConfig, line 42, guard added. go test ./internal/config passes."`,
	ModeUltra: `## Level: ultra
The answer and nothing else. Telegraphic. Drop articles and connectives where the
meaning survives. No context unless the user cannot act without it.

Example: "Fixed: nil deref parseConfig:42. Tests pass."`,
	ModeWenyan: `## Level: wenyan
Render natural-language prose in Classical Chinese (文言文), terse and formal.
This applies ONLY to prose. Code, commands, file paths, URLs, configuration,
error text and test output stay verbatim in their original form. If the user asks
for another language explicitly, that request wins.`,
}

// Instructions returns the full caveman policy to inject for the given mode, or
// the empty string when the mode is off. The result is the always-on core policy
// plus the snippet for the active level.
func Instructions(m Mode) string {
	if !m.IsActive() {
		return ""
	}
	snippet, ok := level[m]
	if !ok {
		return common
	}
	return common + "\n\n" + snippet
}
