// Package drawing draws a machine from its definition in full form, as SVG,
// and marks on it what a run shows of its state.
package drawing

import (
	"encoding/json/v2"
	"fmt"
	"html/template"
	"math"
	"path"
	"strings"

	"github.com/zummed/corbie/agent"
	"github.com/zummed/corbie/flow"
	"github.com/zummed/corbie/machine"
	"github.com/zummed/corbie/statechart"
)

// A Run is what a run shows of its state, to be marked on its machine's
// drawing.
type Run struct {
	// Shown is what the machine's type shows a watcher at the latest step,
	// and Before what it showed at the step before.
	Shown, Before any
	// Events are the names of the events the latest step took.
	Events []string
	// Entered are the states a statechart has been in, at any step.
	Entered []string
	// Asked are the jobs the latest step asked for, by name.
	Asked []string
	// Ended says the run has ended: the states it ended in are no longer
	// lit as current.
	Ended bool
}

// Draw gives the drawing of a definition, with a run's state marked on it
// when run is not nil.
func Draw(def *machine.Definition, run *Run) (template.HTML, error) {
	d, err := drawOf(def, run, nil)
	if err != nil {
		return "", err
	}
	return d.svg(def.Type + " " + def.Name), nil
}

// Unfold says how a drawing's boxes unfold. A box's path is the names of
// the boxes down to it, from the machine drawn, joined by /.
type Unfold struct {
	// Open holds the paths of the boxes unfolded.
	Open map[string]bool
	// Href gives the address of a box's link.
	Href func(path string) string
	// Part gives the file a part of an agent in a file is included from, or
	// nothing: the agent by its name, the part by its name and its `does:`.
	Part func(file, name, part, does string) string
}

// Unfolded gives the drawing of the machine in a file, with a link on each
// box whose job names a machine, and the machines of the boxes unfolded
// drawn inside them.
func Unfolded(draft *machine.Draft, file string, u Unfold) (template.HTML, error) {
	def := draft.Definition
	d, err := drawOf(def, nil, &walker{unfold: u, draft: draft, file: file})
	if err != nil {
		return "", err
	}
	return d.svg(def.Type + " " + def.Name), nil
}

func drawOf(def *machine.Definition, run *Run, w *walker) (drawn, error) {
	switch def.Type {
	case "flow":
		var f flow.Flow
		if err := json.Unmarshal(def.Body, &f); err != nil {
			return drawn{}, err
		}
		return drawFlow(&f, run, w), nil
	case "statechart":
		var c statechart.Chart
		if err := json.Unmarshal(def.Body, &c); err != nil {
			return drawn{}, err
		}
		return drawChart(&c, run, w), nil
	case "agent":
		var a agent.Agent
		if err := json.Unmarshal(def.Body, &a); err != nil {
			return drawn{}, err
		}
		return drawAgent(def.Name, &a, def.Ask, run, w), nil
	}
	return drawn{}, fmt.Errorf("a machine of type %q has no drawing", def.Type)
}

// A walker is one machine of a drawing whose boxes unfold: its draft, the
// file it is in, or for one written in place the file that writes it, and the
// path of the box it is drawn in. A drawing on a run's page has none.
type walker struct {
	unfold    Unfold
	draft     *machine.Draft
	dir, file string
	path      string
}

// open gives the link of a box whose job names a machine, and the machine's
// drawing when the box is unfolded.
func (w *walker) open(name, job string) (string, *drawn) {
	if w == nil || w.draft.Children[job] == nil {
		return "", nil
	}
	at := strings.TrimPrefix(w.path+"/"+name, "/")
	href := w.unfold.Href(at)
	if !w.unfold.Open[at] {
		return href, nil
	}
	sub := &walker{unfold: w.unfold, draft: w.draft.Children[job], file: w.file, path: at}
	if !inPlace(job) {
		sub.file = path.Clean(job)
	}
	d, err := drawOf(sub.draft.Definition, nil, sub)
	if err != nil {
		return href, nil
	}
	return href, &d
}

// inPlace says a name in a definition's table of machines is that of a
// machine written in place.
func inPlace(job string) bool { return strings.HasPrefix(job, "in-place-") }

// part gives the file a part is included from.
func (w *walker) part(name, part, does string) string {
	if w == nil || w.file == "" || w.unfold.Part == nil {
		return ""
	}
	return w.unfold.Part(w.file, name, part, does)
}

// describe writes what a job is into an item: its icon and its line.
func (w *walker) describe(it *item, job *machine.JobSpec) {
	switch {
	case job == nil:
	case inPlace(job.Machine):
		it.icon, it.lines = "flow", []string{"machine: written in place"}
		if w != nil && w.draft.Children[job.Machine] != nil {
			def := w.draft.Children[job.Machine].Definition
			it.icon, it.lines = def.Type, []string{fit(def.Type + ": " + def.Name)}
		}
	case job.Machine != "":
		it.icon, it.lines = "flow", []string{fit("machine: " + job.Machine)}
		if w != nil && w.draft.Children[job.Machine] != nil {
			it.icon = w.draft.Children[job.Machine].Definition.Type
		}
	case job.Script != "":
		it.icon, it.lines = "script", []string{fit("script: " + job.Script)}
	}
}

