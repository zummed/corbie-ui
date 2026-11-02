package drawing

import (
	"fmt"
	"math"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/zummed/corbie/machine"
	"github.com/zummed/corbie/statechart"
)

// chart is a statechart being laid out: every state by its name, the state
// that holds each, and where each is drawn. A send that names a machine is
// laid out as a state, by a name no state can have.
type chart struct {
	states map[string]*statechart.State
	parent map[string]string   // "" for a state at the top
	kids   map[string][]string // by the state that holds them; "" is the top
	start  map[string]string   // the state each level starts in
	at     map[string]box      // relative to its holder until placed
	sends  []rule
	items  map[string]item
	noted  map[rule]bool // the rules written in a box and drawn as no arrow
	all    []rule
	depths map[string]int // how many transitions each state is from the start
	walker *walker
}

// The room inside a state that holds states: its name above, and a margin.
const (
	inner    = 16.0
	innerTop = 38.0
)

// A rule is one transition: from a state, on an event, to a state.
type rule struct {
	from, event, to, when, timer string
	send                         bool
}

func drawChart(c *statechart.Chart, run *Run, w *walker) drawn {
	ch := &chart{states: map[string]*statechart.State{}, parent: map[string]string{}, kids: map[string][]string{},
		start: map[string]string{"": c.Initial}, at: map[string]box{}, items: map[string]item{}, noted: map[rule]bool{}, walker: w}
	ch.index("", c.States)
	ch.all = ch.ruleList()
	ch.depths = ch.depthsOf()
	marks := ch.marks(run)
	ch.write(marks)
	ch.lay("")
	ch.place("", margin, margin+30)

	var d drawn
	arrows := ch.arrows()
	for _, a := range arrows {
		from, okFrom := ch.at[a.from]
		to, okTo := ch.at[a.to]
		if !okFrom || !okTo {
			continue
		}
		var classes []string
		for _, r := range a.rules {
			if r.timer != "" && !slices.Contains(classes, "timer") {
				classes = append(classes, "timer")
			}
			if r.send {
				classes = append(classes, "send")
			}
			if marks.taken(r) && !slices.Contains(classes, "taken") {
				classes = append(classes, "taken")
			}
		}
		class := strings.Join(classes, " ")
		switch {
		case to.y >= from.bottom():
			d.down(from, to, class, a.label())
		default:
			d.across(from, to, class, a.label(), bend(a, arrows))
		}
	}
	ch.paint(&d, "", marks)
	return d
}

// An arrow is every rule from one state to another.
type arrow struct {
	from, to string
	rules    []rule
}

func (a arrow) label() string {
	var labels []string
	for _, r := range a.rules {
		if l := r.label(); !slices.Contains(labels, l) {
			labels = append(labels, l)
		}
	}
	return strings.Join(labels, ", ")
}

// arrows gives the rules drawn as arrows, together by the states they join.
func (ch *chart) arrows() []arrow {
	var all []arrow
	at := map[[2]string]int{}
	for _, r := range ch.all {
		if ch.noted[r] {
			continue
		}
		key := [2]string{r.from, r.to}
		i, ok := at[key]
		if !ok {
			i = len(all)
			at[key] = i
			all = append(all, arrow{from: r.from, to: r.to})
		}
		all[i].rules = append(all[i].rules, r)
	}
	return all
}

// write writes into its box each rule that goes back: to the state itself,
// or to a state nearer the start.
func (ch *chart) write(marks chartMarks) {
	var backs []arrow
	at := map[[2]string]int{}
	for _, r := range ch.all {
		if r.send || !ch.goesBack(r) {
			continue
		}
		ch.noted[r] = true
		key := [2]string{r.from, r.to}
		i, ok := at[key]
		if !ok {
			i = len(backs)
			at[key] = i
			backs = append(backs, arrow{from: r.from, to: r.to})
		}
		backs[i].rules = append(backs[i].rules, r)
	}
	for _, b := range backs {
		text := b.label() + " ↻"
		if b.from != b.to {
			text = b.label() + " ↩ " + b.to
		}
		it := ch.items[b.from]
		it.notes = append(it.notes, note{text: clip(text, longest+6), taken: slices.ContainsFunc(b.rules, marks.taken)})
		ch.items[b.from] = it
	}
}

