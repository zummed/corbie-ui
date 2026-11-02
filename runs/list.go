package runs

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zummed/corbie/client"
	"github.com/zummed/corbie/instance"
	"github.com/zummed/corbie/store"
)

// A narrowing is what the address of the Runs page holds: the runs it
// narrows to, the timeline's window, and what the timeline's lanes are by.
type narrowing struct {
	Status, Client, Window, Lanes string
	Labels                        []string
}

// A window is a length of time the timeline offers.
type window struct {
	Name string
	Long time.Duration
}

var windows = []window{{"1h", time.Hour}, {"6h", 6 * time.Hour}, {"24h", 24 * time.Hour}, {"7d", 7 * 24 * time.Hour}}

func narrowingOf(q url.Values) narrowing {
	n := narrowing{Status: q.Get("status"), Client: q.Get("client"), Window: q.Get("window"), Lanes: q.Get("lanes"), Labels: strings.Fields(q.Get("label"))}
	if !slices.ContainsFunc(windows, func(w window) bool { return w.Name == n.Window }) {
		n.Window = "6h"
	}
	if n.Lanes == "" {
		n.Lanes = "machine"
	}
	return n
}

func (n narrowing) long() time.Duration {
	for _, w := range windows {
		if w.Name == n.Window {
			return w.Long
		}
	}
	return 6 * time.Hour
}

// with gives the address of the page with one thing changed.
func (n narrowing) with(change func(*narrowing)) string {
	change(&n)
	q := url.Values{}
	set := func(k, v, none string) {
		if v != none {
			q.Set(k, v)
		}
	}
	set("status", n.Status, "")
	set("client", n.Client, "")
	set("label", strings.Join(n.Labels, " "), "")
	set("window", n.Window, "6h")
	set("lanes", n.Lanes, "machine")
	if len(q) == 0 {
		return "/runs"
	}
	return "/runs?" + q.Encode()
}

// WithClient gives the address narrowed to a client, or to every client.
func (n narrowing) WithClient(c string) string { return n.with(func(m *narrowing) { m.Client = c }) }

// LabelText is the labels narrowed to, as they are typed.
func (n narrowing) LabelText() string { return strings.Join(n.Labels, " ") }

func (n narrowing) query() string {
	if a := n.with(func(*narrowing) {}); strings.Contains(a, "?") {
		return a[strings.Index(a, "?"):]
	}
	return ""
}

// keeps says whether a run is one the narrowing leaves: a label with a
// value must match it, and one with none need only be there, as `corbie
// ls --label` reads it.
func (n narrowing) keeps(r *client.Run, byStatus bool) bool {
	if byStatus && n.Status != "" && r.Status != n.Status {
		return false
	}
	if n.Client != "" && store.InstanceClient(r.ID) != n.Client {
		return false
	}
	for _, l := range n.Labels {
		name, want, valued := strings.Cut(l, "=")
		got, ok := r.Labels[name]
		if !ok || (valued && got != want) {
			return false
		}
	}
	return true
}

// listView is the Runs page as it is drawn.
type listView struct {
	Narrowing narrowing
	Now       nowView
	Statuses  []choice
	Clients   []choice
	Timeline  timeline
	Running   []row
	Ended     []row
}

type choice struct {
	Name, Href string
	Count      int
	Current    bool
}

type nowView struct {
	Running, Jobs, Timers, Tickets int
}

type row struct {
	ID, Who, Stamp, Status, Failure, Machine, Type, Waits, Labels, When, Took, Href string
	Step                                                                         int
	Child, Ended                                                                 bool
}

