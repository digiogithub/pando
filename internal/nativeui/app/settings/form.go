package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// Kind is the editor of a Field.
type Kind int

// Field kinds.
const (
	KBool   Kind = iota // switch
	KText               // single-line text
	KInt                // whole number (text input, validated on save)
	KFloat              // decimal number
	KSelect             // drop-down over Opts
	KList               // list of strings (chips)
	KSecret             // password input
	KStatic             // read-only text
	KSlider             // 0..1 slider
	KModel              // model id with autosuggest over GET /api/v1/models
)

// Opt is an option of a KSelect: the stored value and its label (an i18n
// key or a literal).
type Opt struct{ V, L string }

// Field describes one editable value of a JSON document. Label and Desc are
// i18n keys of the Web UI catalogs or literals (a.T returns an unknown key
// unchanged).
type Field struct {
	Path  string // dotted path in the document ("server.port")
	Label string
	Desc  string
	Kind  Kind
	Opts  []Opt
	// Inverse stores !value (a "disabled" flag shown as an "enabled" switch).
	Inverse bool
	// Empty is the option whose value is stored as "" (KSelect).
	Empty string
	// Zero is the displayed text of an absent KInt/KFloat.
	Placeholder string
	// Enabled / Visible are evaluated against the current edits.
	Enabled, Visible func(d *Doc) bool
	// After runs when the value changed in the UI (coupled fields).
	After func(d *Doc)
	// Width of the control in DIPs (default 240).
	Width float32
	// Plain shows a KModel as a plain text input while it returns true.
	Plain func(d *Doc) bool
	// Picked runs when a KModel suggestion is chosen.
	Picked func(d *Doc, m client.Model)
	// Control replaces the editor of a field: it edits *value (text kinds),
	// or draws an action (KStatic rows, which are never saved).
	Control func(c *ui.Context, d *Doc, value *string, w float32) ui.Element
	// Extra draws content below the row, across the whole width.
	Extra func(c *ui.Context, d *Doc)
}

// Group is a titled card of fields.
type Group struct {
	Title, Desc string
	Fields      []Field
}

type val struct {
	s    string
	b    bool
	l    []string
	add  string // pending entry of a KList
	seen bool   // the key existed in the document
}

// Doc edits a JSON config document through GET / PUT of one endpoint,
// exactly as the Web UI stores do: unknown keys survive the round trip.
type Doc struct {
	s *Settings
	status
	Endpoint string
	// Sub is the dotted path of the edited object inside the response ("" =
	// the root, "config" for sandbox, "snapshots" for the services doc).
	Sub string
	// PutWhole sends the whole response with Sub replaced (services,
	// settings); otherwise only the Sub object is sent.
	PutWhole bool
	Groups   []Group
	// Head draws content before the groups; Tail after them.
	Head, Tail func(c *ui.Context, d *Doc)
	// Loaded runs after every (re)seed with the root document.
	Loaded func(d *Doc, root client.Obj)
	// Skip lists keys never sent back (UI-only values).
	Skip []string
	// SavedMsg is the success toast (default "Settings saved").
	SavedMsg string

	raw  client.Obj
	vals map[string]*val
	orig map[string]string
}

func newDoc(s *Settings, endpoint string, groups ...Group) *Doc {
	return &Doc{s: s, Endpoint: endpoint, Groups: groups, vals: map[string]*val{}, orig: map[string]string{}}
}

func (d *Doc) fields(fn func(f *Field)) {
	for gi := range d.Groups {
		for fi := range d.Groups[gi].Fields {
			fn(&d.Groups[gi].Fields[fi])
		}
	}
}

// Raw returns the last document from the server (root).
func (d *Doc) Raw() client.Obj { return d.raw }

// Get returns the current text value of a field path (for Enabled hooks).
// A KSelect holds its stored value, never its label.
func (d *Doc) Get(path string) string {
	if v, ok := d.vals[path]; ok {
		return v.s
	}
	return ""
}

// Set changes the text value of a field path (coupled fields).
func (d *Doc) Set(path, value string) {
	if v, ok := d.vals[path]; ok {
		v.s = value
	}
}

// Bool returns the current switch state of a field path.
func (d *Doc) Bool(path string) bool {
	if v, ok := d.vals[path]; ok {
		return v.b
	}
	return false
}

// Load fetches the document.
func (d *Doc) Load() {
	if d.loading {
		return
	}
	d.loading = true
	a := d.s.app
	a.Go(func(ctx context.Context) {
		o, err := a.Client.GetObj(ctx, d.Endpoint)
		a.Update(func() {
			d.loading = false
			if err != nil {
				d.err = err.Error()
				return
			}
			d.err = ""
			d.seed(o)
		})
	})
}

func lookupPath(o client.Obj, path string) (any, bool) {
	if path == "" {
		return o, true
	}
	var cur any = o
	for _, p := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func setPath(o client.Obj, path string, v any) {
	parts := strings.Split(path, ".")
	cur := o
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[p] = next
		}
		cur = next
	}
	cur[parts[len(parts)-1]] = v
}

func scalarString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