// The sizes a drawing is laid out by: every box is a card of one width,
// with an icon, a title, and the lines under it.
const (
	cardW   = 232.0
	lineH   = 15.0
	noteH   = 19.0
	sideGap = 28.0
	rowGap  = 40.0
	margin  = 24.0
	longest = 26
	// longestChip fits between the mid points of two arrows side by side.
	longestChip = 18
)

// A note is what leaves a box with no arrow: a transition to an earlier
// state, or back to the same one.
type note struct {
	text  string
	taken bool
}

// An item is what a box says: its icon, its name, the lines and notes under
// it, its tooltip, its link, and the drawing unfolded inside it.
type item struct {
	icon, name string
	lines      []string
	notes      []note
	title      string
	href       string
	in         *drawn
	narrow     bool
}

// fits gives how many letters of a name a box has room for.
func (it item) fits() int {
	if it.narrow {
		return 16
	}
	return longest
}

// head gives how high a box is above what is unfolded inside it.
func (it item) head() float64 {
	h := 44.0
	if len(it.lines) > 0 {
		h = 34 + float64(len(it.lines))*lineH
	}
	if len(it.notes) > 0 {
		h += float64(len(it.notes))*noteH + 2
	}
	return h
}

// size gives how big a box must be to hold an item.
func (it item) size() box {
	b := box{w: cardW, h: it.head()}
	if it.narrow {
		b.w = 132
	}
	if it.in != nil {
		b.w = math.Max(b.w, it.in.w+margin)
		b.h += it.in.h - margin + 16
	}
	return b
}

// A box is where one thing is drawn.
type box struct{ x, y, w, h float64 }

func (b box) cx() float64     { return b.x + b.w/2 }
func (b box) cy() float64     { return b.y + b.h/2 }
func (b box) right() float64  { return b.x + b.w }
func (b box) bottom() float64 { return b.y + b.h }

// drawn is a drawing being made: its parts in the order they are painted,
// states that hold states first, then arrows, then the states they join,
// how far it reaches, to the right, below and to the left, and how many
// arrows go round it.
type drawn struct {
	holders, edges, shapes strings.Builder
	w, h, left             float64
	backs                  int
}

func (d *drawn) reach(b box) {
	d.w = math.Max(d.w, b.right())
	d.h = math.Max(d.h, b.bottom())
}

func (d *drawn) svg(label string) template.HTML {
	var b strings.Builder
	w, h := num(d.w+margin-d.left), num(d.h+margin)
	fmt.Fprintf(&b, `<svg class="machine" viewBox="%s 0 %s %s" width="%s" height="%s" role="img" aria-label="%s">`, num(d.left), w, h, w, h, esc(label))
	b.WriteString(`<defs><marker id="ah" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path class="arrow" d="M0 0L10 5L0 10z"/></marker></defs>`)
	b.WriteString(d.holders.String())
	b.WriteString(d.edges.String())
	b.WriteString(d.shapes.String())
	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}

// paint draws an item in a box, as a link where it has one, and the drawing
// unfolded inside it. behind is what is drawn under the box.
func (d *drawn) paint(it item, b box, class string, rx float64, behind string) {
	if it.in != nil {
		class += " unfolded"
	}
	if it.href != "" {
		fmt.Fprintf(&d.shapes, `<a href="%s">`, esc(it.href))
	}
	tx, ty, anchor := b.x+50, b.y+22.0, "start"
	if len(it.lines) == 0 {
		ty = b.y + 26
	}
	if it.icon == "" {
		tx, anchor = b.cx(), "middle"
	}
	fmt.Fprintf(&d.shapes, `<g class="%s">%s<rect x="%s" y="%s" width="%s" height="%s" rx="%s"/><text class="title" x="%s" y="%s" text-anchor="%s">%s</text>`,
		class, behind, num(b.x), num(b.y), num(b.w), num(b.h), num(rx), num(tx), num(ty), anchor, esc(clip(it.name, it.fits())))
	y := ty
	for _, l := range it.lines {
		y += lineH
		fmt.Fprintf(&d.shapes, `<text class="sub" x="%s" y="%s" text-anchor="%s">%s</text>`, num(tx), num(y), anchor, esc(l))
	}
	for _, n := range it.notes {
		y += noteH
		cls := "note"
		if n.taken {
			cls += " taken"
		}
		fmt.Fprintf(&d.shapes, `<text class="%s" x="%s" y="%s" text-anchor="%s">%s</text>`, cls, num(tx), num(y), anchor, esc(n.text))
	}
	if it.icon != "" {
		tile := box{x: b.x + 12, y: b.y + 11, w: 28, h: 28}
		fmt.Fprintf(&d.shapes, `<rect class="tile %s" x="%s" y="%s" width="%s" height="%s" rx="7"/>`, it.icon, num(tile.x), num(tile.y), num(tile.w), num(tile.h))
		if it.icon == "model" {
			fmt.Fprintf(&d.shapes, `<path class="spark" d="%s"/>`, spark(tile.cx(), tile.cy(), 9))
		} else {
			fmt.Fprintf(&d.shapes, `<use class="icon" href="#g-%s" x="%s" y="%s" width="20" height="14"/>`, it.icon, num(tile.x+4), num(tile.y+7))
		}
	}
	if it.href != "" {
		sign := "›"
		if it.in != nil {
			sign = "⌄"
		}
		fmt.Fprintf(&d.shapes, `<text class="fold" x="%s" y="%s" text-anchor="middle">%s</text>`, num(b.right()-15), num(b.y+24), sign)
	}
	if it.title != "" {
		fmt.Fprintf(&d.shapes, `<title>%s</title>`, esc(it.title))
	}
	d.shapes.WriteString(`</g>`)
	if it.href != "" {
		d.shapes.WriteString(`</a>`)
	}
	if it.in != nil {
		dx := (b.w - it.in.w - margin) / 2
		fmt.Fprintf(&d.shapes, `<g transform="translate(%s %s)">%s%s%s</g>`, num(b.x+dx), num(b.y+it.head()-margin+8),
			it.in.holders.String(), it.in.edges.String(), it.in.shapes.String())
	}
	d.reach(b)
}

