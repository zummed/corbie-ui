// Package drawing draws a machine from its definition in full form, as SVG,
// and marks on it what a run shows of its state.
package drawing

import (
	"encoding/json/v2"
	"fmt"
	"html/template"
	"maps"
	"math"
	"path"
	"slices"
	"strings"

	"github.com/zummed/corbie/machine"
	"github.com/zummed/corbie/machine/agent"
	"github.com/zummed/corbie/machine/flow"
	"github.com/zummed/corbie/machine/statechart"
)

// A Run is what a run shows of its state as of one step, to be marked on
// its machine's drawing.
type Run struct {
	// Steps are the steps of its record, in order, up to the step it is
	// drawn as of: the latest.
	Steps []Step
	// Going are the jobs going after the latest step, and Held those of
	// them held for the ask. A job is named as the definition names what
	// asked for it, and the ask as `ask`.
	Going, Held []string
	// Children are the runs its jobs started, by the job's name.
	Children map[string]Child
	// Machines are the machines its definition names, by the name its jobs
	// give them.
	Machines map[string]Machine
	// Ended says the run had ended by the latest step: the states it ended
	// in are no longer lit as current. Failed says it ended with status
	// failed.
	Ended, Failed bool
}

// A Step is one step of a run's record: the run's state after it, the
// events it took, the jobs whose failure it took, and the jobs it asked
// for, by name.
type Step struct {
	Shown  machine.Shown
	Events []string
	Failed []string
	Asked  []string
}

// A Child is a run a job started: its ID, its machine and the machine's
// type.
type Child struct{ ID, Machine, Type string }

// A Machine is a machine a definition names: its type and its name.
type Machine struct{ Type, Name string }

// latest gives the run's state after the latest step.
func (r *Run) latest() machine.Shown {
	if r == nil || len(r.Steps) == 0 {
		return machine.Shown{}
	}
	return r.Steps[len(r.Steps)-1].Shown
}

// asked says whether any step asked for a job.
func (r *Run) asked(name string) bool {
	return r != nil && slices.ContainsFunc(r.Steps, func(s Step) bool { return slices.Contains(s.Asked, name) })
}

// failedAt says the run failed at a job: its last step took the job's
// failure, and the run ended with status failed.
func (r *Run) failedAt(name string) bool {
	return r != nil && r.Failed && len(r.Steps) > 0 && slices.Contains(r.Steps[len(r.Steps)-1].Failed, name)
}

// Draw gives the drawing of a definition, with a run's state marked on it
// when run is not nil.
func Draw(def *machine.Definition, run *Run) (template.HTML, error) {
	w := &walker{}
	if run != nil {
		w.children, w.machines = run.Children, run.Machines
	}
	d, err := drawOf(def, run, w)
	if err != nil {
		return "", err
	}
	return d.svg(def.Type + " " + def.Name), nil
}

// Unfold says how a drawing's cards unfold. A card's path is the names of
// the cards down to it, from the machine drawn, joined by /.
type Unfold struct {
	// File is the file of the machine drawn.
	File string
	// Open holds the paths of the cards unfolded.
	Open map[string]bool
	// Href gives the address of a card's link.
	Href func(path string) string
	// Called gives the address of the machine at a location a call writes
	// out, or none where there is no machine there.
	Called func(at string) string
}

// Unfolded gives the drawing of a machine read from its file, with a link
// on each card whose job names a machine, and the machines of the cards
// unfolded drawn inside them.
func Unfolded(draft *machine.Draft, u Unfold) (template.HTML, error) {
	def := draft.Definition
	d, err := drawOf(def, nil, &walker{unfold: u, draft: draft, file: u.File})
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
		return drawAgent(def, &a, run, w), nil
	case "decision":
		var dec agent.Decision
		if err := json.Unmarshal(def.Body, &dec); err != nil {
			return drawn{}, err
		}
		return drawDecision(&dec, run), nil
	}
	return drawn{}, fmt.Errorf("a machine of type %q has no drawing", def.Type)
}

// A walker is what a drawing knows of the machines its jobs name. On the
// Machines page it is a machine's draft, its file and the path of the card
// it is drawn in. On a run's page it is the machines its definition names
// and the runs its jobs started.
type walker struct {
	unfold   Unfold
	draft    *machine.Draft
	file     string
	machines map[string]Machine
	children map[string]Child
	path     string
}