// goesBack says a rule leads to the state it leaves, or to a state nearer the
// start than that one.
func (ch *chart) goesBack(r rule) bool {
	to, ok := ch.depths[r.to]
	return r.from == r.to || ok && to < ch.depths[r.from]
}

// depthsOf gives how many transitions each state that can be entered is from
// the state the chart starts in. A state that holds states is entered with
// the state it starts in.
func (ch *chart) depthsOf() map[string]int {
	leaving := map[string][]rule{}
	for _, r := range ch.all {
		leaving[r.from] = append(leaving[r.from], r)
	}
	seen := map[string]int{ch.start[""]: 0}
	queue := []string{ch.start[""]}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if inside := ch.start[n]; inside != "" {
			if _, ok := seen[inside]; !ok {
				seen[inside] = seen[n]
				queue = append(queue, inside)
			}
		}
		for _, r := range leaving[n] {
			if _, ok := seen[r.to]; !ok {
				seen[r.to] = seen[n] + 1
				queue = append(queue, r.to)
			}
		}
	}
	return seen
}

func (ch *chart) index(holder string, states map[string]*statechart.State) {
	for _, name := range sortedKeys(states) {
		ch.states[name] = states[name]
		ch.parent[name] = holder
		ch.kids[holder] = append(ch.kids[holder], name)
		s := states[name]
		ch.items[name] = ch.itemOf(name, s)
		if s == nil {
			continue
		}
		if len(s.States) > 0 {
			ch.start[name] = s.Initial
			ch.index(name, s.States)
		}
		ch.sent(holder, name, "entry", s.Entry)
		ch.sent(holder, name, "exit", s.Exit)
		for _, event := range sortedKeys(s.On) {
			for _, t := range s.On[event] {
				ch.sent(holder, name, event, t.Send)
			}
		}
	}
}

// sent lays out each send that names a machine as a state below the one that
// sends it.
func (ch *chart) sent(holder, name, event string, sends []statechart.Send) {
	for _, send := range sends {
		if send.Machine == "" {
			continue
		}
		key := fmt.Sprintf("%s.%s.%d", name, event, len(ch.sends))
		ch.states[key] = &statechart.State{Job: &machine.JobSpec{Machine: send.Machine}}
		ch.parent[key] = holder
		ch.kids[holder] = append(ch.kids[holder], key)
		it := item{name: path.Base(send.Machine), icon: "flow", lines: []string{"send " + send.Event}}
		if ch.walker != nil && ch.walker.draft.Children[send.Machine] != nil {
			it.icon = ch.walker.draft.Children[send.Machine].Definition.Type
		}
		it.href, it.in = ch.walker.open(key, send.Machine)
		ch.items[key] = it
		ch.sends = append(ch.sends, rule{from: name, event: event, to: key, send: true})
	}
}

// itemOf gives what a state's box says.
func (ch *chart) itemOf(name string, s *statechart.State) item {
	it := item{name: name}
	if s == nil {
		return it
	}
	it.narrow = final(s)
	if !final(s) {
		it.icon = "statechart"
	}
	ch.walker.describe(&it, s.Job)
	if len(s.Jobs) > 0 {
		it.icon = "script"
		it.lines = append(it.lines, fit("jobs: "+strings.Join(sortedKeys(s.Jobs), ", ")))
	}
	it.lines = append(it.lines, tags(s)...)
	if s.Job != nil {
		it.href, it.in = ch.walker.open(name, s.Job.Machine)
	}
	return it
}

// member gives the state of a level that is the state named or holds it.
func (ch *chart) member(level, name string) string {
	if _, ok := ch.states[name]; !ok {
		return ""
	}
	for name != "" {
		if ch.parent[name] == level {
			return name
		}
		name = ch.parent[name]
	}
	return ""
}

// rules gives every rule of the chart.
func (ch *chart) rules() []rule { return ch.all }

func (ch *chart) ruleList() []rule {
	var rules []rule
	for _, name := range sortedKeys(ch.states) {
		s := ch.states[name]
		if s == nil {
			continue
		}
		for _, event := range sortedKeys(s.On) {
			for _, t := range s.On[event] {
				if t.Go == "" {
					continue
				}
				rules = append(rules, rule{from: name, event: event, to: t.Go, when: t.When, timer: s.Timers[event]})
			}
		}
	}
	return append(rules, ch.sends...)
}