// down draws an arrow straight down, or down to a rail above the box it goes
// to, along it, and down into the box, so arrows from one box or to one row
// run together. A label is a chip on the arrow: on the rail, where it is
// apart from the chips of other arrows into the same row or box.
func (d *drawn) down(from, to box, class, label string) {
	const r = 8
	x1, y1, x2, y2 := from.cx(), from.bottom(), to.cx(), to.y
	rail := y2 - rowGap/2
	if label != "" {
		rail = y2 - rowGap
	}
	var p string
	cx, cy := (x1+x2)/2, rail
	if math.Abs(x1-x2) < 1 {
		p = fmt.Sprintf("M%s %s V%s", num(x1), num(y1), num(y2))
		cy = y2 - 14
	} else {
		turn := r
		if x2 < x1 {
			turn = -r
		}
		p = fmt.Sprintf("M%s %s V%s Q%s %s %s %s H%s Q%s %s %s %s V%s", num(x1), num(y1), num(rail-r),
			num(x1), num(rail), num(x1+float64(turn)), num(rail), num(x2-float64(turn)),
			num(x2), num(rail), num(x2), num(rail+r), num(y2))
	}
	fmt.Fprintf(&d.edges, `<path class="%s" d="%s" marker-end="url(#ah)"/>`, esc(strings.TrimSpace("edge "+class)), p)
	d.reach(box{x: math.Max(x1, x2), y: y2})
	if label != "" {
		d.chip(cx, cy, label)
	}
}

// across draws an arrow from a box to one beside it.
func (d *drawn) across(from, to box, class, label string, bend float64) {
	x1, x2, y := from.right(), to.x, from.cy()+bend
	if x2 < x1 {
		x1, x2 = from.x, to.right()
	}
	fmt.Fprintf(&d.edges, `<path class="%s" d="M%s %s H%s" marker-end="url(#ah)"/>`, esc(strings.TrimSpace("edge "+class)), num(x1), num(y), num(x2))
	if label != "" {
		d.chip((x1+x2)/2, y-11, label)
	}
}

// chip draws a label in a pill, short enough to stay apart from the chips of
// the arrows beside it, with the whole label as its tooltip.
func (d *drawn) chip(x, y float64, label string) {
	short := clip(label, longestChip)
	w := labelWidth(short) + 14
	title := ""
	if short != label {
		title = "<title>" + esc(label) + "</title>"
	}
	fmt.Fprintf(&d.edges, `<rect class="chip" x="%s" y="%s" width="%s" height="18" rx="9"/><text class="on" x="%s" y="%s" text-anchor="middle">%s%s</text>`,
		num(x-w/2), num(y-9), num(w), num(x), num(y+4), esc(short), title)
	d.reach(box{x: x + w/2, y: y + 9})
	d.left = math.Min(d.left, x-w/2)
}

// labelWidth is about how wide a label is, set in IBM Plex Sans at 11.5px.
func labelWidth(label string) float64 { return float64(len([]rune(label))) * 6.3 }

func num(f float64) string {
	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.1f", f), "0"), ".")
}

// fit gives a line short enough for a card: where a path is too long, its
// file's name, and the whole path is in the file's text.
func fit(line string) string {
	if len([]rune(line)) <= longest {
		return line
	}
	field, value, ok := strings.Cut(line, ": ")
	if ok && strings.Contains(value, "/") {
		line = field + ": …/" + path.Base(value)
	}
	return clip(line, longest)
}

func esc(s string) string { return template.HTMLEscapeString(s) }

// shown reads a field of what a type shows of its state.
func shown(v any, field string) any {
	m, _ := v.(map[string]any)
	return m[field]
}
