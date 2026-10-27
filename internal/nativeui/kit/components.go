package kit

import (
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// Variant is the look of a button (web-ui/src/components/ui/Button).
type Variant int

// Button variants.
const (
	Secondary Variant = iota
	Primary
	Ghost
	Danger
	Outline
)

// Size is the height of a control.
type Size int

// Control sizes: --control-h (32) and --control-h-sm (28).
const (
	SizeMD Size = iota
	SizeSM
)

// ButtonOpts configures Button.
type ButtonOpts struct {
	Variant Variant
	Size    Size
	// Icon is an embedded icon name shown before the label.
	Icon  string
	Label string
	// Tooltip shows on hover; icon-only buttons should set it.
	Tooltip  string
	Disabled bool
	// Active draws a pressed/selected state (toggle buttons).
	Active bool
}

// Button is a Pando button on MyGo's ButtonBase. Read Clicked on the
// returned element. Give it a key (c.Key) when it is built in a loop.
func Button(c *ui.Context, o ButtonOpts) ui.Element {
	t := Current()
	b := ui.ButtonBase(c)
	h := t.ControlH
	padX := float32(12)
	if o.Size == SizeSM {
		h, padX = t.ControlHSm, 10
	}
	if o.Label == "" && o.Icon != "" {
		padX = 0
		b.Width(h)
	}
	b.Height(h).Padding(0, padX).Gap(6).AlignItems(ui.Center).Justify(ui.Center).
		Radius(t.RadiusSM).FontSize(t.TextSM).Cursor(ui.CursorPointer).Disabled(o.Disabled)
	if o.Label != "" {
		b.Label(o.Label)
	} else if o.Tooltip != "" {
		b.Label(o.Tooltip)
	}
	if o.Tooltip != "" {
		b.Tooltip(o.Tooltip)
	}
	hover, pressed := b.Hovered() && !o.Disabled, b.Pressed() && !o.Disabled
	switch o.Variant {
	case Primary:
		bg := t.Accent
		if hover || pressed {
			bg = t.AccentHover
		}
		b.Background(bg).TextColor(t.AccentFg)
	case Danger:
		bg := t.Danger
		if hover || pressed {
			bg = mix(t.Danger, t.Fg, 0.85)
		}
		b.Background(bg).TextColor(ui.Hex("#ffffff"))
	case Ghost:
		b.TextColor(t.FgMuted)
		if hover || pressed || o.Active {
			b.Background(t.BgHover).TextColor(t.Fg)
		}
	case Outline:
		b.Border(1, t.BorderStrong).TextColor(t.Fg)
		if hover || pressed || o.Active {
			b.Background(t.BgHover)
		}
	default: // Secondary
		bg := t.BgRaised
		if hover || pressed || o.Active {
			bg = mix(t.BgRaised, t.Fg, 0.9)
		}
		b.Background(bg).Border(1, t.Border).TextColor(t.Fg)
	}
	if o.Disabled {
		b.Opacity(0.5).Cursor(ui.CursorNotAllowed)
	}
	b.Children(func() {
		if o.Icon != "" {
			Icon(c, o.Icon).FontSize(16)
		}
		if o.Label != "" {
			ui.Text(c, o.Label).SingleLine().FontWeight(500)
		}
	})
	return b
}

// PrimaryButton is a Primary button with a label.
func PrimaryButton(c *ui.Context, label string) ui.Element {
	return Button(c, ButtonOpts{Variant: Primary, Label: label})
}

// SecondaryButton is a Secondary button with a label.
func SecondaryButton(c *ui.Context, label string) ui.Element {
	return Button(c, ButtonOpts{Variant: Secondary, Label: label})
}

// IconButton is a square ghost button showing an icon, named for assistive
// technology and the tooltip by label.
func IconButton(c *ui.Context, icon, label string) ui.Element {
	return Button(c, ButtonOpts{Variant: Ghost, Icon: icon, Tooltip: label, Size: SizeSM})
}

// Tooltip gives el a tooltip and an accessible description. It exists so
// views do not depend on MyGo's method name directly.
func Tooltip(el ui.Element, text string) ui.Element { return el.Tooltip(text) }

// Card is a raised panel (bg-card, border, radius-md) holding fn's content
// in a column.
func Card(c *ui.Context, fn func()) ui.Element {
	t := Current()
	return ui.Column(c).Gap(8).Padding(16).Radius(t.RadiusMD).Background(t.BgCard).
		Border(1, t.Border).Children(fn)
}

// Tone colours badges and notices.
type Tone int

// Tones.
const (
	ToneNeutral Tone = iota
	ToneAccent
	ToneSuccess
	ToneWarning
	ToneDanger
	ToneInfo
)

// ToneColors returns the foreground and soft background of a tone.
func ToneColors(tone Tone) (fg, bg ui.Color) {
	t := Current()
	switch tone {
	case ToneAccent:
		return t.Accent, t.AccentSoft
	case ToneSuccess:
		return t.Success, t.SuccessSoft
	case ToneWarning:
		return t.Warning, t.WarningSoft
	case ToneDanger:
		return t.Danger, t.DangerSoft
	case ToneInfo:
		return t.Info, t.InfoSoft
	}
	return t.FgMuted, t.BgRaised
}

// Badge is a small pill label in a tone.
func Badge(c *ui.Context, text string, tone Tone) ui.Element {
	t := Current()
	fg, bg := ToneColors(tone)
	return ui.Row(c).Padding(1, 8).Radius(999).Background(bg).AlignItems(ui.Center).Children(func() {
		ui.Text(c, text).FontSize(t.TextXS).FontWeight(500).TextColor(fg).SingleLine()
	})
}

// Input is a single-line text input with Pando's field look. Read
// Changed/Submitted on the returned element.
func Input(c *ui.Context, value *string, placeholder string) ui.Element {
	t := Current()
	return ui.TextInput(c, value).Placeholder(placeholder).Height(t.ControlH).
		Background(t.BgInput).Border(1, t.BorderStrong).Radius(t.RadiusSM).FontSize(t.TextSM)
}

// Switch is a labelled on/off switch; the label toggles it too.
func Switch(c *ui.Context, on *bool, label string) ui.Element {
	t := Current()
	row := ui.Row(c).Gap(8).AlignItems(ui.Center)
	row.Children(func() {
		ui.Switch(c, on).Label(label)
		if label != "" {
			if ui.Text(c, label).FontSize(t.TextSM).Cursor(ui.CursorPointer).Clicked() {
				*on = !*on
			}
		}
	})
	return row
}

// Select chooses one of options into selected.
func Select(c *ui.Context, selected *string, options []string) ui.Element {
	t := Current()
	return ui.Select(c, selected, options).Height(t.ControlH).FontSize(t.TextSM)
}

// DialogOpts configures Dialog.
type DialogOpts struct {
	Title       string
	Description string
	// Width of the panel in DIPs (default 440).
	Width float32
	// Body builds the content; Footer the buttons, laid out at the end.
	Body, Footer func()
}

// Dialog shows a modal panel over a scrim while *open is true; Escape and a
// click outside close it.
func Dialog(c *ui.Context, open *bool, o DialogOpts) ui.Element {
	t := Current()
	w := o.Width
	if w <= 0 {
		w = 440
	}
	return ui.DialogBase(c, open, func(backdrop, panel ui.Element) {
		backdrop.Background(t.Scrim)
		panel.Width(w).Padding(20).Gap(12).Radius(t.RadiusLG).Background(t.BgOverlay).
			Border(1, t.Border).Shadow(0, 24, 56, 0, ui.RGBA(0, 0, 0, 0.18))
		if o.Title != "" {
			ui.Text(c, o.Title).FontSize(t.TextLG).FontWeight(600)
		}
		if o.Description != "" {
			ui.Text(c, o.Description).FontSize(t.TextSM).TextColor(t.FgMuted)
		}
		if o.Body != nil {
			o.Body()
		}
		if o.Footer != nil {
			ui.Row(c).Gap(8).Justify(ui.End).Margin(8, 0, 0, 0).Children(o.Footer)
		}
	})
}

// Toast types understood by ToastViewport.
const (
	ToastInfo    = ""
	ToastSuccess = "success"
	ToastError   = "error"
	ToastWarning = "warning"
)

// Toaster queues toasts from any goroutine; the view flushes them into the
// window with Flush (c.AddToast needs the UI thread). Invalidate, when set,
// asks the window for a frame after Push.
type Toaster struct {
	mu         sync.Mutex
	pending    []ui.Toast
	Invalidate func()
}

// Push queues a toast. Safe from any goroutine.
func (q *Toaster) Push(t ui.Toast) {
	q.mu.Lock()
	q.pending = append(q.pending, t)
	inv := q.Invalidate
	q.mu.Unlock()
	if inv != nil {
		inv()
	}
}

// Info, Success and Error queue a toast with a title and description.
func (q *Toaster) Info(title, desc string) {
	q.Push(ui.Toast{Title: title, Description: desc, Type: ToastInfo})
}

// Success queues a success toast.
func (q *Toaster) Success(title, desc string) {
	q.Push(ui.Toast{Title: title, Description: desc, Type: ToastSuccess})
}

// Error queues an error toast that stays 8 seconds.
func (q *Toaster) Error(title, desc string) {
	q.Push(ui.Toast{Title: title, Description: desc, Type: ToastError, Timeout: 8 * time.Second})
}

// Flush adds the queued toasts to the window. Call it in the view.
func (q *Toaster) Flush(c *ui.Context) {
	q.mu.Lock()
	p := q.pending
	q.pending = nil
	q.mu.Unlock()
	for _, t := range p {
		c.AddToast(t)
	}
}

// ToastViewport shows the window's toasts bottom-right with Pando's look.
// Call it once in the root view.
func ToastViewport(c *ui.Context) {
	ui.ToastViewportBase(c, func(viewport ui.Element, toasts []ui.Toast) {
		t := Current()
		viewport.Padding(16).AlignItems(ui.End).Gap(8)
		for _, ts := range toasts {
			toast := ui.ToastBase(c.Key(ts.ID), ts)
			icon, color := IconInfo, t.Info
			switch ts.Type {
			case ToastSuccess:
				icon, color = IconSuccess, t.Success
			case ToastError:
				icon, color = IconError, t.Danger
			case ToastWarning:
				icon, color = IconWarning, t.Warning
			}
			toast.Root.Row().Gap(10).Padding(10, 12).Radius(t.RadiusMD).MaxWidth(420).
				Background(t.BgOverlay).Border(1, t.Border).AlignItems(ui.Start).
				Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.16)).
				Transition(ui.ElementTransition{Enter: &ui.Motion{Y: 8}, Exit: &ui.Motion{}})
			toast.Root.Children(func() {
				Icon(c, icon).TextColor(color).FontSize(16).Margin(2, 0, 0, 0)
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Text(c, ts.Title).FontSize(t.TextSM).FontWeight(600)
					if ts.Description != "" {
						ui.Text(c, ts.Description).FontSize(t.TextXS).TextColor(t.FgMuted).MaxLines(4)
					}
				})
				if ts.Action != "" {
					toast.ActionButton().Padding(2, 8).Radius(t.RadiusXS).Background(t.AccentSoft).
						TextColor(t.Accent).Children(func() { ui.Text(c, ts.Action).FontSize(t.TextXS) })
				}
				toast.CloseButton().Label("Close").Padding(2).Radius(t.RadiusXS).TextColor(t.FgFaint).
					Children(func() { Icon(c, IconClose).FontSize(14) })
			})
		}
	})
}

