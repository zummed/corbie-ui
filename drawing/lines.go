package drawing

import (
	"math"
	"slices"
	"strings"
)

// How the arrows of a statechart run between its states, where every arrow
// has a line of its own:
//
//   - An arrow to a state in a row below runs down from its state, along a
//     track in the gap above that row, and down into the state. The arrows
//     from one state share its line and its track, and divide from them.
//   - Tracks that would overlap are put one below another.
//   - An arrow to the state beside it in a row runs straight across, one line
//     each way; an arrow to a state further along runs on a track below the
//     row.
//   - Where a state inside a holder has states below it, an arrow leaving the
//     holder runs down a line of its own at the side of the holder. An arrow
//     into a state with states above it runs down the holder's right side,
//     clear of the holder's name.
//   - Each chip is on its own arrow, clear of other arrows, chips and cards.

// A pt is a point on the drawing.
type pt struct{ x, y float64 }

// A chanKey names a line down the side of a holder: its holder, the line,
// and the side, right or left.
type chanKey struct {
	holder, key string
	right       bool
}

// A sideway is a line down the side of a holder asked for: from a row to the
// holder's bottom, or from its top to a row.
type sideway struct {
	chanKey
	in  bool
	row int
}

// A trackKey names a track: the gap below a row of a level, and the line on
// it.
type trackKey struct {
	level string
	gap   int
	key   string
}

// toRight says b is right of a, or level with it, among the rows of a level.
func (ch *chart) toRight(level, a, b string) bool {
	pos := func(n string) float64 {
		return (float64(ch.col[n]) + 0.5) / float64(len(ch.rows[level][ch.row[n]]))
	}
	return pos(b) >= pos(a)
}

// plan settles how each arrow is drawn, the room each gap in a row needs for
// its chips, and the lines down the sides of holders.
func (ch *chart) plan() {
	pairs := map[[2]string][]int{}
	for i := range ch.list {
		a := &ch.list[i]
		a.kind, a.level, a.a, a.b = ch.kindOf(a.rules[0])
		id := a.from + "→" + a.to
		switch a.kind {
		case across:
			a.right = ch.col[a.b] > ch.col[a.a]
			k := [2]string{min(a.from, a.to), max(a.from, a.to)}
			pairs[k] = append(pairs[k], i)
			l, r := a.a, a.b
			if !a.right {
				l, r = r, l
			}
			ch.room[[2]string{l, r}] = math.Max(ch.room[[2]string{l, r}], chipWidth(a.label())+24)
		case down:
			a.right = ch.toRight(a.level, a.a, a.b)
			ch.side(a.from, a.level, "out "+a.from, a.right, false)
			ch.side(a.to, a.level, "in "+id, true, true)
		case lane:
			a.right = ch.col[a.b] > ch.col[a.a]
			ch.side(a.from, a.level, "out "+a.from, a.right, false)
			ch.side(a.to, a.level, "up "+id, !a.right, false)
		}
	}
	// Between two states, the arrow to the right runs above the arrow back.
	for _, list := range pairs {
		slices.SortStableFunc(list, func(i, j int) int {
			if ch.list[i].right == ch.list[j].right {
				return 0
			}
			if ch.list[i].right {
				return -1
			}
			return 1
		})
		for n, i := range list {
			ch.list[i].line, ch.list[i].of = n, len(list)
		}
	}
	// The lines down a holder's side: out from the states inside, those
	// leaving lower rows first, then those going into higher rows, so that
	// fewer cross.
	slices.SortStableFunc(ch.sideway, func(a, b sideway) int {
		switch {
		case a.in != b.in && !a.in:
			return -1
		case a.in != b.in:
			return 1
		case a.in:
			return a.row - b.row
		}
		return b.row - a.row
	})
	count := map[chanKey]int{}
	for _, s := range ch.sideway {
		side := chanKey{holder: s.holder, right: s.right}
		ch.chans[s.chanKey] = count[side]
		count[side]++
		ch.pad[s.holder] = math.Max(ch.pad[s.holder], chanGap*float64(count[side]))
	}
}

