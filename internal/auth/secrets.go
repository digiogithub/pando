package auth

// Build-time overridable OAuth client IDs.
// Override via -ldflags, for example:
//
//	go build -ldflags ' \
//	  -X github.com/digiogithub/pando/internal/auth.CopilotClientID=your_copilot_id \
//	  -X github.com/digiogithub/pando/internal/auth.ClaudeClientID=your_claude_id \
//	'
var (
	CopilotClientID = "your-copilot-client-id"
	ClaudeClientID  = "your-claude-client-id"
)
