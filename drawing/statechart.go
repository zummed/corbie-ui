package drawing

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/zummed/corbie/machine"
	"github.com/zummed/corbie/machine/statechart"
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
	walker *walker

	rows    map[string][][]string // each level's states in rows, top to bottom, each left to right
	row     map[string]int        // the row of each state among its level's
	col     map[string]int        // its place in its row
	list    []arrow
	room    map[[2]string]float64   // the gap between two states side by side, for the chips on the arrows between them
	sideway []sideway               // the lines down the side of a holder, as asked for
	chans   map[chanKey]int         // each line down the side of a holder, by its place out from the states inside
	pad     map[string]float64      // the room each side of a holder for its lines
	gapY    map[string][]float64    // the first track below each row of a level, from the level's top
	high    map[string][]int        // how many tracks there are below each row of a level
	runs    map[trackKey][2]float64 // how far along each track its line runs, while the tracks are counted
	track   map[trackKey]int        // the track each line runs on, 0 nearest the row
	entry   map[[2]string]float64   // where each arrow enters the state it goes to, from above
	stub    map[[2]string]float64   // where each arrow along a row enters the state it goes to, from below
	origin  map[string]pt           // where each level's rows begin on the drawing
}

// The room inside a state that holds states: its name above, and a margin.
const (
	inner    = 16.0
	innerTop = 38.0
	// trackGap is how far apart two lines are that run side by side.
	trackGap = 22.0
	// chanGap is how far apart the lines down the side of a holder are.
	chanGap = 12.0
)

// A rule is one transition: from a state, on an event, to a state.
type rule struct {
	from, event, to, when, timer string
	send                         bool
}

// How a transition is drawn, by where its states are drawn: in its card, as
// no arrow; across to the state beside it; below the row, to a state further
// along it; or down, to a state in a row below.
const (
	none = iota // a state not in the chart, not drawn
	back
	across
	lane
	down
)

func drawChart(c *statechart.Chart, run *Run, w *walker) drawn {
	ch := &chart{states: map[string]*statechart.State{}, parent: map[string]string{}, kids: map[string][]string{},
		start: map[string]string{"": c.Initial}, at: map[string]box{}, items: map[string]item{}, noted: map[rule]bool{}, walker: w,
		rows: map[string][][]string{}, row: map[string]int{}, col: map[string]int{}, room: map[[2]string]float64{},
		chans: map[chanKey]int{}, pad: map[string]float64{}, gapY: map[string][]float64{}, high: map[string][]int{},
		track: map[trackKey]int{}, entry: map[[2]string]float64{}, stub: map[[2]string]float64{}, origin: map[string]pt{}}
	ch.index("", c.States)
	ch.all = ch.ruleList()
	ch.order("")
	marks := ch.marks(run)
	ch.write(marks)
	ch.list = ch.arrows()
	ch.plan()
	// Where lines run across and how many tracks each gap needs follow from
	// where the states are across, so the states are placed twice: the second
	// time with room for the tracks.
	ch.lay("")
	ch.place("", margin, margin+30)
	ch.assign()
	ch.lay("")
	ch.place("", margin, margin+30)

	routes := make([][]pt, len(ch.list))
	for i, a := range ch.list {
		if a.kind != none {
			routes[i] = simplify(ch.route(a))
		}
	}
	var d drawn
	chips := ch.chips(routes)
	// A transition just taken is drawn last, over any arrow it shares a
	// line with.
	order := make([]int, len(ch.list))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(i, j int) int {
		a, b := ch.list[i].taken(marks), ch.list[j].taken(marks)
		switch {
		case a == b:
			return 0
		case a:
			return 1
		}
		return -1
	})
	for _, i := range order {
		if routes[i] == nil {
			continue
		}
		a := ch.list[i]
		class := a.class(marks)
		d.line(routes[i], class)
		d.chip(chips[i].x, chips[i].y, a.label(), class)
	}
	ch.paint(&d, "", marks)
	return d
}

// An arrow is every rule from one state to another, and how it is drawn: its
// kind, the level its states are drawn apart at, and the states of that level
// they are in or are.
type arrow struct {
	from, to string
	rules    []rule
	kind     int
	level    string
	a, b     string
	right    bool // the state it goes to is right of the one it leaves, or below it
	line, of int  // across: which of the lines between its two states it is, of how many
}

func (a arrow) key() [2]string { return [2]string{a.from, a.to} }

// taken says a rule of the arrow is one the latest step took.
func (a arrow) taken(marks chartMarks) bool {
	return slices.ContainsFunc(a.rules, func(r rule) bool { return marks.taken[r] })
}

