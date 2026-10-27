package app

import (
	"strings"
	"sync"
)

// The chat draft is the text areas hand to the chat composer (the Web UI's
// chatDraftStore.insertIntoDraft): the Design Studio's "Ask about this
// element" and "Try it" put a prompt there and navigate to the chat, whose
// composer should call TakeChatDraft when it is shown and append the result
// to its own text.
var (
	draftMu   sync.Mutex
	chatDraft string
)

// InsertIntoChatDraft appends text to the pending chat draft.
func (a *Context) InsertIntoChatDraft(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	draftMu.Lock()
	defer draftMu.Unlock()
	if chatDraft != "" {
		chatDraft += "\n"
	}
	chatDraft += text
}

// TakeChatDraft returns and clears the pending chat draft.
func (a *Context) TakeChatDraft() string {
	draftMu.Lock()
	defer draftMu.Unlock()
	d := chatDraft
	chatDraft = ""
	return d
}

// PeekChatDraft returns the pending chat draft without clearing it.
func (a *Context) PeekChatDraft() string {
	draftMu.Lock()
	defer draftMu.Unlock()
	return chatDraft
}
