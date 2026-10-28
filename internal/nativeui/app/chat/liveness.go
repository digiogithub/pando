package chat

import (
	"context"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// stallAfter is how long a streamed run may stay silent, heartbeats
// included, before its connection is taken for down: the server sends a
// heartbeat every 5 s while the run is quiet.
const stallAfter = 20 * time.Second

// quietAfter is how long the agent may produce nothing, its heartbeats
// still arriving, before the composer says so.
const quietAfter = 30 * time.Second

// clock is time.Now, replaced by tests.
var clock = time.Now

// liveness is what the composer shows of a run's stream.
type liveness uint8

const (
	liveNone    liveness = iota // no run streamed by this client
	liveActive                  // events or heartbeats keep arriving
	liveStalled                 // nothing for stallAfter, the run not over
)

func (cv *Conversation) liveness() liveness {
	if !cv.Running || !cv.Streaming {
		return liveNone
	}
	if clock().Sub(cv.heard) >= stallAfter {
		return liveStalled
	}
	return liveActive
}

// heardFrom records an event of the run's stream.
func (cv *Conversation) heardFrom(ev client.StreamEvent) {
	cv.heard = clock()
	if ev.Type != client.EventHeartbeat {
		cv.worked = cv.heard
	}
	cv.reconnected = false
}

// silentFor is how long the stream (since) and the agent (quiet) have
// delivered nothing.
func (cv *Conversation) silentFor() (since, quiet time.Duration) {
	now := clock()
	return now.Sub(cv.heard), now.Sub(cv.worked)
}

// watch rebuilds the view every second while the stream of ctx is open, so
// a silence shows (and counts) without any event to trigger a frame.
func (ch *Chat) watch(ctx context.Context) {
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				ch.app.Invalidate()
			}
		}
	}()
}

// reconnect drops a stream gone silent and follows the run again; the
// server replays it from its start, so the partial turn goes first.
func (ch *Chat) reconnect(cv *Conversation) {
	if cv.ID == "" {
		return // the session never came: nothing to reattach to
	}
	cv.Apply(client.StreamEvent{Type: client.EventReattach, SessionID: cv.ID})
	ch.attach(cv)
	cv.reconnected = true
}

// trail runs a comet of the accent color around the composer's border while
// the run's stream is alive.
func trail(box ui.Element, t *kit.Tokens) {
	turn := box.Loop("trail", 2400*time.Millisecond, ui.Linear) * 360
	base, head := t.BorderStrong, t.Accent
	box.Border(1.5, base).BorderConicGradient(ui.ConicGradient{Angle: turn, Stops: []ui.GradientStop{
		{Color: base, At: 0},
		{Color: base, At: 0.6},
		{Color: head.Alpha(0.35), At: 0.85},
		{Color: head, At: 0.985},
		{Color: base, At: 1},
	}})
}

// stallNotice tells that a run's stream went silent and offers to follow
// it again. The first stall reconnects on its own.
func (ch *Chat) stallNotice(c *ui.Context, t *kit.Tokens, cv *Conversation) {
	if !cv.reconnected && cv.ID != "" {
		ch.reconnect(cv)
		return
	}
	since, _ := cv.silentFor()
	ui.Row(c.Key("stall")).Gap(8).Padding(6, 10).Radius(t.RadiusMD).AlignItems(ui.Center).
		Background(t.Warning.Alpha(0.1)).Border(1, t.Warning.Alpha(0.4)).Children(func() {
		kit.Icon(c, kit.IconWarning).FontSize(14).TextColor(t.Warning)
		ui.Text(c, ch.app.T("chat.composer.stalled", "seconds", int(since.Seconds()))).FontSize(t.TextXS).
			TextColor(t.Fg).Grow(1).Shrink(1)
		if cv.ID != "" && kit.Button(c.Key("reconnect"), kit.ButtonOpts{Variant: kit.Secondary, Size: kit.SizeSM,
			Label: ch.app.T("chat.composer.reconnect")}).Clicked() {
			ch.reconnect(cv)
		}
	})
}