// open gives the link of a card whose job names a machine in a draft, and
// the machine's drawing when the card is unfolded.
func (w *walker) open(at, job string) (string, *drawn) {
	at = strings.TrimPrefix(w.path+"/"+at, "/")
	href := w.unfold.Href(at)
	if !w.unfold.Open[at] {
		return href, nil
	}
	sub := &walker{unfold: w.unfold, draft: w.draft.Children[job], file: path.Clean(job), path: at}
	if inPlace(job) {
		sub.file = w.file
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

// ToolkitFile gives the file a toolkit of an agent is included from, as
// Corbie writes out the agent: the file its tools are written in, or its
// `description:` where it has no tools, when that is not own, the agent's
// own file. A toolkit written in place gives nothing.
func ToolkitFile(def *machine.Definition, toolkit, own string) string {
	at := "toolkits." + toolkit + "."
	file := fileOf(def.Lines[at+"description"])
	for _, place := range slices.Sorted(maps.Keys(def.Lines)) {
		if strings.HasPrefix(place, at+"tools.") {
			file = fileOf(def.Lines[place])
			break
		}
	}
	if file == own {
		return ""
	}
	return file
}

// fileOf gives the file of a value's file and line, as base.yaml:3.
func fileOf(line string) string {
	if i := strings.LastIndexByte(line, ':'); i >= 0 {
		return line[:i]
	}
	return line
}

// describe writes what a card's job is into it: its icon, its lines, a line
// where it needs approval, and where the job names a machine, the link to
// it. at is the name a run gives the card's job, and its path on the
// Machines page.
func (w *walker) describe(it *item, at string, job *machine.JobSpec) {
	if job == nil {
		return
	}
	w.describeJob(it, at, job)
	if job.Approve {
		it.lines = append(it.lines, "approve: true")
	}
}

// describeJob writes a card's icon, its line and its link. The machine a
// job names is known by its draft, or by the definition a run was started
// from; one that is neither is known only by how the definition names it.
// On a run's page the card links to the run its job started, and on the
// Machines page a call's card to the machine at the location it writes out.
func (w *walker) describeJob(it *item, at string, job *machine.JobSpec) {
	switch {
	case job.Script != "":
		it.icon, it.lines = "script", []string{fit("script: " + job.Script)}
		return
	case job.Send != nil:
		it.icon, it.lines = "statechart", []string{fit("send: " + job.Send.Event), fit("to: " + Address(&job.Send.To))}
		return
	case job.Call != nil:
		it.icon, it.lines = "file", []string{fit("call: " + Address(job.Call))}
		if c, ok := w.children[at]; ok {
			it.icon, it.lines, it.href = c.Type, append(it.lines, fit(c.Type+": "+c.Machine)), "/run/"+c.ID
		} else if w.unfold.Called != nil && job.Call.Expr == "" && !machine.HasExpr(job.Call.At) {
			it.href = w.unfold.Called(job.Call.At)
		}
		return
	case job.Machine == "":
		return
	}
	it.icon, it.lines = "file", []string{fit("machine: " + job.Machine)}
	if inPlace(job.Machine) {
		it.lines = []string{"machine: written in place"}
	}
	if m, ok := w.machines[job.Machine]; ok {
		it.icon, it.lines = m.Type, []string{fit(m.Type + ": " + m.Name)}
	}
	if c, ok := w.children[at]; ok {
		it.icon, it.lines, it.href = c.Type, []string{fit(c.Type + ": " + c.Machine)}, "/run/"+c.ID
		return
	}
	if w.draft == nil || w.draft.Children[job.Machine] == nil {
		return
	}
	def := w.draft.Children[job.Machine].Definition
	it.icon, it.lines = def.Type, []string{fit(def.Type + ": " + def.Name)}
	it.href, it.in = w.open(at, job.Machine)
}

// The sizes a drawing is laid out by: every card is of one width, with an
// icon, a title, and the lines under it.
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
	// cardR is how round the corners of every card are.
	cardR = 10.0
	// nameW is about how wide a letter of a holder's name is, set in IBM
	// Plex Mono at 12px.
	nameW = 7.3
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
// states that hold states first, then arrows, then their chips, then the
// states they join, and how far it reaches, to the right, below and to the
// left.
type drawn struct {
	holders, edges, chips, shapes strings.Builder
	w, h, left                    float64
}

func (d *drawn) reach(b box) {
	d.w = math.Max(d.w, b.right())
	d.h = math.Max(d.h, b.bottom())
}

func (d *drawn) svg(label string) template.HTML {
	var b strings.Builder
	w, h := num(d.w+margin-d.left), num(d.h+margin)
	fmt.Fprintf(&b, `<svg class="machine" viewBox="%s 0 %s %s" width="%s" height="%s" role="img" aria-label="%s">`, num(d.left), w, h, w, h, esc(label))
	b.WriteString(`<defs><marker id="ah" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path class="arrow" d="M0 0L10 5L0 10z"/></marker>` +
		`<marker id="at" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" markerHeight="8" markerUnits="userSpaceOnUse" orient="auto-start-reverse"><path class="arrow taken" d="M0 0L10 5L0 10z"/></marker></defs>`)
	b.WriteString(d.holders.String())
	b.WriteString(d.edges.String())
	b.WriteString(d.chips.String())
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
	fmt.Fprintf(&d.shapes, `<g class="%s">%s<rect class="box" x="%s" y="%s" width="%s" height="%s" rx="%s"/><text class="title" x="%s" y="%s" text-anchor="%s">%s</text>`,
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
		fmt.Fprintf(&d.shapes, `<g transform="translate(%s %s)">%s%s%s%s</g>`, num(b.x+dx), num(b.y+it.head()-margin+8),
			it.in.holders.String(), it.in.edges.String(), it.in.chips.String(), it.in.shapes.String())
	}
	d.reach(b)
}

// down draws an arrow straight down, or down to a rail above the box it goes
// to, along it, and down into the box, so arrows from one box or to one row
// run together. A label is a chip on the arrow: on the rail, where it is
// apart from the chips of other arrows into the same row or box.
func (d *drawn) down(from, to box, class, label string) {
	x1, y1, x2, y2 := from.cx(), from.bottom(), to.cx(), to.y
	rail := y2 - rowGap/2
	if label != "" {
		rail = y2 - rowGap
	}
	cx, cy := (x1+x2)/2, rail
	if math.Abs(x1-x2) < 1 {
		cy = y2 - 14
	}
	d.line([]pt{{x1, y1}, {x1, rail}, {x2, rail}, {x2, y2}}, class)
	if label != "" {
		d.chip(cx, cy, label, class)
	}
}

// line draws an arrow through points, one after another across or down,
// with its turns rounded.
func (d *drawn) line(pts []pt, class string) {
	const r = 8
	pts = simplify(pts)
	var p strings.Builder
	at := pts[0]
	fmt.Fprintf(&p, "M%s %s", num(at.x), num(at.y))
	to := func(q pt) {
		if math.Abs(q.y-at.y) < 0.01 {
			fmt.Fprintf(&p, " H%s", num(q.x))
		} else {
			fmt.Fprintf(&p, " V%s", num(q.y))
		}
		at = q
	}
	toward := func(a, b pt, n float64) pt {
		l := math.Hypot(b.x-a.x, b.y-a.y)
		return pt{a.x + (b.x-a.x)*n/l, a.y + (b.y-a.y)*n/l}
	}
	for i := 1; i < len(pts)-1; i++ {
		prev, q, next := pts[i-1], pts[i], pts[i+1]
		n := math.Min(r, math.Min(math.Hypot(q.x-prev.x, q.y-prev.y), math.Hypot(next.x-q.x, next.y-q.y))/2)
		to(toward(q, prev, n))
		end := toward(q, next, n)
		fmt.Fprintf(&p, " Q%s %s %s %s", num(q.x), num(q.y), num(end.x), num(end.y))
		at = end
	}
	to(pts[len(pts)-1])
	fmt.Fprintf(&d.edges, `<path class="%s" d="%s" marker-end="url(#%s)"/>`, esc(strings.TrimSpace("edge "+class)), p.String(), head(class))
	for _, q := range pts {
		d.reach(box{x: q.x, y: q.y})
		d.left = math.Min(d.left, q.x)
	}
}

// head gives the arrowhead of an arrow: a transition just taken has its
// own, in its colour and of the size of the others.
func head(class string) string {
	if slices.Contains(strings.Fields(class), "taken") {
		return "at"
	}
	return "ah"
}

// chip draws a label in a pill, short enough to stay apart from the chips of
// the arrows beside it, with the whole label as its tooltip. It has the
// classes of its arrow.
func (d *drawn) chip(x, y float64, label, class string) {
	short := clip(label, longestChip)
	w := chipWidth(label)
	title := ""
	if short != label {
		title = "<title>" + esc(label) + "</title>"
	}
	fmt.Fprintf(&d.chips, `<rect class="%s" x="%s" y="%s" width="%s" height="18" rx="9"/><text class="on" x="%s" y="%s" text-anchor="middle">%s%s</text>`,
		esc(strings.TrimSpace("chip "+class)), num(x-w/2), num(y-9), num(w), num(x), num(y+4), esc(short), title)
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
