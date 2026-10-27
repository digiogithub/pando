package chat

import (
	"fmt"
	"os"
	"strconv"
)

// spikeMessages reads PANDO_NATIVE_SPIKE_MESSAGES: when set, the chat opens
// a synthetic conversation of that many rows, to measure transcript
// rendering (P0 spike) without a model.
func spikeMessages() int {
	n, _ := strconv.Atoi(os.Getenv("PANDO_NATIVE_SPIKE_MESSAGES"))
	return max(0, min(n, 100000))
}

// SpikeConversation builds n alternating rows of realistic markdown.
func SpikeConversation(n int) *Conversation {
	cv := newConversation("spike")
	cv.Loaded = true
	for i := range n {
		if i%2 == 0 {
			cv.add(&Item{Kind: ItemUser, Text: fmt.Sprintf("Question %d: how do I cache results of an expensive call in Go?", i/2+1), Done: true})
			continue
		}
		it := cv.add(&Item{Kind: ItemAssistant, Done: true})
		if i%6 == 1 {
			cv.newPart(it, PTool).Tool = &Tool{ID: fmt.Sprintf("t%d", i), Name: "read", Title: "Read cache.go", Status: "completed", Output: "package cache"}
		}
		cv.newPart(it, PText).Text = fmt.Sprintf("Answer %d. Here is a minimal **LRU cache**, safe for concurrent use:\n\n"+
			"```go\ntype LRU struct {\n\tmu    sync.Mutex\n\tcap   int\n\titems map[string]*list.Element\n}\n```\n\n"+
			"1. `container/list` keeps the order of use.\n2. The mutex makes `Get` a writer too.\n\n| Strategy | Latency |\n|---|--:|\n| none | 41 ms |\n| cache | 0.2 ms |\n", i/2+1)
	}
	cv.runStart = len(cv.Items)
	return cv
}

func (ch *Chat) loadSpike(n int) {
	cv := SpikeConversation(n)
	ch.convs[cv.ID] = cv
	ch.current = cv.ID
}