func (r rule) label() string {
	label := r.event
	if r.when != "" {
		label += ", when " + clip(strings.TrimSuffix(strings.TrimPrefix(r.when, "${"), "}"), 28)
	}
	if r.timer != "" {
		label += ", " + r.timer
	}
	return label
}

// bend keeps apart two arrows between the same states, one each way.
func bend(a arrow, arrows []arrow) float64 {
	if !slices.ContainsFunc(arrows, func(other arrow) bool { return other.from == a.to && other.to == a.from }) {
		return 0
	}
	if a.from < a.to {
		return -8
	}
	return 8
}

func final(s *statechart.State) bool { return s != nil && s.End != "" }

// lay places the states of a level, each relative to the level, in rows by
// how many transitions each is from the state the level starts in, and gives
// the level's size.
func (ch *chart) lay(level string) (float64, float64) {
	members := ch.kids[level]
	if len(members) == 0 {
		return 0, 0
	}
	size := map[string]box{}
	for _, m := range members {
		s := ch.states[m]
		switch {
		case s != nil && len(s.States) > 0:
			w, h := ch.lay(m)
			size[m] = box{w: math.Max(w, cardW) + 2*inner, h: h + ch.top(m) + inner}
		case final(s):
			b := ch.items[m].size()
			size[m] = box{w: b.w + 8, h: b.h + 8}
		default:
			size[m] = ch.items[m].size()
		}
	}
	next := map[string][]string{}
	for _, r := range ch.rules() {
		a, b := ch.member(level, r.from), ch.member(level, r.to)
		if a != "" && b != "" && a != b && !slices.Contains(next[a], b) && !ch.noted[r] {
			next[a] = append(next[a], b)
		}
	}
	row := map[string]int{}
	var order []string
	visit := func(first string, base int) {
		row[first] = base
		queue := []string{first}
		for len(queue) > 0 {
			n := queue[0]
			queue = queue[1:]
			order = append(order, n)
			for _, m := range next[n] {
				if _, seen := row[m]; !seen {
					row[m] = row[n] + 1
					queue = append(queue, m)
				}
			}
		}
	}
	start := ch.start[level]
	if _, ok := size[start]; !ok && len(members) > 0 {
		start = members[0]
	}
	if start != "" {
		visit(start, 0)
	}
	for _, m := range members {
		if _, seen := row[m]; !seen {
			last := 0
			for _, c := range row {
				last = max(last, c)
			}
			visit(m, last+1)
		}
	}
	var rows [][]string
	for _, n := range order {
		for len(rows) <= row[n] {
			rows = append(rows, nil)
		}
		rows[row[n]] = append(rows[row[n]], n)
	}
	// The gaps within a row are wide enough for the labels between its
	// states.
	sides := map[string]float64{}
	for _, r := range ch.rules() {
		a, b := ch.member(level, r.from), ch.member(level, r.to)
		if a != "" && b != "" && a != b && row[b] == row[a] && !ch.noted[r] {
			sides[a] = math.Max(sides[a], labelWidth(r.label())+44)
			sides[b] = math.Max(sides[b], labelWidth(r.label())+44)
		}
	}
	across := func(c []string) float64 {
		used := 0.0
		for i, n := range c {
			used += size[n].w
			if i < len(c)-1 {
				used += math.Max(sideGap, sides[n])
			}
		}
		return used
	}
	total := 0.0
	for _, c := range rows {
		total = math.Max(total, across(c))
	}
	y := 0.0
	for _, c := range rows {
		high := 0.0
		x := (total - across(c)) / 2
		for _, n := range c {
			b := size[n]
			b.x, b.y = x, y
			ch.at[n] = b
			x += b.w + math.Max(sideGap, sides[n])
			high = math.Max(high, b.h)
		}
		y += high + 2*rowGap
	}
	return total, y - 2*rowGap
}

// top gives the room above the states a state holds: its name and its notes.
func (ch *chart) top(name string) float64 {
	return innerTop + float64(len(ch.items[name].notes))*noteH
}

// place turns where each state of a level is drawn into where it is on the
// drawing, the level being at ox, oy.
func (ch *chart) place(level string, ox, oy float64) {
	for _, n := range ch.kids[level] {
		b := ch.at[n]
		b.x, b.y = b.x+ox, b.y+oy
		ch.at[n] = b
		if len(ch.kids[n]) > 0 {
			ch.place(n, b.x+inner, b.y+ch.top(n))
		}
	}
}