// side asks for a line down the side of each holder between a state and a
// level, where states are drawn between them: below the state, for a line
// out of it, or above it, for a line in.
func (ch *chart) side(s, level, key string, right, in bool) {
	for h := ch.parent[s]; h != level; h = ch.parent[h] {
		r := ch.row[ch.member(h, s)]
		if in && r == 0 || !in && r == len(ch.rows[h])-1 {
			continue
		}
		k := chanKey{h, key, right}
		if !slices.ContainsFunc(ch.sideway, func(w sideway) bool { return w.chanKey == k }) {
			ch.sideway = append(ch.sideway, sideway{k, in, r})
		}
	}
}

// chanX gives where a line down the side of a holder is.
func (ch *chart) chanX(k chanKey) float64 {
	b, out := ch.at[k.holder], 14+chanGap*float64(ch.chans[k])
	if k.right {
		return b.right() - inner - ch.pad[k.holder] + out
	}
	return b.x + inner + ch.pad[k.holder] - out
}

// trackY gives where a track is, and notes how far along it a line runs
// while the tracks are being counted.
func (ch *chart) trackY(level string, gap int, key string, xs ...float64) float64 {
	k := trackKey{level, gap, key}
	if ch.runs != nil {
		run, ok := ch.runs[k]
		if !ok {
			run = [2]float64{math.Inf(1), math.Inf(-1)}
		}
		for _, x := range xs {
			run = [2]float64{math.Min(run[0], x), math.Max(run[1], x)}
		}
		ch.runs[k] = run
	}
	return ch.origin[level].y + ch.gapY[level][gap] + trackGap*float64(ch.track[k])
}

// assign settles where each arrow enters its state, and gives each run along
// a gap a track, apart from the tracks of the runs it would overlap.
func (ch *chart) assign() {
	ch.entries()
	ch.runs = map[trackKey][2]float64{}
	for _, a := range ch.list {
		if a.kind != none {
			ch.route(a)
		}
	}
	gaps := map[trackKey][]trackKey{}
	for k, run := range ch.runs {
		if run[1]-run[0] > 0.5 {
			g := trackKey{level: k.level, gap: k.gap}
			gaps[g] = append(gaps[g], k)
		}
	}
	for g, keys := range gaps {
		// Shorter runs nearer the row, so that fewer lines cross.
		slices.SortFunc(keys, func(a, b trackKey) int {
			ra, rb := ch.runs[a], ch.runs[b]
			if c := cmpFloat(ra[1]-ra[0], rb[1]-rb[0]); c != 0 {
				return c
			}
			if c := cmpFloat(ra[0], rb[0]); c != 0 {
				return c
			}
			return strings.Compare(a.key, b.key)
		})
		var tracks [][][2]float64
		for _, k := range keys {
			run := ch.runs[k]
			j := 0
			for ; j < len(tracks); j++ {
				if !slices.ContainsFunc(tracks[j], func(o [2]float64) bool { return run[0] < o[1]+16 && o[0] < run[1]+16 }) {
					break
				}
			}
			if j == len(tracks) {
				tracks = append(tracks, nil)
			}
			tracks[j] = append(tracks[j], run)
			ch.track[k] = j
		}
		high := ch.high[g.level]
		for len(high) < len(ch.rows[g.level]) {
			high = append(high, 0)
		}
		high[g.gap] = len(tracks)
		ch.high[g.level] = high
	}
	ch.runs = nil
	ch.stubs()
}

