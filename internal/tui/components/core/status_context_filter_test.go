package core

import (
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/tui/util"
)

// The relevance filter summary reaches the status bar as an info message, the
// same path routing notices use.
func TestStatusShowsContextFilterNotice(t *testing.T) {
	notice := "Context filter: kept 4/9 (38 ms) - code 1/3, kb 2/5, events 1/1"
	model, _ := NewStatusCmp(nil).Update(util.InfoMsg{Type: util.InfoTypeInfo, Msg: notice})
	s, ok := model.(statusCmp)
	if !ok {
		t.Fatalf("unexpected model type %T", model)
	}
	s.width = 200
	if got := s.View(); !strings.Contains(got, "Context filter: kept 4/9") {
		t.Errorf("status bar = %q", got)
	}
}