// class gives how an arrow is drawn: dashed for a timer or a send, and in
// petrol where the latest step took it.
func (a arrow) class(marks chartMarks) string {
	var classes []string
	for _, r := range a.rules {
		if r.timer != "" && !slices.Contains(classes, "timer") {
			classes = append(classes, "timer")
		}
		if r.send {
			classes = append(classes, "send")
		}
		if marks.taken[r] && !slices.Contains(classes, "taken") {
			classes = append(classes, "taken")
		}
	}
	return strings.Join(classes, " ")
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
// or to a state drawn in a row above.
func (ch *chart) write(marks chartMarks) {
	var backs []arrow
	at := map[[2]string]int{}
	for _, r := range ch.all {
		if kind, _, _, _ := ch.kindOf(r); r.send || kind != back {
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
		it.notes = append(it.notes, note{text: clip(text, longest+6), taken: slices.ContainsFunc(b.rules, func(r rule) bool { return marks.taken[r] })})
		ch.items[b.from] = it
	}
}

// kindOf gives how a rule is drawn, by the rows of the level its two states
// are drawn apart at: the level, and the states of it they are in or are.
// A rule from a state to one it holds, or to one that holds it, goes back.
func (ch *chart) kindOf(r rule) (int, string, string, string) {
	if _, ok := ch.states[r.to]; !ok {
		return none, "", "", ""
	}
	if r.from == r.to {
		return back, "", "", ""
	}
	for level := ch.parent[r.from]; ; level = ch.parent[level] {
		a, b := ch.member(level, r.from), ch.member(level, r.to)
		if a == "" || b == "" {
			if level == "" {
				return none, "", "", ""
			}
			continue
		}
		switch {
		case a == b || ch.row[b] < ch.row[a]:
			return back, level, a, b
		case ch.row[b] > ch.row[a]:
			return down, level, a, b
		case ch.col[b]-ch.col[a] == 1 || ch.col[a]-ch.col[b] == 1:
			return across, level, a, b
		}
		return lane, level, a, b
	}
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

// sent lays out each send to an address as a card below the state that
// sends it, named by the address.
func (ch *chart) sent(holder, name, event string, sends []statechart.Send) {
	for _, send := range sends {
		if send.Address == nil {
			continue
		}
		key := fmt.Sprintf("%s.%s.%d", name, event, len(ch.sends))
		ch.states[key] = &statechart.State{}
		ch.parent[key] = holder
		ch.kids[holder] = append(ch.kids[holder], key)
		ch.items[key] = item{name: Address(send.Address), icon: "statechart", lines: []string{"send " + send.Event}}
		ch.sends = append(ch.sends, rule{from: name, event: event, to: key, send: true})
	}
}

// Address writes an address as the command line does: the location, with
// #key or a run's ID after it, or the one expression that gives it.
func Address(a *machine.AddressSpec) string {
	switch {
	case a.Expr != "":
		return a.Expr
	case a.Key != "":
		return a.At + "#" + a.Key
	case a.Run != "":
		return a.At + " run " + a.Run
	}
	return a.At
}

// itemOf gives what a state's box says.
func (ch *chart) itemOf(name string, s *statechart.State) item {
	it := item{name: name}
	if s == nil {
		return it
	}
	it.narrow = final(s)
	ch.walker.describe(&it, name, s.Job)
	if len(s.Jobs) > 0 {
		it.icon = "script"
		it.lines = append(it.lines, fit("jobs: "+strings.Join(sortedKeys(s.Jobs), ", ")))
	}
	it.lines = append(it.lines, tags(s)...)
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

func final(s *statechart.State) bool { return s != nil && s.End != "" }

// order puts the states of a level, and of each level inside it, in rows by
// how many transitions each is from the state the level starts in.
func (ch *chart) order(level string) {
	members := ch.kids[level]
	for _, m := range members {
		if len(ch.kids[m]) > 0 {
			ch.order(m)
		}
	}
	if len(members) == 0 {
		return
	}
	next := map[string][]string{}
	for _, r := range ch.all {
		a, b := ch.member(level, r.from), ch.member(level, r.to)
		if a != "" && b != "" && a != b && !slices.Contains(next[a], b) {
			next[a] = append(next[a], b)
		}
	}
	var rows [][]string
	seen := map[string]bool{}
	visit := func(first string, base int) {
		seen[first] = true
		at := map[string]int{first: base}
		queue := []string{first}
		for len(queue) > 0 {
			n := queue[0]
			queue = queue[1:]
			for len(rows) <= at[n] {
				rows = append(rows, nil)
			}
			rows[at[n]] = append(rows[at[n]], n)
			for _, m := range next[n] {
				if !seen[m] {
					seen[m] = true
					at[m] = at[n] + 1
					queue = append(queue, m)
				}
			}
		}
	}
	start := ch.start[level]
	if !slices.Contains(members, start) {
		start = members[0]
	}
	visit(start, 0)
	for _, m := range members {
		if !seen[m] {
			visit(m, len(rows))
		}
	}
	for r := range rows {
		rows[r] = arrange(rows[r], members, next)
		for i, n := range rows[r] {
			ch.row[n], ch.col[n] = r, i
		}
	}
	ch.rows[level] = rows
}

// arrange orders a row so that its states joined by transitions are side by
// side: first as few transitions as can be to states not beside, then as few
// lines below the row at any place, then as few states passed, then as few
// transitions to the left between states side by side, so that a chain reads
// left to right. Of orders as good, the one nearest the order of the level's
// states, by name, is taken. A row with no transitions in it stays in the
// order found.
func arrange(found, members []string, next map[string][]string) []string {
	row := slices.Clone(found)
	slices.SortStableFunc(row, func(a, b string) int { return slices.Index(members, a) - slices.Index(members, b) })
	var pairs [][2]int
	for i, a := range row {
		for j, b := range row {
			if i != j && slices.Contains(next[a], b) {
				pairs = append(pairs, [2]int{i, j})
			}
		}
	}
	n := len(row)
	if len(pairs) == 0 {
		return found
	}
	cost := func(seq []int) [4]int {
		at := make([]int, n)
		for p, i := range seq {
			at[i] = p
		}
		cover := make([]int, n)
		var c [4]int
		for _, pr := range pairs {
			lo, hi := min(at[pr[0]], at[pr[1]]), max(at[pr[0]], at[pr[1]])
			if hi-lo < 2 {
				if at[pr[1]] < at[pr[0]] {
					c[3]++
				}
				continue
			}
			c[0]++
			c[2] += hi - lo - 1
			for k := lo; k <= hi; k++ {
				cover[k]++
				c[1] = max(c[1], cover[k])
			}
		}
		return c
	}
	best := make([]int, n)
	for i := range best {
		best[i] = i
	}
	least := cost(best)
	if n <= 7 {
		// Every order, in order from the one found.
		seq, used := make([]int, 0, n), make([]bool, n)
		var try func()
		try = func() {
			if len(seq) == n {
				if c := cost(seq); slices.Compare(c[:], least[:]) < 0 {
					least = c
					copy(best, seq)
				}
				return
			}
			for i := range n {
				if !used[i] {
					used[i] = true
					seq = append(seq, i)
					try()
					seq = seq[:len(seq)-1]
					used[i] = false
				}
			}
		}
		try()
	} else {
		// Two states side by side change places while that makes it better.
		for round, better := 0, true; better && round < n; round++ {
			better = false
			for p := 0; p+1 < n; p++ {
				best[p], best[p+1] = best[p+1], best[p]
				if c := cost(best); slices.Compare(c[:], least[:]) < 0 {
					least, better = c, true
				} else {
					best[p], best[p+1] = best[p+1], best[p]
				}
			}
		}
	}
	out := make([]string, n)
	for p, i := range best {
		out[p] = row[i]
	}
	return out
}

// lay places the states of a level in its rows, each relative to the level,
// and gives the level's size. The gaps in a row are wide enough for the chips
// between its states, and the gap below a row has room for its tracks.
func (ch *chart) lay(level string) (float64, float64) {
	rows := ch.rows[level]
	if len(rows) == 0 {
		return 0, 0
	}
	size := map[string]box{}
	for _, c := range rows {
		for _, m := range c {
			s := ch.states[m]
			switch {
			case len(ch.kids[m]) > 0:
				w, h := ch.lay(m)
				size[m] = box{w: math.Max(w, cardW) + 2*(inner+ch.pad[m]), h: h + ch.top(m) + inner}
			case final(s):
				b := ch.items[m].size()
				size[m] = box{w: b.w + 8, h: b.h + 8}
			default:
				size[m] = ch.items[m].size()
			}
		}
	}
	gap := func(c []string, i int) float64 { return math.Max(sideGap, ch.room[[2]string{c[i], c[i+1]}]) }
	across := func(c []string) float64 {
		used := 0.0
		for i, n := range c {
			used += size[n].w
			if i < len(c)-1 {
				used += gap(c, i)
			}
		}
		return used
	}
	total := 0.0
	for _, c := range rows {
		total = math.Max(total, across(c))
	}
	ch.gapY[level] = make([]float64, len(rows))
	y, height := 0.0, 0.0
	for r, c := range rows {
		high := 0.0
		x := (total - across(c)) / 2
		for i, n := range c {
			b := size[n]
			b.x, b.y = x, y
			ch.at[n] = b
			if i < len(c)-1 {
				x += b.w + gap(c, i)
			}
			high = math.Max(high, b.h)
		}
		tracks := 0
		if r < len(ch.high[level]) {
			tracks = ch.high[level][r]
		}
		ch.gapY[level][r] = y + high + rowGap
		height = y + high
		if tracks > 0 {
			height = y + high + rowGap + trackGap*float64(tracks-1) + rowGap/2
		}
		y += high + 2*rowGap + trackGap*float64(max(tracks-1, 0))
	}
	return total, height
}

// top gives the room above the states a state holds: its name and its notes.
func (ch *chart) top(name string) float64 {
	return innerTop + float64(len(ch.items[name].notes))*noteH
}

// place turns where each state of a level is drawn into where it is on the
// drawing, the level being at ox, oy.
func (ch *chart) place(level string, ox, oy float64) {
	ch.origin[level] = pt{ox, oy}
	for _, n := range ch.kids[level] {
		b := ch.at[n]
		b.x, b.y = b.x+ox, b.y+oy
		ch.at[n] = b
		if len(ch.kids[n]) > 0 {
			ch.place(n, b.x+inner+ch.pad[n], b.y+ch.top(n))
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
			if s.End == "failed" && marks.of(n) != "failed" {
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
			outer := fmt.Sprintf(`<rect class="outer" x="%s" y="%s" width="%s" height="%s" rx="%s"/>`, num(b.x), num(b.y), num(b.w), num(b.h), num(cardR))
			d.paint(ch.items[n], inside, class, cardR-4, outer)
		default:
			d.paint(ch.items[n], b, class, cardR, "")
		}
		if ch.start[level] == n {
			fmt.Fprintf(&d.shapes, `<circle class="start" cx="%s" cy="%s" r="4"/><path class="edge" d="M%s %s V%s" marker-end="url(#ah)"/>`,
				num(b.cx()), num(b.y-24), num(b.cx()), num(b.y-20), num(b.y))
		}
		ch.paint(d, n, marks)
	}
}

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

// chartMarks are a run's states, as the drawing marks them: a state the run
// is in whose job is held for the ask is marked held.
type chartMarks struct {
	run      *Run
	now, was map[string]bool // in now, and in at any step
	taken    map[rule]bool   // the rules the latest step took
}

// marks follows a run through its steps: from the states it was in before
// each step, by the events the step took, to the states it was in after.
// A state passed through within one step is one it was in.
func (ch *chart) marks(run *Run) chartMarks {
	m := chartMarks{run: run, now: map[string]bool{}, was: map[string]bool{}, taken: map[rule]bool{}}
	if run == nil || len(run.Steps) == 0 {
		return m
	}
	in := ch.with(ch.entering(ch.start[""])...)
	maps.Copy(m.was, in)
	for _, step := range run.Steps {
		after := ch.with(step.Shown.States...)
		m.taken = map[rule]bool{}
		for _, r := range ch.took(in, step.Events, after) {
			m.taken[r] = true
			maps.Copy(m.was, ch.with(ch.entering(r.to)...))
		}
		maps.Copy(m.was, after)
		in = after
	}
	m.now = in
	return m
}

// took gives the rules a step took, an event at a time, from the states the
// run was in before it. Where an event has rules to several states, the one
// to a state the run is in after the step is the one taken; a rule with a
// condition that leads to none of those is not known to be taken.
func (ch *chart) took(in map[string]bool, events []string, after map[string]bool) []rule {
	var taken []rule
	for _, e := range events {
		var found []rule
		for _, r := range ch.all {
			if !r.send && r.event == e && in[r.from] {
				found = append(found, r)
			}
		}
		i := slices.IndexFunc(found, func(r rule) bool { return after[r.to] })
		if i < 0 && len(found) == 1 && found[0].when == "" {
			i = 0
		}
		if i < 0 {
			continue
		}
		taken = append(taken, found[i])
		in = ch.with(ch.entering(found[i].to)...)
	}
	return taken
}

// with gives states with every state that holds them.
func (ch *chart) with(names ...string) map[string]bool {
	in := map[string]bool{}
	for _, n := range names {
		for ; n != "" && !in[n]; n = ch.parent[n] {
			in[n] = true
		}
	}
	return in
}

// entering gives a state with the state it starts in, and the state that
// one starts in, down to a state that holds none.
func (ch *chart) entering(name string) []string {
	var names []string
	for ; name != "" && !slices.Contains(names, name); name = ch.start[name] {
		names = append(names, name)
	}
	return names
}

func (m chartMarks) of(name string) string {
	switch {
	case m.run == nil:
		return ""
	case m.now[name] && !m.run.Ended && slices.Contains(m.run.Held, name):
		return "held"
	case m.now[name] && !m.run.Ended:
		return "now"
	case m.now[name] && m.run.Failed:
		return "failed"
	case m.was[name]:
		return "done"
	}
	return "never"
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