// entries settles where each arrow from above enters its state: in the
// middle where it is the only one, and otherwise side by side in the order
// they come from, clear of the start's arrow and of lines that pass through.
// Where it can, the set moves so that one arrow goes straight down.
func (ch *chart) entries() {
	into := map[string][]int{}
	for i, a := range ch.list {
		if a.kind == down {
			into[a.to] = append(into[a.to], i)
		}
	}
	depth := func(n string) int {
		d := 0
		for ; n != ""; n = ch.parent[n] {
			d++
		}
		return d
	}
	targets := sortedKeys(into)
	// The states inside a holder first, so that its own arrows keep clear
	// of the lines that pass through its top.
	slices.SortStableFunc(targets, func(a, b string) int { return depth(b) - depth(a) })
	for _, t := range targets {
		b := ch.at[t]
		var avoid []float64
		if ch.start[ch.parent[t]] == t {
			avoid = append(avoid, b.cx())
		}
		for _, a := range ch.list {
			if a.kind == down && a.to != t && ch.inside(a.to, t) && ch.inside(t, a.level) {
				for _, p := range ch.enter(a, ch.entry[a.key()]) {
					if math.Abs(p.y-b.y) < 1 {
						avoid = append(avoid, p.x)
					}
				}
			}
		}
		list := into[t]
		from := map[int]float64{}
		for _, i := range list {
			from[i] = ch.approach(ch.list[i])
		}
		slices.SortStableFunc(list, func(i, j int) int { return cmpFloat(from[i], from[j]) })
		lo, hi := b.x+12, b.right()-12
		clear := func(xs []float64) bool {
			for _, x := range xs {
				if x < lo || x > hi || slices.ContainsFunc(avoid, func(v float64) bool { return math.Abs(v-x) < 10 }) {
					return false
				}
			}
			return true
		}
		// Far enough apart for a chip on a line straight down between two.
		apart := 20.0
		for _, i := range list {
			apart = math.Max(apart, chipWidth(ch.list[i].label())/2+8)
		}
		if len(list) > 1 {
			apart = math.Min(apart, (hi-lo)/float64(len(list)-1))
		}
		at := func(shift float64) []float64 {
			xs := make([]float64, len(list))
			for n := range list {
				xs[n] = b.cx() + (float64(n)-float64(len(list)-1)/2)*apart + shift
			}
			return xs
		}
		xs := at(0)
		for step := 1; !clear(xs) && step < 20; step++ {
			xs = at(float64((step+1)/2*10) * float64(1-2*(step%2)) * -1)
		}
		best := math.Inf(1)
		var straight []float64
		for n, i := range list {
			shift := from[i] - xs[n]
			if from[i] < lo || from[i] > hi || math.Abs(shift) >= math.Abs(best) {
				continue
			}
			moved := make([]float64, len(xs))
			for k := range xs {
				moved[k] = xs[k] + shift
			}
			if clear(moved) {
				best, straight = shift, moved
			}
		}
		if straight != nil {
			xs = straight
		}
		for n, i := range list {
			ch.entry[ch.list[i].key()] = xs[n]
		}
	}
}

// approach gives the x an arrow comes from as it enters its state: the line
// down the side of a holder it enters, or the line it leaves its own state
// by.
func (ch *chart) approach(a arrow) float64 {
	for h := ch.parent[a.to]; h != a.level; h = ch.parent[h] {
		if k := (chanKey{h, "in " + a.from + "→" + a.to, true}); ch.has(k) {
			return ch.chanX(k)
		}
	}
	out := ch.exit(a.from, a.level, ch.portX(a.from), "out "+a.from, a.from, a.right)
	return out[len(out)-1].x
}

// stubs settles where each arrow along a row enters its state from below:
// beside the middle, on the side it comes from, those on lower tracks
// nearer the middle, so that they do not cross.
func (ch *chart) stubs() {
	sides := map[[2]string][]int{}
	for i, a := range ch.list {
		if a.kind == lane {
			side := "left"
			if !a.right {
				side = "right"
			}
			sides[[2]string{a.to, side}] = append(sides[[2]string{a.to, side}], i)
		}
	}
	for k, list := range sides {
		depth := func(i int) int {
			a := ch.list[i]
			return ch.track[trackKey{a.level, ch.row[a.a], "lane " + a.from + "→" + a.to}]
		}
		slices.SortStableFunc(list, func(i, j int) int { return depth(j) - depth(i) })
		sign := -1.0
		if k[1] == "right" {
			sign = 1
		}
		for n, i := range list {
			ch.stub[ch.list[i].key()] = ch.portX(k[0]) + sign*16*float64(n+1)
		}
	}
}

func (ch *chart) has(k chanKey) bool {
	_, ok := ch.chans[k]
	return ok
}

