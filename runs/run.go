package runs

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"html/template"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zummed/corbie-ui/drawing"
	"github.com/zummed/corbie-ui/page"
	"github.com/zummed/corbie/client"
	"github.com/zummed/corbie/identity"
	"github.com/zummed/corbie/instance"
	"github.com/zummed/corbie/machine"
	"github.com/zummed/corbie/store"
)

// runParts are the parts of a run's page that follow the run.
var runParts = []string{"summary", "path", "drawing", "waits", "record", "output", "children"}

// runView is a run's page as it is drawn.
type runView struct {
	Run                  *client.Run
	Mine                 bool
	Started, Ended, Took string
	Path                 []link
	Drawing              template.HTML
	Waits                []wait
	Labels               []label
	Lines                []line
	Output               string
	Used                 string
	Files                []file
	Children             []child
	Cancel               *page.Call
	Send, Reply          bool
}

// A link is a run named by its machine, as the path and the waits show it.
type link struct {
	ID, Machine, Type string
	This              bool
}

// A wait is one thing a run waits on, in words: a ticket with its name,
// and its value where the UI's client started the run, or a running child
// with its run.
type wait struct {
	Text, Ticket, Value string
	Run                 *link
}

// String is a wait as one line of words.
func (w wait) String() string {
	if w.Run != nil {
		return w.Text + " " + w.Run.Machine
	}
	return w.Text
}

// inWords gives waits as the Runs page writes them, in one line.
func inWords(waits []wait) string {
	var words []string
	for _, w := range waits {
		words = append(words, w.String())
	}
	return strings.Join(words, ", ")
}

type label struct{ Name, Value string }

type file struct {
	Path, Type, Size, Href string
}

