package agentvcs

import (
	"context"
	"fmt"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

func fmtInt(n int) string { return fmt.Sprintf("%d", n) }

// snapshotsView is the table of named snapshots (components/snapshots).
func (v *View) snapshotsView(c *ui.Context, t *kit.Tokens) {
	ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		ui.Row(c).Padding(10, 16).Children(func() {
			ui.Text(c, fmt.Sprintf("Snapshots (%d total)", len(v.snapshots))).FontWeight(600)
		})
		if v.snapLoading && len(v.snapshots) == 0 {
			ui.Row(c).Grow(1).Justify(ui.Center).AlignItems(ui.Center).Children(func() { kit.Spinner(c).FontSize(26).TextColor(t.FgFaint) })
			return
		}
		if len(v.snapshots) == 0 {
			kit.EmptyState(c, kit.IconAgentVCS, "No snapshots yet", "Create a snapshot to save the state of the project and restore it later.")
			return
		}
		ui.List(c.Key("vcs:snaps"), &v.snapList, len(v.snapshots), func(i int) { v.snapshotRow(c, t, v.snapshots[i]) }).
			Grow(1).Padding(0, 12, 12, 12).Gap(2)
	})
}

func (v *View) snapshotRow(c *ui.Context, t *kit.Tokens, s client.Snapshot) {
	cc := c.Key("s:" + s.ID)
	row := ui.Row(cc).Gap(12).Padding(8, 12).Radius(t.RadiusSM).AlignItems(ui.Center)
	if row.Hovered() {
		row.Background(t.BgHover)
	}
	row.Children(func() {
		ui.Text(cc, s.Name).FontWeight(500).SingleLine().Ellipsis("…").Grow(1).Shrink(1).Tooltip(s.Name)
		ui.Text(cc, short(s.SessionID, 6)).Font("monospace").FontSize(t.TextXS).TextColor(t.FgMuted).Width(60)
		tone := kit.ToneNeutral
		switch s.Status {
		case "active":
			tone = kit.ToneSuccess
		case "pending":
			tone = kit.ToneWarning
		}
		kit.Badge(cc, s.Status, tone)
		ui.Text(cc, snapDate(s.CreatedAt)).FontSize(t.TextXS).TextColor(t.FgMuted).Width(70)
		ui.Text(cc, formatSize(s.Size)).FontSize(t.TextXS).TextColor(t.FgMuted).Width(70)
		if kit.Button(cc.Key("rv"), kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Icon: iconUndo, Tooltip: "Revert to this snapshot"}).Width(26).Height(26).Clicked() {
			v.snapshotAction(s.ID, "revert", "Snapshot reverted")
		}
		if kit.Button(cc.Key("dl"), kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Icon: iconTrash, Tooltip: "Delete snapshot"}).Width(26).Height(26).Clicked() {
			v.m = modal{open: true, danger: true, title: "Delete Snapshot", ok: "Delete",
				message: fmt.Sprintf("Are you sure you want to delete %q? This action cannot be undone.", s.Name),
				onOK: func(string) {
					v.app.Go(func(ctx context.Context) {
						err := v.app.Client.DeleteSnapshot(ctx, s.ID)
						v.app.Update(func() {
							if err != nil {
								v.app.Toasts.Error("Failed to delete snapshot", err.Error())
								return
							}
							v.app.Toasts.Success("Snapshot deleted", "")
							v.fetchSnapshots()
						})
					})
				}}
		}
	})
}

func (v *View) snapshotAction(id, action, done string) {
	v.app.Go(func(ctx context.Context) {
		err := v.app.Client.SnapshotAction(ctx, id, action)
		v.app.Update(func() {
			if err != nil {
				v.app.Toasts.Error("Snapshot "+action+" failed", err.Error())
				return
			}
			v.app.Toasts.Success(done, "")
			v.fetchSnapshots()
		})
	})
}

func (v *View) promptSnapshot() {
	v.m = modal{open: true, prompt: true, title: "Create Snapshot", message: "Snapshot Name", ok: "Create",
		onOK: func(name string) {
			v.app.Go(func(ctx context.Context) {
				err := v.app.Client.CreateSnapshot(ctx, name)
				v.app.Update(func() {
					if err != nil {
						v.app.Toasts.Error("Failed to create snapshot", err.Error())
						return
					}
					v.app.Toasts.Success(fmt.Sprintf("Snapshot %q created", name), "")
					v.fetchSnapshots()
				})
			})
		}}
}

func snapDate(s string) string {
	ts, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return s
	}
	return ts.Local().Format("Jan 2")
}