// inside says a state is inside a holder, at any depth; every state is
// inside the top, "".
func (ch *chart) inside(s, holder string) bool {
	for s != "" {
		s = ch.parent[s]
		if s == holder {
			return true
		}
	}
	return false
}

// portX gives where the arrows from a state leave its bottom: its middle,
// or for a holder, the nearest place clear of the lines from the states
// inside it that leave through its bottom.
func (ch *chart) portX(s string) float64 {
	b := ch.at[s]
	if len(ch.kids[s]) == 0 {
		return b.cx()
	}
	var taken []float64
	for _, a := range ch.list {
		if (a.kind != down && a.kind != lane) || !ch.inside(a.from, s) || !ch.inside(s, a.level) || ch.has(chanKey{s, "out " + a.from, a.right}) {
			continue
		}
		out := ch.exit(a.from, s, ch.portX(a.from), "out "+a.from, a.from, a.right)
		taken = append(taken, out[len(out)-1].x)
	}
	x := b.cx()
	for step := 1; slices.ContainsFunc(taken, func(t float64) bool { return math.Abs(t-x) < 16 }); step++ {
		x = b.cx() + float64((step+1)/2*16)*float64(1-2*(step%2))*-1
	}
	return x
}

// route gives the points an arrow runs through, from the state it leaves to
// the state it goes to.
func (ch *chart) route(a arrow) []pt {
	id := a.from + "→" + a.to
	switch a.kind {
	case across:
		return ch.beside(a)
	case lane:
		out := ch.exit(a.from, a.level, ch.portX(a.from), "out "+a.from, a.from, a.right)
		x, ok := ch.stub[a.key()]
		if !ok {
			x = ch.at[a.to].cx()
		}
		up := ch.exit(a.to, a.level, x, "up "+id, "up "+id, !a.right)
		p, q := out[len(out)-1], up[len(up)-1]
		y := ch.trackY(a.level, ch.row[a.a], "lane "+id, p.x, q.x)
		pts := append(out, pt{p.x, y}, pt{q.x, y})
		for i := len(up) - 1; i >= 0; i-- {
			pts = append(pts, up[i])
		}
		return pts
	}
	out := ch.exit(a.from, a.level, ch.portX(a.from), "out "+a.from, a.from, a.right)
	x, ok := ch.entry[a.key()]
	if !ok {
		x = ch.at[a.to].cx()
	}
	in := ch.enter(a, x)
	p, q := out[len(out)-1], in[0]
	y := ch.trackY(a.level, ch.row[a.a], a.from, p.x, q.x)
	return append(append(out, pt{p.x, y}, pt{q.x, y}), in...)
}

// exit gives the line down from a state at x to the bottom of the state of a
// level it is in: straight down, or where states are drawn below it in a
// holder, along a track to the side of the holder and down it.
func (ch *chart) exit(s, level string, x float64, key, track string, right bool) []pt {
	pts := []pt{{x, ch.at[s].bottom()}}
	for h := ch.parent[s]; h != level; h = ch.parent[h] {
		hb, last := ch.at[h], pts[len(pts)-1]
		k := chanKey{h, key, right}
		if !ch.has(k) {
			pts = append(pts, pt{last.x, hb.bottom()})
			continue
		}
		cx := ch.chanX(k)
		y := ch.trackY(h, ch.row[ch.member(h, s)], track, last.x, cx)
		pts = append(pts, pt{last.x, y}, pt{cx, y}, pt{cx, hb.bottom()})
	}
	return pts
}

// enter gives the line from the top of the state of the arrow's level that
// its state is in, down to its state at x: straight down, or where states
// are drawn above it in a holder, down the side of the holder and along a
// track to it.
func (ch *chart) enter(a arrow, x float64) []pt {
	key := "in " + a.from + "→" + a.to
	pts := []pt{{x, ch.at[a.to].y}}
	for h := ch.parent[a.to]; h != a.level; h = ch.parent[h] {
		hb, first := ch.at[h], pts[0]
		k := chanKey{h, key, true}
		if !ch.has(k) {
			pts = append([]pt{{first.x, hb.y}}, pts...)
			continue
		}
		cx := ch.chanX(k)
		y := ch.trackY(h, ch.row[ch.member(h, a.to)]-1, key, first.x, cx)
		pts = append([]pt{{cx, hb.y}, {cx, y}, {first.x, y}}, pts...)
	}
	return pts
}