// SettingsSection groups settings rows under a title (settings.css
// .settings-section).
func SettingsSection(c *ui.Context, title, description string, fn func()) ui.Element {
	t := Current()
	return ui.Column(c).Gap(4).Children(func() {
		ui.Text(c, title).FontSize(t.TextMD).FontWeight(600)
		if description != "" {
			ui.Text(c, description).FontSize(t.TextSM).TextColor(t.FgMuted)
		}
		ui.Column(c).Margin(8, 0, 0, 0).Radius(t.RadiusMD).Background(t.BgCard).Border(1, t.Border).
			Dividers(1, t.Border).Children(fn)
	})
}

// SettingsRow is a row of a SettingsSection: a label and description on the
// left, the control on the right.
func SettingsRow(c *ui.Context, label, description string, control func()) ui.Element {
	t := Current()
	return ui.Row(c).Gap(16).Padding(12, 16).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Text(c, label).FontSize(t.TextSM).FontWeight(500)
			if description != "" {
				ui.Text(c, description).FontSize(t.TextXS).TextColor(t.FgMuted)
			}
		})
		if control != nil {
			ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(8).Children(control)
		}
	})
}

// NavItem is an item of the app sidebar: an icon and a label (the label
// becomes a tooltip when collapsed). Read Clicked on the result.
func NavItem(c *ui.Context, icon, label string, active, collapsed bool) ui.Element {
	t := Current()
	b := ui.ButtonBase(c).Label(label).Height(32).Gap(10).Padding(0, 10).Radius(t.RadiusSM).
		AlignItems(ui.Center).Justify(ui.Start).Cursor(ui.CursorPointer).FocusRing(true)
	if collapsed {
		b.Width(36).Padding(0).Justify(ui.Center).Tooltip(label)
	}
	switch {
	case active:
		b.Background(t.AccentSoft).TextColor(t.Accent)
	case b.Hovered():
		b.Background(t.BgHover).TextColor(t.Fg)
	default:
		b.TextColor(t.FgMuted)
	}
	b.Children(func() {
		Icon(c, icon).FontSize(16)
		if !collapsed {
			txt := ui.Text(c, label).FontSize(t.TextSM).SingleLine().Ellipsis("…")
			if active {
				txt.FontWeight(600)
			}
		}
	})
	return b
}

