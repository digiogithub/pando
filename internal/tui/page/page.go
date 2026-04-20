package page

type PageID string

const (
	ChatPage         PageID = "chat"
	LogsPage         PageID = "logs"
	SettingsPage     PageID = "settings"
	OrchestratorPage PageID = "orchestrator"
	SnapshotsPage    PageID = "snapshots"
	EvaluatorPage    PageID = "evaluator"
)

// PageChangeMsg is used to change the current page
type PageChangeMsg struct {
	ID PageID
}

// OrchestratorFilterMsg asks the orchestrator page to apply a tag filter and navigate to it.
type OrchestratorFilterMsg struct {
	Tag string
}

// TagFilterable is implemented by pages that support tag-based task filtering.
type TagFilterable interface {
	SetFilterTag(tag string)
}

// ModalPage is implemented by pages that host modal dialogs, allowing the
// app-level key handler to check whether a modal is active before intercepting
// navigation keys like Esc.
type ModalPage interface {
	HasActiveModal() bool
	ClearModals()
}