// beside gives the line from a state to the state beside it: straight
// across, each line between the two apart from the others, or where the
// two are not level, across, down the gap between them, and across.
func (ch *chart) beside(a arrow) []pt {
	s, t := ch.at[a.from], ch.at[a.to]
	off := (float64(a.line) - float64(a.of-1)/2) * trackGap
	x1, x2 := s.right(), t.x
	gx := (ch.at[a.a].right() + ch.at[a.b].x) / 2
	if !a.right {
		x1, x2 = s.x, t.right()
		gx = (ch.at[a.b].right() + ch.at[a.a].x) / 2
	}
	lo, hi := math.Max(s.y, t.y), math.Min(s.bottom(), t.bottom())
	if y := (lo+hi)/2 + off; y > lo+8 && y < hi-8 {
		return []pt{{x1, y}, {x2, y}}
	}
	y1, y2 := s.cy()+off, t.cy()+off
	return []pt{{x1, y1}, {gx + off/2, y1}, {gx + off/2, y2}, {x2, y2}}
}

// chips gives where each arrow's chip goes: on the arrow, as near the middle
// of its last run across as is clear of cards, other arrows and chips placed
// before it; on its last run down where no run across is clear.
func (ch *chart) chips(routes [][]pt) []pt {
	var cards []box
	var walls [][2]pt
	for _, n := range sortedKeys(ch.at) {
		b := ch.at[n]
		if len(ch.kids[n]) == 0 {
			cards = append(cards, b)
			continue
		}
		head := 28 + nameW*float64(len([]rune(n)))
		for _, note := range ch.items[n].notes {
			head = math.Max(head, 28+labelWidth(note.text))
		}
		cards = append(cards, box{x: b.x, y: b.y, w: head, h: ch.top(n) - 8})
		walls = append(walls, [2]pt{{b.x, b.y}, {b.right(), b.y}}, [2]pt{{b.x, b.bottom()}, {b.right(), b.bottom()}},
			[2]pt{{b.x, b.y}, {b.x, b.bottom()}}, [2]pt{{b.right(), b.y}, {b.right(), b.bottom()}})
	}
	for level, s := range ch.start {
		if b, ok := ch.at[s]; ok && slices.Contains(ch.kids[level], s) {
			cards = append(cards, box{x: b.cx() - 5, y: b.y - 29, w: 10, h: 29})
		}
	}
	var lines [][2]pt
	var owner []int
	for i, route := range routes {
		for k := 1; k < len(route); k++ {
			lines, owner = append(lines, [2]pt{route[k-1], route[k]}), append(owner, i)
		}
	}
	at := make([]pt, len(routes))
	var placed []box
	for i, route := range routes {
		if route == nil {
			continue
		}
		w := chipWidth(ch.list[i].label())
		// What a chip on this arrow could meet: what is near the arrow.
		near := box{x: math.Inf(1), y: math.Inf(1)}
		far := pt{math.Inf(-1), math.Inf(-1)}
		for _, p := range route {
			near.x, near.y, far.x, far.y = math.Min(near.x, p.x), math.Min(near.y, p.y), math.Max(far.x, p.x), math.Max(far.y, p.y)
		}
		near = box{x: near.x - w - 4, y: near.y - 24, w: far.x - near.x + 2*w + 8, h: far.y - near.y + 48}
		var boxes []box
		for _, b := range append(cards, placed...) {
			if meets(near, b) {
				boxes = append(boxes, b)
			}
		}
		var others [][2]pt
		for k, l := range append(lines, walls...) {
			if (k >= len(owner) || owner[k] != i) && math.Max(l[0].x, l[1].x) >= near.x && math.Min(l[0].x, l[1].x) <= near.right() &&
				math.Max(l[0].y, l[1].y) >= near.y && math.Min(l[0].y, l[1].y) <= near.bottom() {
				others = append(others, l)
			}
		}
		free := func(c pt) bool {
			r := box{x: c.x - w/2 - 2, y: c.y - 11, w: w + 4, h: 22}
			return !slices.ContainsFunc(boxes, func(b box) bool { return meets(r, b) }) &&
				!slices.ContainsFunc(others, func(l [2]pt) bool { return crosses(r, l[0], l[1]) })
		}
		var spotsOf []pt
		for _, across := range []bool{true, false} {
			for k := len(route) - 1; k > 0; k-- {
				if p, q := route[k-1], route[k]; (p.y == q.y) == across {
					spotsOf = append(spotsOf, spots(p, q, w, k == len(route)-1)...)
				}
			}
		}
		if len(spotsOf) == 0 {
			p, q := route[len(route)-2], route[len(route)-1]
			spotsOf = []pt{{(p.x + q.x) / 2, (p.y + q.y) / 2}}
		}
		at[i] = spotsOf[0]
		if k := slices.IndexFunc(spotsOf, free); k >= 0 {
			at[i] = spotsOf[k]
		}
		placed = append(placed, box{x: at[i].x - w/2, y: at[i].y - 9, w: w, h: 18})
	}
	return at
}

