package auth

// Build-time overridable OAuth client IDs.
// Override via -ldflags, for example:
//
//	go build -ldflags ' \
//	  -X github.com/digiogithub/pando/internal/auth.CopilotClientID=your_copilot_id \
//	  -X github.com/digiogithub/pando/internal/auth.ClaudeClientID=your_claude_id \
//	'
var (
	CopilotClientID = "Ov23li8tweQw6odWQebz"
	ClaudeClientID  = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
)