func stringList(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		out = append(out, scalarString(e))
	}
	return out
}

func (d *Doc) seed(root client.Obj) {
	d.raw = root
	target := root
	if d.Sub != "" {
		if v, ok := lookupPath(root, d.Sub); ok {
			if m, ok := v.(map[string]any); ok {
				target = m
			}
		}
	}
	d.vals = map[string]*val{}
	d.fields(func(f *Field) {
		raw, found := lookupPath(target, f.Path)
		v := &val{seen: found && raw != nil}
		switch f.Kind {
		case KBool:
			b, _ := raw.(bool)
			v.b = b != f.Inverse
		case KList:
			v.l = stringList(raw)
		case KSelect:
			sv := scalarString(raw)
			if sv == "" {
				sv = f.Empty
			}
			if sv == "" && len(f.Opts) > 0 {
				sv = f.Opts[0].V
			}
			v.s = sv
		default:
			v.s = scalarString(raw)
		}
		d.vals[f.Path] = v
	})
	d.orig = map[string]string{}
	d.fields(func(f *Field) { d.orig[f.Path] = d.canon(f) })
	d.loaded = true
	if d.Loaded != nil {
		d.Loaded(d, root)
	}
}

func (d *Doc) optLabel(f *Field, value string) string {
	for _, o := range f.Opts {
		if o.V == value {
			return d.s.app.T(o.L)
		}
	}
	return value
}

// canon is the comparable form of a field's current value.
func (d *Doc) canon(f *Field) string {
	v := d.vals[f.Path]
	if v == nil {
		return ""
	}
	switch f.Kind {
	case KBool:
		return strconv.FormatBool(v.b)
	case KList:
		return strings.Join(v.l, "\x00")
	}
	return strings.TrimSpace(v.s)
}

// Dirty reports edits not saved yet.
func (d *Doc) Dirty() bool {
	if !d.loaded {
		return false
	}
	dirty := false
	d.fields(func(f *Field) {
		if f.Kind != KStatic && d.canon(f) != d.orig[f.Path] {
			dirty = true
		}
	})
	return dirty
}

// Discard drops the edits.
func (d *Doc) Discard() {
	if d.raw != nil {
		d.seed(d.raw)
	}
}

func deepCopy(o client.Obj) client.Obj {
	b, _ := json.Marshal(o)
	c, _ := client.DecodeObj(b)
	return c
}

// build applies the edits to a copy of the document and returns the body to
// send.
func (d *Doc) build() (body any, root client.Obj, err error) {
	root = deepCopy(d.raw)
	target := root
	if d.Sub != "" {
		if v, ok := lookupPath(root, d.Sub); ok {
			if m, ok := v.(map[string]any); ok {
				target = m
			}
		} else {
			target = client.Obj{}
			setPath(root, d.Sub, target)
		}
	}
	var verr error
	d.fields(func(f *Field) {
		if f.Kind == KStatic || verr != nil {
			return
		}
		v := d.vals[f.Path]
		changed := d.canon(f) != d.orig[f.Path]
		if !changed && !v.seen {
			return
		}
		if !changed {
			return
		}
		switch f.Kind {
		case KBool:
			setPath(target, f.Path, v.b != f.Inverse)
		case KList:
			setPath(target, f.Path, append([]string{}, v.l...))
		case KSelect:
			sv := v.s
			if sv == f.Empty {
				sv = ""
			}
			setPath(target, f.Path, sv)
		case KInt:
			txt := strings.TrimSpace(v.s)
			if txt == "" {
				txt = "0"
			}
			n, e := strconv.ParseInt(txt, 10, 64)
			if e != nil {
				verr = fmt.Errorf("%s: %q is not a whole number", d.s.app.T(f.Label), v.s)
				return
			}
			setPath(target, f.Path, n)
		case KFloat, KSlider:
			txt := strings.TrimSpace(v.s)
			if txt == "" {
				txt = "0"
			}
			n, e := strconv.ParseFloat(txt, 64)
			if e != nil {
				verr = fmt.Errorf("%s: %q is not a number", d.s.app.T(f.Label), v.s)
				return
			}
			setPath(target, f.Path, n)
		default:
			setPath(target, f.Path, strings.TrimSpace(v.s))
		}
	})
	if verr != nil {
		return nil, nil, verr
	}
	for _, k := range d.Skip {
		delete(root, k)
	}
	if d.PutWhole {
		return root, root, nil
	}
	return target, root, nil
}

// Save PUTs the document and reseeds from the answer.
func (d *Doc) Save(done func(bool)) {
	if d.saving || !d.loaded {
		done(false)
		return
	}
	body, root, err := d.build()
	if err != nil {
		d.err = err.Error()
		d.s.app.Toasts.Error(err.Error(), "")
		done(false)
		return
	}
	d.saving, d.err = true, ""
	a := d.s.app
	a.Go(func(ctx context.Context) {
		resp, err := a.Client.SendObj(ctx, "PUT", d.Endpoint, body)
		a.Update(func() {
			d.saving = false
			if err != nil {
				d.err = err.Error()
				a.Toasts.Error(d.s.t("Save failed", "settings.unsaved.saveFailed"), err.Error())
				done(false)
				return
			}
			next := root
			if len(resp) > 0 && d.responseIsDoc(resp) {
				next = resp
			}
			d.seed(next)
			msg := d.SavedMsg
			if msg == "" {
				msg = "Settings saved"
			}
			a.Toasts.Success(msg, "")
			done(true)
		})
	})
}