// A child is a run another started, as its parent's page lists it.
type child struct {
	ID, Status, Failure, Machine, Type, When, Took string
	Ended                                          bool
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

// reading is what a run's page has read of a run the UI's client started,
// kept while the page is open: its latest step, read again when the run
// takes another, and its definition and the machines it names, which never
// change.
type reading struct {
	step     *instance.Step
	def      *machine.Definition
	machines map[string]drawing.Machine
}

// read reads a run's latest step, and its definition and the machines it
// names once. A run with no step yet, or one sealed to a key the client no
// longer has, has neither.
func (h *handler) read(ctx context.Context, id string, number int, r *reading) error {
	if r.step != nil && r.step.Number >= number {
		return nil
	}
	step, err := instance.Latest(ctx, h.Client.Store, h.Client.Controller, h.Client.ID, id, number)
	if errors.Is(err, identity.ErrNotForUs) {
		return nil
	}
	if err != nil || step == nil {
		return err
	}
	r.step = step
	if r.def != nil {
		return nil
	}
	def, err := machine.Load(ctx, h.Client.Store, step.State.Definition)
	if err != nil {
		return err
	}
	machines := map[string]drawing.Machine{}
	for name, ref := range def.Machines {
		child, err := machine.Load(ctx, h.Client.Store, step.State.Definition.Beside(ref))
		if err != nil {
			return err
		}
		machines[name] = drawing.Machine{Type: child.Type, Name: child.Name}
	}
	r.def, r.machines = def, machines
	return nil
}

// mine says whether the UI's client started a run, and so may read it all.
func (h *handler) mine(id string) bool {
	return h.Client != nil && store.InstanceClient(id) == h.Client.ID.Name
}

// runOf reads what a run's page shows: everything for a run the UI's
// client started, and what the list says of any other.
func (h *handler) runOf(ctx context.Context, id string, entries []client.Entry, r *reading) (runView, error) {
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
	v.Run = run
	v.Started = clock(run.Started, at)
	if run.HasEnded() {
		v.Ended, v.Took = clock(run.Ended, at), took(run.Ended.Sub(run.Started))
	}
	for _, name := range slices.Sorted(maps.Keys(run.Labels)) {
		v.Labels = append(v.Labels, label{Name: name, Value: run.Labels[name]})
	}
	runs, _ := h.List.Runs()
	byID := map[string]*client.Run{}
	under := map[string][]*client.Run{}
	for _, r := range runs {
		byID[r.ID] = r
		under[r.Parent] = append(under[r.Parent], r)
	}
	v.Path = pathOf(run, byID, under)
	for _, r := range under[id] {
		v.Children = append(v.Children, childOf(r, at))
	}
	slices.SortFunc(v.Children, func(a, b child) int { return strings.Compare(a.ID, b.ID) })

	v.Reply = h.Client != nil && !run.HasEnded() && !v.Mine
	switch {
	case !run.HasEnded() && v.Mine:
		v.Send = sends(run)
		cancel := cancelOf(id)
		v.Cancel = &cancel
	case !run.HasEnded() && h.Controller:
		v.Cancel = &page.Call{Words: []string{"controller", "cancel"}, Args: []string{id}}
	}
	if !v.Mine {
		v.Waits = waitsOf(run, nil, running(under[id]), at)
		return v, nil
	}

	if err := h.read(ctx, id, run.Step, r); err != nil {
		return v, err
	}
	var st *instance.State
	if r.step != nil && !run.HasEnded() {
		st = &r.step.State
	}
	v.Waits = waitsOf(run, st, running(under[id]), at)
	if r.def != nil {
		marks := marksOf(id, entries, run, st, byID)
		marks.Machines = r.machines
		v.Drawing, err = drawing.Draw(r.def, marks)
		if err != nil {
			return v, err
		}
	}
	if run.Output != nil {
		data, _ := json.Marshal(run.Output, json.Deterministic(true))
		v.Output = indent(data)
	}
	v.Used = strings.Join(run.Used, " ")
	if !run.HasEnded() && len(run.Tickets) > 0 {
		tickets, err := h.Client.Tickets(ctx, id)
		if err != nil {
			return v, err
		}
		for i, w := range v.Waits {
			if w.Ticket != "" {
				v.Waits[i].Value = tickets[w.Ticket]
			}
		}
	}
	if run.HasEnded() {
		files, err := h.Client.Files(ctx, id)
		if err != nil {
			return v, err
		}
		for _, p := range slices.Sorted(maps.Keys(files)) {
			f := files[p]
			v.Files = append(v.Files, file{Path: p, Type: f.Type, Size: size(f.Size), Href: "/run/" + id + "/file?path=" + template.URLQueryEscaper(p)})
		}
	}
	names := map[string]string{}
	for _, e := range entries {
		if r := byID[e.ID]; r != nil {
			names[e.ID] = r.Machine
		}
	}
	for _, e := range entries {
		v.Lines = append(v.Lines, lines(e, names)...)
	}
	return v, nil
}

// sends says whether a run of the UI's client is offered Send: only a
// running statechart is.
func sends(run *client.Run) bool { return !run.HasEnded() && run.Type == "statechart" }

// pathOf gives the runs from a run's root down to it, by parent, and on
// from it to the deepest running run.
func pathOf(run *client.Run, byID map[string]*client.Run, under map[string][]*client.Run) []link {
	path := chainOf(run, under)
	path[0].This = true
	seen := map[string]bool{run.ID: true}
	for up := byID[run.Parent]; up != nil && !seen[up.ID]; up = byID[up.Parent] {
		seen[up.ID] = true
		path = slices.Insert(path, 0, link{ID: up.ID, Machine: up.Machine, Type: up.Type})
	}
	return path
}

// chainOf gives a run and the running runs below it down to the deepest,
// by the newest running child at each level.
func chainOf(r *client.Run, under map[string][]*client.Run) []link {
	var chain []link
	for r != nil {
		chain = append(chain, link{ID: r.ID, Machine: r.Machine, Type: r.Type})
		var next *client.Run
		for _, k := range under[r.ID] {
			if !k.HasEnded() && (next == nil || k.ID > next.ID) {
				next = k
			}
		}
		r = next
	}
	return chain
}

// running gives the runs among kids that have not ended.
func running(kids []*client.Run) []*client.Run {
	return slices.DeleteFunc(slices.Clone(kids), (*client.Run).HasEnded)
}

// waitsOf gives what a running run waits on: each ticket, each running
// child, each job by its name, each timer with when it is due, and events
// at its key where it is a statechart. With no state, as for a run another
// client started, its jobs and timers are only counted.
func waitsOf(run *client.Run, st *instance.State, kids []*client.Run, at time.Time) []wait {
	if run.HasEnded() {
		return nil
	}
	var waits []wait
	for _, t := range run.Tickets {
		waits = append(waits, wait{Text: "ticket " + t, Ticket: t})
	}
	for _, r := range kids {
		waits = append(waits, wait{Text: "child", Run: &link{ID: r.ID, Machine: r.Machine, Type: r.Type}})
	}
	if st == nil {
		if run.Jobs > 0 {
			waits = append(waits, wait{Text: counted(run.Jobs, "job")})
		}
		if run.Timers > 0 {
			waits = append(waits, wait{Text: counted(run.Timers, "timer")})
		}
	} else {
		for _, name := range slices.Sorted(maps.Keys(st.Jobs)) {
			switch j := st.Jobs[name]; {
			case j.Held:
				waits = append(waits, wait{Text: "held " + jobOf(j.Name)})
			case j.Runner != "":
				waits = append(waits, wait{Text: "job " + jobOf(j.Name)})
			}
		}
		timers := slices.SortedFunc(maps.Keys(st.Timers), func(a, b string) int {
			return cmp.Or(st.Timers[a].At.Compare(st.Timers[b].At), strings.Compare(a, b))
		})
		for _, name := range timers {
			waits = append(waits, wait{Text: "timer " + jobOf(name) + " due " + due(st.Timers[name].At, at)})
		}
	}
	if run.Type == "statechart" && run.Key != "" {
		waits = append(waits, wait{Text: "events at key " + run.Key})
	}
	return waits
}

// marksOf gives what a run shows of its state, from the steps of its own
// record and the jobs going, for its drawing.
func marksOf(id string, entries []client.Entry, run *client.Run, st *instance.State, byID map[string]*client.Run) *drawing.Run {
	m := &drawing.Run{Ended: run.HasEnded(), Failed: run.Status == instance.Failed, Children: map[string]drawing.Child{}}
	for _, e := range entries {
		if e.ID != id || e.Step == 0 {
			continue
		}
		s := drawing.Step{Shown: plain(e.Shown)}
		for _, event := range e.Events {
			s.Events = append(s.Events, event.Name)
			if event.Name == machine.Failed && event.From != "" {
				s.Failed = append(s.Failed, jobOf(event.From))
			}
		}
		asks := asksOf(e.Asks)
		for _, a := range e.Asks {
			if a.Kind != instance.AskScript && a.Kind != instance.AskChild {
				continue
			}
			name := jobOf(a.Name)
			if name == instance.AskName && len(asks) > 0 {
				name, asks = asks[0], asks[1:]
			}
			s.Asked = append(s.Asked, name)
			if r := byID[a.Child]; r != nil {
				m.Children[name] = drawing.Child{ID: r.ID, Machine: r.Machine, Type: r.Type}
			}
		}
		m.Steps = append(m.Steps, s)
	}
	if st != nil {
		for _, key := range slices.Sorted(maps.Keys(st.Jobs)) {
			j := st.Jobs[key]
			name := jobOf(j.Name)
			if name == instance.AskName && len(j.ForAsk) > 0 && st.Jobs[j.ForAsk[0]] != nil {
				name = askAt(&st.Jobs[j.ForAsk[0]].Job)
			}
			m.Going = append(m.Going, name)
			if j.Held {
				m.Held = append(m.Held, name)
			}
		}
	}
	return m
}

// asksOf gives where each ask a step starts is written, in the order the
// asks start: the order in which the step held the first job of each, less
// those whose jobs the step denied.
func asksOf(asks []instance.Ask) []string {
	var places []string
	denied := map[string]bool{}
	for _, a := range asks {
		if a.Kind == instance.AskFail {
			denied[a.Name] = true
		}
	}
	started := map[string]bool{}
	for _, a := range asks {
		if a.Kind != instance.AskHold || a.Job == nil || denied[a.Name] {
			continue
		}
		if at := askAt(a.Job); !started[at] {
			started[at] = true
			places = append(places, at)
		}
	}
	return places
}

// askAt gives where the ask a held job goes to is written, as the drawing
// names an ask: the machine's own is `ask`.
func askAt(job *machine.Job) string {
	if job.AskAt == "" {
		return instance.AskName
	}
	return job.AskAt
}

// jobOf gives the name of what asked for a job or set a timer, as the
// definition writes it: without what tells its calls, attempts, items and
// states apart.
func jobOf(name string) string {
	if i := strings.IndexAny(name, "#@~[."); i > 0 {
		return name[:i]
	}
	return name
}

func childOf(r *client.Run, at time.Time) child {
	c := child{ID: r.ID, Status: r.Status, Failure: r.Failure, Machine: r.Machine, Type: r.Type, Ended: r.HasEnded()}
	c.When = when(r.Started, at)
	if r.HasEnded() {
		c.When, c.Took = when(r.Ended, at), took(r.Ended.Sub(r.Started))
	}
	return c
}

// clock is a time as a run's page shows it: the time of day near now, and
// the date and the time further off.
func clock(t, at time.Time) string {
	t = t.Local()
	if d := at.Sub(t); d < 20*time.Hour && d > -20*time.Hour {
		return t.Format("15:04:05")
	}
	return t.Format("2 Jan 15:04")
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
