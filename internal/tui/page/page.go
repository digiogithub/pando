package page

type PageID string

const (
	ChatPage         PageID = "chat"
	LogsPage         PageID = "logs"
	SettingsPage     PageID = "settings"
	OrchestratorPage PageID = "orchestrator"
)

// PageChangeMsg is used to change the current page
type PageChangeMsg struct {
	ID PageID
}