func listOf(runs []*client.Run, n narrowing, at time.Time) listView {
	v := listView{Narrowing: n}
	base := slices.DeleteFunc(slices.Clone(runs), func(r *client.Run) bool { return !n.keeps(r, false) })
	count := map[string]int{"": len(base)}
	for _, r := range base {
		count[r.Status]++
		if r.Status == instance.Running {
			v.Now.Running++
			v.Now.Jobs += r.Jobs
			v.Now.Timers += r.Timers
			v.Now.Tickets += len(r.Tickets)
		}
	}
	for _, s := range append([]string{""}, client.Statuses...) {
		name := "All"
		if s != "" {
			name = strings.ToUpper(s[:1]) + s[1:]
		}
		v.Statuses = append(v.Statuses, choice{Name: name, Href: n.with(func(m *narrowing) { m.Status = s }), Count: count[s], Current: n.Status == s})
	}
	clients := map[string]bool{}
	for _, r := range runs {
		clients[store.InstanceClient(r.ID)] = true
	}
	for _, c := range slices.Sorted(maps.Keys(clients)) {
		v.Clients = append(v.Clients, choice{Name: c, Href: n.with(func(m *narrowing) { m.Client = c }), Current: n.Client == c})
	}

	shown := slices.DeleteFunc(slices.Clone(base), func(r *client.Run) bool { return !n.keeps(r, true) })
	from := at.Add(-n.long())
	v.Timeline = timelineOf(shown, n, from, at)

	running := map[string]*client.Run{}
	for _, r := range shown {
		if r.Status == instance.Running {
			running[r.ID] = r
		}
	}
	var roots []*client.Run
	for _, r := range running {
		if running[r.Parent] == nil {
			roots = append(roots, r)
		}
	}
	slices.SortFunc(roots, func(a, b *client.Run) int { return b.Started.Compare(a.Started) })
	var add func(r *client.Run, child bool)
	add = func(r *client.Run, child bool) {
		v.Running = append(v.Running, rowOf(r, child, at))
		var kids []*client.Run
		for _, k := range running {
			if k.Parent == r.ID {
				kids = append(kids, k)
			}
		}
		slices.SortFunc(kids, func(a, b *client.Run) int { return a.Started.Compare(b.Started) })
		for _, k := range kids {
			add(k, true)
		}
	}
	for _, r := range roots {
		add(r, false)
	}
	var ended []*client.Run
	for _, r := range shown {
		if r.HasEnded() && !r.Ended.Before(from) {
			ended = append(ended, r)
		}
	}
	slices.SortFunc(ended, func(a, b *client.Run) int { return b.Ended.Compare(a.Ended) })
	for _, r := range ended {
		v.Ended = append(v.Ended, rowOf(r, false, at))
	}
	return v
}

func rowOf(r *client.Run, child bool, at time.Time) row {
	who, stamp, _ := strings.Cut(r.ID, "/")
	w := row{ID: r.ID, Who: who, Stamp: stamp, Status: r.Status, Failure: r.Failure, Machine: r.Machine, Type: r.Type,
		Step: r.Step, Child: child, Ended: r.HasEnded(), Waits: waitingOn(r), Labels: labelsOf(r), Href: "/run/" + r.ID}
	if r.HasEnded() {
		w.When, w.Took = when(r.Ended, at), took(r.Ended.Sub(r.Started))
	} else {
		w.When = when(r.Started, at)
	}
	return w
}

// waitingOn and labelsOf are a run's words as `corbie run ls` prints them.
func waitingOn(r *client.Run) string {
	var parts []string
	if r.Jobs > 0 {
		parts = append(parts, plural(r.Jobs, "job"))
	}
	if r.Timers > 0 {
		parts = append(parts, plural(r.Timers, "timer"))
	}
	if len(r.Tickets) > 0 {
		parts = append(parts, "ticket "+strings.Join(r.Tickets, ", "))
	}
	return strings.Join(parts, ", ")
}