// SectionLabel is the small uppercase heading of a sidebar section.
func SectionLabel(c *ui.Context, text string) ui.Element {
	t := Current()
	return ui.Text(c, strings.ToUpper(text)).FontSize(11).FontWeight(600).LetterSpacing(0.6).
		TextColor(t.FgFaint).Padding(8, 10, 4, 10)
}

// CodeBlock shows highlighted code with a copy button, through the
// markdown renderer (one fenced block). opts may be nil.
func CodeBlock(c *ui.Context, lang, code string, opts *markdown.Options) ui.Element {
	if opts == nil {
		opts = &markdown.Options{}
	}
	if opts.Theme == nil {
		opts.Theme = Current().Markdown()
	}
	fence := "```"
	for strings.Contains(code, fence) {
		fence += "`"
	}
	src := fence + lang + "\n" + code + "\n" + fence + "\n"
	return ui.Column(c).Children(func() { markdown.View(c, src, opts) })
}

// Spinner is a rotating loader icon.
func Spinner(c *ui.Context) ui.Element {
	s := Icon(c, IconSpinner)
	return s.Rotate(s.Loop("spin", time.Second, ui.Linear) * 360)
}

// EmptyState is a centred icon, title and description, for views with
// nothing to show yet.
func EmptyState(c *ui.Context, icon, title, description string) ui.Element {
	t := Current()
	return ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Gap(8).Padding(32).Children(func() {
		Icon(c, icon).FontSize(32).TextColor(t.FgFaint)
		ui.Text(c, title).FontSize(t.TextLG).FontWeight(600).TextAlign(ui.Center)
		if description != "" {
			ui.Text(c, description).FontSize(t.TextSM).TextColor(t.FgMuted).TextAlign(ui.Center).MaxWidth(420)
		}
	})
}
