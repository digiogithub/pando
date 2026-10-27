package design

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// bridgeSource is the message envelope shared with
// internal/design/preview/bridge.js.
const bridgeSource = "pando-design"

// preloadJS runs before the page's own scripts. In the Web UI the preview is
// an iframe and the bridge talks to its parent with postMessage. Here the
// preview is the top-level page of an embedded web view, so window.parent is
// the page itself: the preload records the bridge's reports in a queue the
// native side drains with Page.Eval (a page of another origin cannot call
// Go, but Eval works on any page). Commands go the other way as
// window.postMessage, which the bridge already listens to.
const preloadJS = `(function(){
  if (window.__pandoBridgeQ) return;
  window.__pandoBridgeQ = [];
  window.addEventListener('message', function (e) {
    var d = e.data;
    if (d && d.source === '` + bridgeSource + `' && (d.type === 'ready' || d.type === 'selected' || d.type === 'slide')) {
      window.__pandoBridgeQ.push(d);
    }
  });
})();`

const drainJS = `return window.__pandoBridgeQ ? window.__pandoBridgeQ.splice(0) : [];`

// bridgeMsg is a report from the preview bridge.
type bridgeMsg struct {
	Type      string `json:"type"`
	NodeID    string `json:"nodeId"`
	Selection string `json:"selection"`
	Tag       string `json:"tag"`
	Text      string `json:"text"`
	Slide     int    `json:"slide"`
	Slides    int    `json:"slides"`
}

// canvas is the preview canvas: the preview server's own document in an
// embedded web view plus the selection bridge (web PreviewFrame.tsx). It is
// read-only by design: the user selects and asks the agent.
type canvas struct {
	s    *store
	pane *kit.WebPane

	url   string
	nonce int

	// Tracks what the page was last told, so a selection made in the
	// inspector reaches the page once and the page's own report is not
	// echoed back.
	sentSel   string
	sentSlide int
	ready     bool

	polling   bool
	lastBuild atomic.Int64
	stop      chan struct{}
	stopOnce  sync.Once
}

func newCanvas(s *store) *canvas {
	cv := &canvas{s: s, stop: make(chan struct{})}
	cv.pane = &kit.WebPane{Host: s.a.WebViewHost, Preload: preloadJS, OnChange: s.a.Invalidate}
	return cv
}

func (cv *canvas) close() { cv.stopOnce.Do(func() { close(cv.stop) }) }

// Build draws the canvas for an artifact. emptyMsg explains an empty canvas.
func (cv *canvas) Build(c *ui.Context, art client.DesignArtifact, emptyMsg string) {
	t := kit.Current()
	s := cv.s
	url := rebase(art.BridgeURL, s.a.Client.BaseURL())
	if url == "" {
		ui.Column(c).Grow(1).AlignSelf(ui.Stretch).AlignItems(ui.Center).Justify(ui.Center).Padding(24).
			Background(t.BgCard).Children(func() {
			ui.Text(c, emptyMsg).FontSize(t.TextSM).TextColor(t.FgMuted).TextAlign(ui.Center).MaxWidth(460)
		})
		return
	}
	cv.lastBuild.Store(time.Now().UnixNano())
	reload := cv.url != url || cv.nonce != s.reloadNonce
	if reload {
		// A reload drops the bridge, so the ready flag is re-armed with it.
		cv.ready, cv.sentSel, cv.sentSlide = false, "", 0
		force := cv.url == url && cv.url != ""
		cv.url, cv.nonce = url, s.reloadNonce
		cv.pane.Load(url, force)
	}
	cv.startPolling()
	cv.syncOut()
	cv.pane.Build(c, art.URL)
}

// syncOut pushes a selection or slide made outside the canvas into the page.
func (cv *canvas) syncOut() {
	s := cv.s
	if !cv.ready || !cv.pane.Ready() {
		return
	}
	if s.sel != nil && s.sel.NodeID != "" && s.sel.NodeID != cv.sentSel {
		cv.sentSel = s.sel.NodeID
		cv.post(map[string]any{"type": "select", "nodeId": s.sel.NodeID})
	} else if s.sel == nil && cv.sentSel != "" {
		cv.sentSel = ""
		cv.post(map[string]any{"type": "clearSelection"})
	}
	if s.slide > 0 && s.slide != cv.sentSlide {
		cv.sentSlide = s.slide
		cv.post(map[string]any{"type": "goToSlide", "slide": s.slide})
	}
}

func (cv *canvas) post(msg map[string]any) {
	msg["source"] = bridgeSource
	b, err := json.Marshal(msg)
	if err != nil {
		return
	}
	js := fmt.Sprintf("window.postMessage(%s, '*'); return true;", b)
	cv.s.a.Go(func(context.Context) { _, _ = cv.pane.Eval(js) })
}

// startPolling drains the bridge queue while the canvas is on screen.
func (cv *canvas) startPolling() {
	if cv.polling || !cv.pane.Ready() {
		return
	}
	cv.polling = true
	cv.s.a.Go(func(ctx context.Context) {
		tick := time.NewTicker(150 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-cv.stop:
				return
			case <-tick.C:
			}
			// Idle while another area is showing.
			if time.Since(time.Unix(0, cv.lastBuild.Load())) > 2*time.Second {
				continue
			}
			v, err := cv.pane.Eval(drainJS)
			if err != nil || v == nil {
				continue
			}
			raw, _ := json.Marshal(v)
			var msgs []bridgeMsg
			if json.Unmarshal(raw, &msgs) != nil || len(msgs) == 0 {
				continue
			}
			cv.s.a.Update(func() { cv.apply(msgs) })
		}
	})
}

// apply handles the reports of the bridge (UI thread).
func (cv *canvas) apply(msgs []bridgeMsg) {
	s := cv.s
	for _, m := range msgs {
		switch m.Type {
		case "ready":
			cv.ready = true
			// The page just (re)loaded: give it the selection and slide
			// made meanwhile.
			cv.sentSel, cv.sentSlide = "", 0
		case "selected":
			if m.NodeID == "" {
				s.sel = nil
				cv.sentSel = ""
				continue
			}
			sel := m.Selection
			if sel == "" {
				sel = "design://" + m.NodeID
			}
			s.sel = &selection{NodeID: m.NodeID, Selection: sel, Tag: m.Tag, Text: m.Text, Slide: m.Slide}
			cv.sentSel = m.NodeID
		case "slide":
			if m.Slide > 0 {
				s.slide = m.Slide
				cv.sentSlide = m.Slide
			}
		}
	}
}
