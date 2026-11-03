package drawing

import (
	"fmt"
	"maps"
	"math"
	"slices"

	"github.com/zummed/corbie/flow"
	"github.com/zummed/corbie/machine"
)

func drawFlow(f *flow.Flow, run *Run, w *walker) drawn {
	names := sortedKeys(f.Stages)
	after := map[string][]string{}
	for _, n := range names {
		for _, a := range waitsFor(f.Stages[n]) {
			if _, ok := f.Stages[a]; ok && a != n {
				after[n] = append(after[n], a)
			}
		}
	}
	// A stage's row is the longest chain of stages it waits for.
	row := map[string]int{}
	var depth func(n string, seen map[string]bool) int
	depth = func(n string, seen map[string]bool) int {
		if c, ok := row[n]; ok {
			return c
		}
		if seen[n] {
			return 0 // a stage that waits for itself, which the check refuses
		}
		seen[n] = true
		c := 0
		for _, a := range after[n] {
			c = max(c, depth(a, seen)+1)
		}
		row[n] = c
		return c
	}
	var rows [][]string
	for _, n := range names {
		c := depth(n, map[string]bool{})
		for len(rows) <= c {
			rows = append(rows, nil)
		}
		rows[c] = append(rows[c], n)
	}
	// Within a row, a stage goes by the places of the stages it waits for,
	// to keep arrows short.
	place := map[string]float64{}
	for _, c := range rows {
		mean := func(n string) float64 {
			if len(after[n]) == 0 {
				return math.Inf(1)
			}
			sum := 0.0
			for _, a := range after[n] {
				sum += place[a]
			}
			return sum / float64(len(after[n]))
		}
		slices.SortStableFunc(c, func(a, b string) int {
			ma, mb := mean(a), mean(b)
			switch {
			case ma < mb:
				return -1
			case ma > mb:
				return 1
			}
			return 0
		})
		for i, n := range c {
			place[n] = float64(i) - float64(len(c)-1)/2
		}
	}

	items := map[string]item{}
	for _, n := range names {
		s := f.Stages[n]
		it := item{name: n}
		w.describe(&it, n, s.Job)
		if s.When != "" {
			it.title = "when: " + s.When
		}
		items[n] = it
	}
	at := map[string]box{}
	across := func(c []string) float64 {
		used := float64(len(c)-1) * sideGap
		for _, n := range c {
			used += items[n].size().w
		}
		return used
	}
	total := 0.0
	for _, c := range rows {
		total = math.Max(total, across(c))
	}
	y := margin + 8
	for _, c := range rows {
		high := 0.0
		x := margin + (total-across(c))/2
		for _, n := range c {
			b := items[n].size()
			b.x, b.y = x, y
			at[n] = b
			x += b.w + sideGap
			high = math.Max(high, b.h)
		}
		y += high + rowGap
	}

	var d drawn
	reach := map[string]map[string]bool{}
	for _, n := range names {
		for _, a := range after[n] {
			if !through(after, reach, n, a) {
				d.down(at[a], at[n], "", "")
			}
		}
	}
	stages := map[string]string{}
	if run != nil {
		if m, ok := shown(run.latest(), "stages").(map[string]any); ok {
			for n, s := range m {
				stages[n], _ = s.(string)
			}
		}
	}
	for _, n := range names {
		s, b := f.Stages[n], at[n]
		class := "state stage"
		if run != nil {
			class += " " + stageMark(stages[n])
		}
		if s.When != "" {
			class += " when"
		}
		behind := ""
		if s.Each != nil || s.Emits != "" || s.Blob != nil {
			for _, off := range []float64{8, 4} {
				behind += fmt.Sprintf(`<rect class="behind" x="%s" y="%s" width="%s" height="%s" rx="%s"/>`, num(b.x+off), num(b.y-off), num(b.w), num(b.h), num(cardR))
			}
		}
		d.paint(items[n], b, class, cardR, behind)
		d.reach(box{x: b.x, y: b.y, w: b.w + 8, h: b.h})
	}
	return d
}

// through says a stage waits for another through a third, so an arrow to it
// from the other says nothing more.
func through(after map[string][]string, reach map[string]map[string]bool, n, a string) bool {
	return slices.ContainsFunc(after[n], func(b string) bool { return b != a && reaches(after, reach, b)[a] })
}

// reaches gives every stage a stage waits for, near or far, once for each
// stage. A stage that waits for itself, which the check refuses, ends it.
func reaches(after map[string][]string, reach map[string]map[string]bool, n string) map[string]bool {
	if known, ok := reach[n]; ok {
		return known
	}
	all := map[string]bool{}
	reach[n] = all
	for _, a := range after[n] {
		all[a] = true
		maps.Copy(all, reaches(after, reach, a))
	}
	return all
}

// stageMark is how a stage's status is marked.
func stageMark(status string) string {
	switch status {
	case "running":
		return "now"
	case "done", "failed", "skipped":
		return status
	}
	return "never"
}

// waitsFor gives the stages a stage waits for: those its expressions read,
// those it follows, and the one whose emitted items are its own. It is the
// rule the flow package keeps to itself, applied to the same expressions.
func waitsFor(s *flow.Stage) []string {
	var reads []machine.Read
	if s.Job != nil {
		reads = append(reads, machine.ReadsIn(s.Job.Input, "stages")...)
		reads = append(reads, machine.Reads(s.Job.Machine, "stages")...)
		reads = append(reads, machine.Reads(s.Job.Script, "stages")...)
	}
	reads = append(reads, machine.ReadsIn(s.Each, "stages")...)
	reads = append(reads, machine.ReadsIn(s.Blob, "stages")...)
	if s.When != "" {
		reads = append(reads, machine.ReadsOf(s.When, "stages")...)
	}
	var names []string
	add := func(n string) {
		if n != "" && !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	for _, r := range reads {
		add(r.Name)
	}
	for _, n := range s.Follows {
		add(n)
	}
	add(s.Emits)
	return names
}
