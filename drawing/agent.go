package drawing

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/zummed/corbie/agent"
	"github.com/zummed/corbie/machine"
)

// A partBox is one column of what the model can call: a part, or the tools
// in no part, with where its tools and its ask are drawn. file is the file
// the part is included from, where it is known.
type partBox struct {
	name, file string
	part       bool
	folded     bool
	tools      []string
	items      map[string]item
	ask        *item
	at         box
}

// askAt is where a part's ask is written, as a run names it.
func askAt(part string) string { return "open." + part + ".ask" }

// foldedH is how high a folded part is: its name and how many tools it has.
const foldedH = 52.0

func (g *partBox) size() {
	g.at.h, g.at.w = 0, cardW
	if g.folded {
		g.at.h = foldedH
		return
	}
	if g.part {
		g.at.h, g.at.w = innerTop+inner, cardW+2*inner
	}
	if g.ask != nil {
		g.at.h += g.ask.size().h + 12
		g.at.w = math.Max(g.at.w, g.ask.size().w)
	}
	for _, t := range g.tools {
		b := g.items[t].size()
		g.at.h += b.h + 12
		g.at.w = math.Max(g.at.w, b.w)
	}
	g.at.h -= 12
}

// drawAgent draws three things, one below another: the model, what it can
// call, and its reply.
func drawAgent(agentName string, a *agent.Agent, ask *machine.JobSpec, run *Run, w *walker) drawn {
	name := a.Model.Name
	if name == "" {
		name = "model"
	}
	columns := columnsOf(groupsOf(agentName, a, w), run)
	var askItem *item
	if ask != nil {
		it := item{name: "ask"}
		w.describe(&it, "ask", ask)
		askItem = &it
	}

	across, high := float64(max(len(columns)-1, 0))*sideGap, 0.0
	for _, c := range columns {
		w, h := c.size()
		across += w
		high = math.Max(high, h)
	}
	if askItem != nil {
		across = math.Max(across, askItem.size().w)
		if high > 0 {
			high += 12
		}
		high += askItem.size().h
	}
	has := high > 0
	tools := box{w: math.Max(across+2*inner, cardW), h: innerTop + high + inner}
	total := cardW
	if has {
		total = tools.w
	}
	left := margin
	model := item{icon: "model", name: name}
	top := model.size()
	top.x, top.y = left+(total-top.w)/2, margin+18
	reply := item{name: "reply", narrow: true}.size()
	reply.x = left + (total-reply.w)/2

	waiting, _ := shown(run.latest(), "waiting").(string)
	var d drawn
	y := top.bottom()
	class := ""
	if has {
		tools.x, tools.y = left, y+2*rowGap
		fmt.Fprintf(&d.holders, `<g class="state holder"><rect x="%s" y="%s" width="%s" height="%s" rx="14"/><text class="name" x="%s" y="%s">tools</text></g>`,
			num(tools.x), num(tools.y), num(tools.w), num(tools.h), num(tools.x+14), num(tools.y+22))
		d.reach(tools)
		x, ty := tools.x+inner+(tools.w-2*inner-across)/2, tools.y+innerTop
		for _, c := range columns {
			y, w := ty, 0.0
			for _, g := range c {
				g.at.x, g.at.y = x, y
				y += g.at.h + 12
				w = math.Max(w, g.at.w)
				if d.paintGroup(g, run) {
					class = "taken"
				}
			}
			x += w + sideGap
		}
		if askItem != nil {
			b := askItem.size()
			b.x, b.y = tools.cx()-b.w/2, tools.bottom()-inner-b.h
			d.paint(*askItem, b, strings.TrimSpace("state ask "+jobMark(run, "ask")), cardR, "")
		}
		turns := a.Turns
		if turns == 0 {
			turns = 20
		}
		d.down(top, tools, class, fmt.Sprintf("%d turns", turns))
		y = tools.bottom()
	}
	reply.y = y + 2*rowGap/1.5
	if has {
		d.down(tools, reply, "", "")
	} else {
		d.down(top, reply, "", "")
	}

	mark := ""
	switch {
	case run == nil:
	case run.failedAt("turn"):
		mark = "failed"
	case waiting == "turn" && !run.Ended:
		mark = "now"
	default:
		mark = "done"
	}
	d.paint(model, top, strings.TrimSpace("state model "+mark), cardR, "")
	if run != nil {
		if turn, ok := shown(run.latest(), "turn").(float64); ok {
			fmt.Fprintf(&d.shapes, `<text class="tag" x="%s" y="%s" text-anchor="start">turn %d</text>`, num(top.x+4), num(top.y-8), int(turn))
		}
	}
	d.paint(item{name: "reply"}, reply, "state reply", cardR, "")
	return d
}