// spots gives the places along a line from p to q where a chip w wide can
// go, from its middle out; on the last run down of an arrow, from just
// above the state it enters, up.
func spots(p, q pt, w float64, last bool) []pt {
	var out []pt
	if p.y == q.y {
		lo, hi := math.Min(p.x, q.x)+w/2+10, math.Max(p.x, q.x)-w/2-10
		if lo > hi {
			return nil
		}
		mid := (lo + hi) / 2
		for d := 0.0; mid-d >= lo || mid+d <= hi; d += 4 {
			if mid+d <= hi {
				out = append(out, pt{mid + d, p.y})
			}
			if d > 0 && mid-d >= lo {
				out = append(out, pt{mid - d, p.y})
			}
		}
		return out
	}
	lo, hi := math.Min(p.y, q.y)+20, math.Max(p.y, q.y)-14
	if lo > hi {
		return nil
	}
	if last && q.y > p.y {
		for y := hi; y >= lo; y -= 4 {
			out = append(out, pt{p.x, y})
		}
		return out
	}
	mid := (lo + hi) / 2
	for d := 0.0; mid-d >= lo || mid+d <= hi; d += 4 {
		if mid+d <= hi {
			out = append(out, pt{p.x, mid + d})
		}
		if d > 0 && mid-d >= lo {
			out = append(out, pt{p.x, mid - d})
		}
	}
	return out
}

// meets says two boxes overlap.
func meets(a, b box) bool {
	return a.x < b.right() && b.x < a.right() && a.y < b.bottom() && b.y < a.bottom()
}

// crosses says a line from p to q, across or down, passes through a box.
func crosses(b box, p, q pt) bool {
	if p.y == q.y {
		return p.y > b.y && p.y < b.bottom() && math.Max(p.x, q.x) > b.x && math.Min(p.x, q.x) < b.right()
	}
	return p.x > b.x && p.x < b.right() && math.Max(p.y, q.y) > b.y && math.Min(p.y, q.y) < b.bottom()
}

// simplify drops the points of a line that are the same as the one before
// or on the way from it to the next.
func simplify(pts []pt) []pt {
	var out []pt
	for _, p := range pts {
		if n := len(out); n > 0 && math.Abs(out[n-1].x-p.x) < 0.01 && math.Abs(out[n-1].y-p.y) < 0.01 {
			continue
		}
		if n := len(out); n > 1 {
			a, b := out[n-2], out[n-1]
			if (math.Abs(a.x-b.x) < 0.01 && math.Abs(b.x-p.x) < 0.01) || (math.Abs(a.y-b.y) < 0.01 && math.Abs(b.y-p.y) < 0.01) {
				out[n-1] = p
				continue
			}
		}
		out = append(out, p)
	}
	return out
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// chipWidth is how wide a chip is for a label.
func chipWidth(label string) float64 { return labelWidth(clip(label, longestChip)) + 14 }
