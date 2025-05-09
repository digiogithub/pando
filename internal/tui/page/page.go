package page

type PageID string

// PageChangeMsg is used to change the current page
type PageChangeMsg struct {
	ID PageID
}

// ChatPageInterface is the interface for the chat page
type ChatPageInterface interface {
	GetSessionID() string
}