// responseIsDoc reports whether a PUT answer carries the document (some
// endpoints answer {"status":"ok"}).
func (d *Doc) responseIsDoc(resp client.Obj) bool {
	if d.Sub != "" && !d.PutWhole {
		_, ok := lookupPath(resp, d.Sub)
		return ok
	}
	n := 0
	d.fields(func(f *Field) {
		if _, ok := lookupPath(resp, f.Path); ok {
			n++
		}
	})
	return n > 0
}

// Build draws the document.
func (d *Doc) Build(c *ui.Context) {
	if !d.loaded {
		d.loadingView(c)
		return
	}
	if d.Head != nil {
		d.Head(c, d)
	}
	for gi := range d.Groups {
		g := &d.Groups[gi]
		c := c.Key(fmt.Sprintf("g%d", gi))
		kit.SettingsSection(c, d.s.app.T(g.Title), d.s.app.T(g.Desc), func() {
			for fi := range g.Fields {
				f := &g.Fields[fi]
				if f.Visible != nil && !f.Visible(d) {
					continue
				}
				d.row(c.Key(f.Path), f)
			}
		})
	}
	if d.Tail != nil {
		d.Tail(c, d)
	}
}

func (d *Doc) loadingView(c *ui.Context) {
	d.s.loadingView(c, &d.status)
}

func (d *Doc) row(c *ui.Context, f *Field) {
	a := d.s.app
	t := kit.Current()
	v := d.vals[f.Path]
	if v == nil {
		return
	}
	enabled := f.Enabled == nil || f.Enabled(d)
	w := f.Width
	if w == 0 {
		w = 240
	}
	desc := a.T(f.Desc)
	if f.Desc == "" {
		desc = ""
	}
	before := d.canon(f)
	if f.Kind == KList {
		// Lists use the whole row width under the label.
		ui.Column(c).Padding(12, 16).Gap(8).Children(func() {
			ui.Text(c, a.T(f.Label)).FontSize(t.TextSM).FontWeight(500)
			if desc != "" {
				ui.Text(c, desc).FontSize(t.TextXS).TextColor(t.FgMuted)
			}
			tagList(c, &v.l, &v.add, a.T(f.Placeholder)).Disabled(!enabled)
		})
	} else {
		kit.SettingsRow(c, a.T(f.Label), desc, func() {
			ctl := d.control(c, f, v, w)
			ctl.Disabled(!enabled)
		})
	}
	if f.Extra != nil {
		f.Extra(c.Key("extra"), d)
	}
	if d.canon(f) != before && f.After != nil {
		f.After(d)
	}
}

func (d *Doc) control(c *ui.Context, f *Field, v *val, w float32) ui.Element {
	a := d.s.app
	if f.Control != nil {
		return f.Control(c, d, &v.s, w)
	}
	switch f.Kind {
	case KBool:
		return kit.Switch(c, &v.b, "")
	case KSelect:
		// The select shows labels but the value is kept: labels change
		// with the UI language, the stored value must not.
		labels := make([]string, len(f.Opts))
		for i, o := range f.Opts {
			labels[i] = a.T(o.L)
		}
		label := d.optLabel(f, v.s)
		el := kit.Select(c, &label, labels).Width(w)
		for _, o := range f.Opts {
			if a.T(o.L) == label {
				v.s = o.V
			}
		}
		return el
	case KSecret:
		return kit.Input(c, &v.s, a.T(f.Placeholder)).Password().Width(w)
	case KStatic:
		return kit.Input(c, &v.s, "").ReadOnly(true).Width(w)
	case KSlider:
		return d.slider(c, v, w)
	}
	if f.Kind == KModel && (f.Plain == nil || !f.Plain(d)) {
		var picked func(client.Model)
		if f.Picked != nil {
			picked = func(m client.Model) { f.Picked(d, m) }
		}
		return d.s.modelCombo(c, &v.s, w, a.T(f.Placeholder), picked)
	}
	return kit.Input(c, &v.s, a.T(f.Placeholder)).Width(w)
}

func (d *Doc) slider(c *ui.Context, v *val, w float32) ui.Element {
	x, _ := strconv.ParseFloat(strings.TrimSpace(v.s), 64)
	row := ui.Row(c).Width(w).Gap(8).AlignItems(ui.Center)
	row.Children(func() {
		if ui.Slider(c, &x, 0, 1).Grow(1).Changed() {
			v.s = strconv.FormatFloat(float64(int(x*100+0.5))/100, 'f', -1, 64)
		}
		ui.Text(c, fmt.Sprintf("%.2f", x)).FontSize(kit.Current().TextXS)
	})
	return row
}
