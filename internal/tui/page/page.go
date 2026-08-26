package page

import tea "github.com/charmbracelet/bubbletea"

type PageID string

const (
	ChatPage         PageID = "chat"
	LogsPage         PageID = "logs"
	SettingsPage     PageID = "settings"
	OrchestratorPage PageID = "orchestrator"
	SnapshotsPage    PageID = "snapshots"
	EvaluatorPage    PageID = "evaluator"
	DesignPage       PageID = "design"
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

// Refreshable is implemented by pages whose content can go stale while another
// page is on screen. moveToPage calls Refresh on every navigation, so a page
// shows current data each time it is opened rather than only the first time it
// is constructed — Init runs once per process.
type Refreshable interface {
	Refresh() tea.Cmd
}