// groupsOf gives each part of an agent, then the tools in no part.
func groupsOf(agentName string, a *agent.Agent, w *walker) []*partBox {
	var groups []*partBox
	byName := map[string]*partBox{}
	for _, part := range sortedKeys(a.Parts) {
		g := &partBox{name: part, file: w.part(agentName, part, a.Parts[part].Does), part: true, items: map[string]item{}}
		if ask := a.Parts[part].Ask; ask != nil {
			it := item{name: "ask"}
			w.describe(&it, askAt(part), ask)
			g.ask = &it
		}
		byName[part] = g
		groups = append(groups, g)
	}
	var free *partBox
	for _, t := range sortedKeys(a.Tools) {
		tool := a.Tools[t]
		if tool.Job != nil && strings.HasPrefix(tool.Job.Script, "corbie:") {
			continue // Corbie's own, which no definition writes
		}
		g := byName[tool.Part]
		if g == nil {
			if free == nil {
				free = &partBox{name: "", items: map[string]item{}}
			}
			g = free
		}
		it := item{name: t, title: tool.Does}
		w.describe(&it, t, tool.Job)
		if tool.Job != nil && tool.Job.Approve {
			it.lines = append(it.lines, "approve: true")
		}
		g.tools, g.items[t] = append(g.tools, t), it
	}
	if free != nil {
		groups = append(groups, free)
	}
	for _, g := range groups {
		g.size()
	}
	return groups
}

// A column is what is drawn one below another in the holder of an agent's
// tools: one part, the tools in no part, or the parts folded.
type column []*partBox

func (c column) size() (w, h float64) {
	for _, g := range c {
		w = math.Max(w, g.at.w)
		h += g.at.h + 12
	}
	return w, h - 12
}

// columnsOf gives each group a column of its own, but on a run's page a
// part whose tools and ask the run has not asked for is folded, and the
// parts folded are one column after the others.
func columnsOf(groups []*partBox, run *Run) []column {
	var columns []column
	var folded column
	for _, g := range groups {
		if run != nil && g.part && !g.used(run) {
			g.folded = true
			g.size()
			folded = append(folded, g)
			continue
		}
		columns = append(columns, column{g})
	}
	if folded != nil {
		columns = append(columns, folded)
	}
	return columns
}

// used says a run has asked for a part's ask or one of its tools.
func (g *partBox) used(run *Run) bool {
	if g.ask != nil && jobMark(run, askAt(g.name)) != "never" {
		return true
	}
	return slices.ContainsFunc(g.tools, func(t string) bool { return jobMark(run, t) != "never" })
}

// paintGroup draws a column of what the model can call, and says whether a
// tool in it is one the agent waits on. A part's name is cut to its holder,
// and on the Machines page its tooltip gives the file it is included from.
func (d *drawn) paintGroup(g *partBox, run *Run) bool {
	b := g.at
	y := b.y
	if g.folded {
		fmt.Fprintf(&d.holders, `<g class="state holder part folded"><rect x="%s" y="%s" width="%s" height="%s" rx="14"/><text class="name" x="%s" y="%s">%s</text><text class="sub" x="%s" y="%s">%d %s</text><title>%s</title></g>`,
			num(b.x), num(b.y), num(b.w), num(b.h), num(b.x+14), num(b.y+22), esc(clip(g.name, int((b.w-28)/nameW))),
			num(b.x+14), num(b.y+39), len(g.tools), word(len(g.tools), "tool"), esc(strings.Join(g.tools, ", ")))
		d.reach(b)
		return false
	}
	if g.part {
		title := ""
		if g.file != "" {
			title = "<title>" + esc(g.file) + "</title>"
		}
		fmt.Fprintf(&d.holders, `<g class="state holder part"><rect x="%s" y="%s" width="%s" height="%s" rx="14"/><text class="name" x="%s" y="%s">%s</text>%s</g>`,
			num(b.x), num(b.y), num(b.w), num(b.h), num(b.x+14), num(b.y+22), esc(clip(g.name, int((b.w-28)/nameW))), title)
		y += innerTop
	}
	d.reach(b)
	now := false
	for _, t := range g.tools {
		mark := jobMark(run, t)
		now = now || mark == "now" || mark == "held"
		at := g.items[t].size()
		at.x, at.y = b.cx()-at.w/2, y
		d.paint(g.items[t], at, strings.TrimSpace("state tool "+mark), cardR, "")
		y += at.h + 12
	}
	if g.ask != nil {
		at := g.ask.size()
		at.x, at.y = b.cx()-at.w/2, y
		d.paint(*g.ask, at, strings.TrimSpace("state ask "+jobMark(run, askAt(g.name))), cardR, "")
	}
	return now
}

// jobMark is how a tool or an ask of a run is marked: where the run failed,
// held for the ask, going now, asked for at an earlier step, or never asked
// for.
func jobMark(run *Run, name string) string {
	switch {
	case run == nil:
		return ""
	case run.failedAt(name):
		return "failed"
	case slices.Contains(run.Held, name) && !run.Ended:
		return "held"
	case slices.Contains(run.Going, name) && !run.Ended:
		return "now"
	case run.asked(name):
		return "done"
	}
	return "never"
}

func word(n int, one string) string {
	if n == 1 {
		return one
	}
	return one + "s"
}

// spark is the mark of a model: a star of four points.
func spark(cx, cy, r float64) string {
	k := r * 0.22
	return fmt.Sprintf("M%s %sC%s %s %s %s %s %sC%s %s %s %s %s %sC%s %s %s %s %s %sC%s %s %s %s %s %sZ",
		num(cx), num(cy-r),
		num(cx+k), num(cy-k), num(cx+k), num(cy-k), num(cx+r), num(cy),
		num(cx+k), num(cy+k), num(cx+k), num(cy+k), num(cx), num(cy+r),
		num(cx-k), num(cy+k), num(cx-k), num(cy+k), num(cx-r), num(cy),
		num(cx-k), num(cy-k), num(cx-k), num(cy-k), num(cx), num(cy-r))
}
