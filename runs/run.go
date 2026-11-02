package runs

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"html/template"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zummed/corbie-ui/drawing"
	"github.com/zummed/corbie-ui/page"
	"github.com/zummed/corbie/client"
	"github.com/zummed/corbie/instance"
	"github.com/zummed/corbie/machine"
	"github.com/zummed/corbie/statechart"
	"github.com/zummed/corbie/store"
)

// runView is a run's page as it is drawn.
type runView struct {
	Row      row
	Run      *client.Run
	Mine     bool
	Labels   string
	Started  string
	Ended    string
	Drawing  template.HTML
	Tickets  []ticket
	Timers   []string
	Lines    []line
	Output   string
	Used     string
	Shape    string
	Files    []file
	Children []row
	Cancel   *page.Call
	Act      bool
	Reply    bool
}

type ticket struct{ Name, Value string }

type file struct {
	Path, Type, Size, Href string
}

// record is what a run's page has read of its record, as it is read.
type record struct {
	mu      sync.Mutex
	entries []client.Entry
	changed chan struct{}
}

func newRecord() *record { return &record{changed: make(chan struct{})} }

func (r *record) add(e client.Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, e)
	close(r.changed)
	r.changed = make(chan struct{})
}

func (r *record) all() []client.Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.entries)
}

func (r *record) Changed() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.changed
}

// mine says whether the UI's client started a run, and so may read it all.
func (h *handler) mine(id string) bool {
	return h.Client != nil && store.InstanceClient(id) == h.Client.ID.Name
}

// runOf reads what a run's page shows: everything for a run the UI's
// client started, and its status for any other.
func (h *handler) runOf(ctx context.Context, id string, entries []client.Entry) (runView, error) {
	at := time.Now()
	v := runView{Mine: h.mine(id)}
	c := h.Lister
	if v.Mine {
		c = h.Client
	}
	run, err := c.Show(ctx, id)
	if err != nil {
		return v, err
	}
	v.Run, v.Row = run, rowOf(run, false, at)
	v.Labels = labelsOf(run)
	v.Started = run.Started.Local().Format("15:04:05")
	if run.HasEnded() {
		v.Ended = run.Ended.Local().Format("15:04:05")
	}
	runs, _ := h.List.Runs()
	for _, r := range runs {
		if r.Parent == id {
			v.Children = append(v.Children, rowOf(r, false, at))
		}
	}
	slices.SortFunc(v.Children, func(a, b row) int { return strings.Compare(a.Stamp, b.Stamp) })

	v.Reply = h.Client != nil && !run.HasEnded() && !v.Mine
	switch {
	case !run.HasEnded() && v.Mine:
		v.Act = true
		v.Cancel = &page.Call{Words: []string{"run", "cancel"}, Args: []string{id}}
	case !run.HasEnded() && h.Controller:
		v.Cancel = &page.Call{Words: []string{"controller", "cancel"}, Args: []string{id}}
	}
	if !v.Mine {
		return v, nil
	}

	if run.Output != nil {
		data, _ := json.Marshal(run.Output, json.Deterministic(true))
		v.Output = indent(data)
	}
	v.Used = strings.Join(run.Used, " ")
	def, err := h.Client.Definition(ctx, id)
	if err == nil {
		data, _ := json.Marshal(def, json.Deterministic(true))
		v.Shape = indent(data)
		v.Drawing, _ = drawing.Draw(def, marksOf(id, entries, run))
		v.Timers = timersOf(def, entries, id, run)
	}
	if !run.HasEnded() && len(run.Tickets) > 0 {
		tickets, err := h.Client.Tickets(ctx, id)
		if err == nil {
			for _, name := range slices.Sorted(maps.Keys(tickets)) {
				v.Tickets = append(v.Tickets, ticket{Name: name, Value: tickets[name]})
			}
		}
	}
	if run.HasEnded() {
		files, err := h.Client.Files(ctx, id)
		if err == nil {
			paths := slices.Sorted(maps.Keys(files))
			for _, p := range paths {
				f := files[p]
				v.Files = append(v.Files, file{Path: p, Type: f.Type, Size: size(f.Size), Href: "/run/" + id + "/file?path=" + template.URLQueryEscaper(p)})
			}
		}
	}
	for _, e := range entries {
		v.Lines = append(v.Lines, lines(e)...)
	}
	return v, nil
}

// marksOf gives what a run shows of its state, from the steps of its own
// record, for its drawing.
func marksOf(id string, entries []client.Entry, run *client.Run) *drawing.Run {
	var steps []client.Entry
	for _, e := range entries {
		if e.ID == id && e.Step > 0 {
			steps = append(steps, e)
		}
	}
	if len(steps) == 0 {
		return nil
	}
	m := &drawing.Run{Ended: run.HasEnded()}
	latest := steps[len(steps)-1]
	m.Shown = plain(latest.Shown)
	if len(steps) > 1 {
		m.Before = plain(steps[len(steps)-2].Shown)
	}
	for _, e := range latest.Events {
		m.Events = append(m.Events, e.Name)
	}
	for _, a := range latest.Asks {
		if a.Kind == instance.AskScript || a.Kind == instance.AskChild {
			m.Asked = append(m.Asked, a.Name)
		}
	}
	for _, s := range steps {
		if shown, ok := plain(s.Shown).(map[string]any); ok {
			names, _ := shown["states"].([]any)
			for _, n := range names {
				if name, ok := n.(string); ok && !slices.Contains(m.Entered, name) {
					m.Entered = append(m.Entered, name)
				}
			}
		}
	}
	return m
}

// plain gives what a type shows as JSON reads it: the drawing reads lists,
// numbers and mappings as JSON gives them, whatever Go types they were made
// as.
func plain(v any) any {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out any
	json.Unmarshal(data, &out)
	return out
}

// timersOf gives the timers a statechart run waits on, each with its time
// as the definition writes it: those of the states it is in.
func timersOf(def *machine.Definition, entries []client.Entry, id string, run *client.Run) []string {
	if def.Type != "statechart" || run.Timers == 0 {
		return nil
	}
	m := marksOf(id, entries, run)
	if m == nil {
		return nil
	}
	var chart statechart.Chart
	if json.Unmarshal(def.Body, &chart) != nil {
		return nil
	}
	states := map[string]*statechart.State{}
	var walk func(map[string]*statechart.State)
	walk = func(in map[string]*statechart.State) {
		for name, s := range in {
			states[name] = s
			if s != nil {
				walk(s.States)
			}
		}
	}
	walk(chart.States)
	shown, _ := m.Shown.(map[string]any)
	active, _ := shown["states"].([]any)
	var timers []string
	for _, a := range active {
		name, _ := a.(string)
		s := states[name]
		if s == nil {
			continue
		}
		for _, t := range slices.Sorted(maps.Keys(s.Timers)) {
			timers = append(timers, t+", "+s.Timers[t])
		}
	}
	sort.Strings(timers)
	return timers
}

func indent(data []byte) string {
	var v any
	if json.Unmarshal(data, &v) != nil {
		return string(data)
	}
	out, err := json.Marshal(v, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return string(data)
	}
	return string(out)
}

func size(n int64) string {
	switch {
	case n < 1<<10:
		return plural(int(n), "byte")
	case n < 1<<20:
		return strconv.FormatFloat(float64(n)/(1<<10), 'f', 1, 64) + " KB"
	}
	return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) + " MB"
}