// paint draws the states of a level, a holder before what it holds.
func (ch *chart) paint(d *drawn, level string, marks chartMarks) {
	for _, n := range ch.kids[level] {
		b, s := ch.at[n], ch.states[n]
		d.reach(b)
		class := strings.TrimSpace("state " + marks.of(n))
		if strings.Contains(n, ".") && ch.items[n].name != n {
			class += " sends"
		}
		if final(s) {
			class += " final"
			if s.End == "failed" {
				class += " failed"
			}
		}
		switch {
		case len(ch.kids[n]) > 0:
			fmt.Fprintf(&d.holders, `<g class="%s holder"><rect x="%s" y="%s" width="%s" height="%s" rx="14"/><text class="name" x="%s" y="%s">%s</text></g>`,
				class, num(b.x), num(b.y), num(b.w), num(b.h), num(b.x+14), num(b.y+22), esc(n))
			for i, note := range ch.items[n].notes {
				fmt.Fprintf(&d.shapes, `<text class="note" x="%s" y="%s">%s</text>`, num(b.x+14), num(b.y+22+float64(i+1)*noteH), esc(note.text))
			}
		case final(s):
			inside := box{x: b.x + 4, y: b.y + 4, w: b.w - 8, h: b.h - 8}
			outer := fmt.Sprintf(`<rect class="outer" x="%s" y="%s" width="%s" height="%s" rx="%s"/>`, num(b.x), num(b.y), num(b.w), num(b.h), num(rounded(b)))
			d.paint(ch.items[n], inside, class, rounded(inside), outer)
		default:
			d.paint(ch.items[n], b, class, rounded(b), "")
		}
		if ch.start[level] == n {
			fmt.Fprintf(&d.shapes, `<circle class="start" cx="%s" cy="%s" r="4"/><path class="edge" d="M%s %s V%s" marker-end="url(#ah)"/>`,
				num(b.cx()), num(b.y-24), num(b.cx()), num(b.y-20), num(b.y))
		}
		ch.paint(d, n, marks)
	}
}

// rounded gives how round a state's corners are.
func rounded(box) float64 { return 12 }

// tags are what a state waits on, written in it: its ticket and its timers.
func tags(s *statechart.State) []string {
	if s == nil || len(s.States) > 0 {
		return nil
	}
	var lines []string
	if s.Ticket != "" {
		lines = append(lines, fit("ticket "+s.Ticket))
	}
	for _, name := range sortedKeys(s.Timers) {
		lines = append(lines, fit("timer "+name))
	}
	return lines
}

// chartMarks are a run's states, as the drawing marks them.
type chartMarks struct {
	run            *Run
	now, was, then map[string]bool // in now, in at any step, in at the step before
}

func (ch *chart) marks(run *Run) chartMarks {
	m := chartMarks{run: run, now: map[string]bool{}, was: map[string]bool{}, then: map[string]bool{}}
	if run == nil {
		return m
	}
	with := func(into map[string]bool, names []string) {
		for _, n := range names {
			for ; n != ""; n = ch.parent[n] {
				into[n] = true
			}
		}
	}
	with(m.now, listed(shown(run.Shown, "states")))
	with(m.then, listed(shown(run.Before, "states")))
	with(m.was, run.Entered)
	with(m.was, listed(shown(run.Shown, "states")))
	return m
}

func (m chartMarks) of(name string) string {
	switch {
	case m.run == nil:
		return ""
	case m.now[name] && !m.run.Ended:
		return "now"
	case m.was[name]:
		return "done"
	}
	return "never"
}

// taken says whether a rule is the transition the latest step took: on an
// event it took, from a state the run was in, to one it is in now.
func (m chartMarks) taken(r rule) bool {
	return m.run != nil && slices.Contains(m.run.Events, r.event) && m.then[r.from] && m.now[r.to] && (r.to == r.from || !m.then[r.to])
}

// listed reads a list of names from what a type shows.
func listed(v any) []string {
	list, _ := v.([]any)
	var names []string
	for _, item := range list {
		if s, ok := item.(string); ok {
			names = append(names, s)
		}
	}
	return names
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// clipStart keeps the end of a text, where a path says most.
func clipStart(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return "…" + string(r[len(r)-n+1:])
	}
	return s
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