func labelsOf(r *client.Run) string {
	var labels []string
	for _, name := range slices.Sorted(maps.Keys(r.Labels)) {
		labels = append(labels, strings.TrimSuffix(name+"="+r.Labels[name], "="))
	}
	return strings.Join(labels, " ")
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

// when is a time as a list shows it: the time of day within the last day,
// and the date before that.
func when(t, at time.Time) string {
	t = t.Local()
	if at.Sub(t) < 20*time.Hour {
		return t.Format("15:04")
	}
	return t.Format("2 Jan")
}

func took(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

// A timeline is the runs of a window that ends now: a lane for each
// machine, client or label value, and a bar for each run.
type timeline struct {
	Lanes   []laneView
	Ticks   []tick
	Windows []choice
	By      []choice
}

type laneView struct {
	Key, Name, Type string
	Height          float64
	Bars            []barView
}

type barView struct {
	ID, Status, Href, Tip string
	Left, Width, Top     float64
	Clipped              bool
}

type tick struct {
	Left  float64
	Label string
}

func timelineOf(runs []*client.Run, n narrowing, from, to time.Time) timeline {
	t := timeline{}
	long := to.Sub(from)
	for _, w := range windows {
		t.Windows = append(t.Windows, choice{Name: w.Name, Href: n.with(func(m *narrowing) { m.Window = w.Name }), Current: n.Window == w.Name})
	}
	labels := map[string]bool{}
	for _, r := range runs {
		for k := range r.Labels {
			labels[k] = true
		}
	}
	for _, by := range append([]string{"machine", "client"}, slices.Sorted(maps.Keys(labels))...) {
		name := by
		if by != "machine" && by != "client" {
			name += "="
		}
		t.By = append(t.By, choice{Name: name, Href: n.with(func(m *narrowing) { m.Lanes = by }), Current: n.Lanes == by})
	}
	laneOf := func(r *client.Run) string {
		switch n.Lanes {
		case "machine":
			return r.Machine
		case "client":
			return store.InstanceClient(r.ID)
		}
		return r.Labels[n.Lanes]
	}
	in := slices.DeleteFunc(slices.Clone(runs), func(r *client.Run) bool { return r.HasEnded() && r.Ended.Before(from) })
	slices.SortFunc(in, func(a, b *client.Run) int { return cmp.Or(a.Started.Compare(b.Started), strings.Compare(a.ID, b.ID)) })
	lanes := map[string]*laneView{}
	rows := map[string][]time.Time{}
	gap := long / 160
	pct := func(at time.Time) float64 { return math.Round(float64(at.Sub(from))/float64(long)*10000) / 100 }
	for _, r := range in {
		key := laneOf(r)
		l := lanes[key]
		if l == nil {
			l = &laneView{Key: key, Name: key}
			if n.Lanes == "machine" {
				l.Type = r.Type
			}
			lanes[key] = l
		}
		start, end := r.Started, to
		if r.HasEnded() {
			end = r.Ended
		}
		// Runs that overlap in a lane are drawn one under another.
		ends := rows[key]
		i := slices.IndexFunc(ends, func(e time.Time) bool { return !e.After(start) })
		if i < 0 {
			i = len(ends)
			ends = append(ends, time.Time{})
		}
		ends[i] = later(end, start.Add(gap))
		rows[key] = ends
		left := pct(later(start, from))
		b := barView{ID: r.ID, Status: r.Status, Href: "/run/" + r.ID, Left: left, Width: math.Max(0, pct(end)-left), Top: 6.5 + float64(i)*9, Clipped: start.Before(from)}
		b.Tip = tipOf(r, to)
		l.Bars = append(l.Bars, b)
	}
	for key, l := range lanes {
		l.Height = 19 + float64(len(rows[key])-1)*9
		t.Lanes = append(t.Lanes, *l)
	}
	slices.SortFunc(t.Lanes, func(a, b laneView) int {
		if (a.Key == "") != (b.Key == "") {
			if a.Key == "" {
				return 1
			}
			return -1
		}
		return strings.Compare(a.Key, b.Key)
	})
	for i := range t.Lanes {
		if t.Lanes[i].Key == "" {
			t.Lanes[i].Name = "no " + n.Lanes + "="
		}
	}
	step := time.Hour
	switch {
	case long <= time.Hour:
		step = 10 * time.Minute
	case long <= 6*time.Hour:
		step = time.Hour
	case long <= 24*time.Hour:
		step = 3 * time.Hour
	default:
		step = 24 * time.Hour
	}
	for at := from.Truncate(step).Add(step); at.Before(to.Add(-step * 35 / 100)); at = at.Add(step) {
		if at.Sub(from) < step/5 {
			continue
		}
		label := at.Local().Format("15:04")
		if step >= 24*time.Hour {
			label = at.Local().Format("2 Jan")
		}
		t.Ticks = append(t.Ticks, tick{Left: pct(at), Label: label})
	}
	return t
}

func tipOf(r *client.Run, at time.Time) string {
	how := "for " + took(at.Sub(r.Started))
	if r.HasEnded() {
		how = "took " + took(r.Ended.Sub(r.Started))
	}
	status := r.Status
	if r.Failure != "" {
		status += " (" + r.Failure + ")"
	}
	return fmt.Sprintf("%s %s\n%s\n%s · step %d · %s", r.Type, r.Machine, r.ID, status, r.Step, how)
}

func strconvF(f float64) string { return strconv.FormatFloat(f, 'f', 2, 64) }

func pluralWord(n int, one string) string {
	if n == 1 {
		return one
	}
	return one + "s"
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
