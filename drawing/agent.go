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
// in no part, with where its tools and its ask are drawn.
type partBox struct {
	name  string
	part  bool
	tools []string
	items map[string]item
	ask   *item
	at    box
}

func (g *partBox) size() {
	g.at.h, g.at.w = 0, cardW
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
	groups := groupsOf(agentName, a, run, w)
	var askItem *item
	if ask != nil {
		it := item{name: "ask"}
		w.describe(&it, ask)
		it.href, it.in = w.open("ask", ask.Machine)
		askItem = &it
	}

	across, high := float64(max(len(groups)-1, 0))*sideGap, 0.0
	for _, g := range groups {
		across += g.at.w
		high = math.Max(high, g.at.h)
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

	waiting, _ := shown(run.shownOrNil(), "waiting").(string)
	var d drawn
	y := top.bottom()
	class := ""
	if has {
		tools.x, tools.y = left, y+2*rowGap
		fmt.Fprintf(&d.holders, `<g class="state holder"><rect x="%s" y="%s" width="%s" height="%s" rx="14"/><text class="name" x="%s" y="%s">tools</text></g>`,
			num(tools.x), num(tools.y), num(tools.w), num(tools.h), num(tools.x+14), num(tools.y+22))
		d.reach(tools)
		x, ty := tools.x+inner+(tools.w-2*inner-across)/2, tools.y+innerTop
		for _, g := range groups {
			g.at.x, g.at.y = x, ty
			x += g.at.w + sideGap
			if d.paintGroup(g, run, waiting) {
				class = "taken"
			}
		}
		if askItem != nil {
			b := askItem.size()
			b.x, b.y = tools.cx()-b.w/2, tools.bottom()-inner-b.h
			d.paint(*askItem, b, "state ask", 10, "")
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
	if run != nil {
		mark = "done"
		if waiting == "turn" && !run.Ended {
			mark = "now"
		}
	}
	d.paint(model, top, strings.TrimSpace("state model "+mark), 12, "")
	if run != nil {
		if turn, ok := shown(run.Shown, "turn").(float64); ok {
			fmt.Fprintf(&d.shapes, `<text class="tag" x="%s" y="%s" text-anchor="start">turn %d</text>`, num(top.x+4), num(top.y-8), int(turn))
		}
	}
	d.paint(item{name: "reply"}, reply, "state reply", reply.h/2, "")
	return d
}

// groupsOf gives each part of an agent, then the tools in no part.
func groupsOf(agentName string, a *agent.Agent, run *Run, w *walker) []*partBox {
	var groups []*partBox
	byName := map[string]*partBox{}
	for _, part := range sortedKeys(a.Parts) {
		g := &partBox{name: part, part: true, items: map[string]item{}}
		if file := w.part(agentName, part, a.Parts[part].Does); file != "" {
			g.name += " · " + clipStart(file, longest)
		}
		if ask := a.Parts[part].Ask; ask != nil {
			it := item{name: "ask"}
			w.describe(&it, ask)
			it.href, it.in = w.open("open."+part+".ask", ask.Machine)
			g.ask = &it
		}
		byName[part] = g
		groups = append(groups, g)
	}
	var free *partBox
	for _, t := range sortedKeys(a.Tools) {
		tool := a.Tools[t]
		if run == nil && tool.Job != nil && strings.HasPrefix(tool.Job.Script, "corbie:") {
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
		w.describe(&it, tool.Job)
		if tool.Job != nil {
			if tool.Job.Approve {
				it.lines = append(it.lines, "approve: true")
			}
			it.href, it.in = w.open(t, tool.Job.Machine)
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

// paintGroup draws a column of what the model can call, and says whether a
// tool in it is one the agent waits on.
func (d *drawn) paintGroup(g *partBox, run *Run, waiting string) bool {
	b := g.at
	y := b.y
	if g.part {
		fmt.Fprintf(&d.holders, `<g class="state holder part"><rect x="%s" y="%s" width="%s" height="%s" rx="12"/><text class="name" x="%s" y="%s">%s</text></g>`,
			num(b.x), num(b.y), num(b.w), num(b.h), num(b.x+14), num(b.y+22), esc(g.name))
		y += innerTop
	}
	d.reach(b)
	now := false
	for _, t := range g.tools {
		mark := ""
		if run != nil {
			mark = "never"
			if waiting == "tools" && asked(run.Asked, t) {
				mark, now = "now", true
			}
		}
		at := g.items[t].size()
		at.x, at.y = b.cx()-at.w/2, y
		d.paint(g.items[t], at, strings.TrimSpace("state tool "+mark), 10, "")
		y += at.h + 12
	}
	if g.ask != nil {
		at := g.ask.size()
		at.x, at.y = b.cx()-at.w/2, y
		d.paint(*g.ask, at, "state ask", 10, "")
	}
	return now
}

// asked says whether a tool is among the jobs a step asked for: a job takes
// its tool's name, with what tells its calls apart after it.
func asked(jobs []string, tool string) bool {
	return slices.ContainsFunc(jobs, func(job string) bool {
		return job == tool || strings.HasPrefix(job, tool+"~") || strings.HasPrefix(job, tool+"#") || strings.HasPrefix(job, tool+".")
	})
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

func (r *Run) shownOrNil() any {
	if r == nil {
		return nil
	}
	return r.Shown
}
