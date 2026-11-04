package runs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/zummed/corbie-ui/page"
	"github.com/zummed/corbie/client"
	"github.com/zummed/corbie/identity"
	"github.com/zummed/corbie/instance"
	"github.com/zummed/corbie/machine"
	"github.com/zummed/corbie/store"
)

// A narrowing is what the address of the Runs page holds: the runs it
// narrows to, and the window of the runs that ended.
type narrowing struct {
	Client, Window string
	Labels         []string
}

// A window is a length of time the ended runs are listed for.
type window struct {
	Name string
	Long time.Duration
}

var windows = []window{{"1h", time.Hour}, {"6h", 6 * time.Hour}, {"24h", 24 * time.Hour}, {"7d", 7 * 24 * time.Hour}}

func narrowingOf(q url.Values) narrowing {
	n := narrowing{Client: q.Get("client"), Window: q.Get("window"), Labels: strings.Fields(q.Get("label"))}
	if !slices.ContainsFunc(windows, func(w window) bool { return w.Name == n.Window }) {
		n.Window = "6h"
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
	set("client", n.Client, "")
	set("label", strings.Join(n.Labels, " "), "")
	set("window", n.Window, "6h")
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
func (n narrowing) keeps(r *client.Run) bool {
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
	Narrowing                   narrowing
	Clients                     []choice
	Busy, Idle, Services, Ended section
}

type choice struct {
	Name, Href string
	Current    bool
}

// A section is one panel of lines: its part's ID, its name, and what its
// head offers.
type section struct {
	ID, Name string
	Lines    []rootLine
	Windows  []choice
	Cancel   *cancelling
}

// cancelling is the idle runs on old versions the UI may cancel, and the
// commands that cancel them.
type cancelling struct {
	IDs      []string
	Commands string
}

// A rootLine is one root run: the chain of running runs from it to the
// deepest, what the deepest waits on, and the rows it unfolds to.
type rootLine struct {
	ID, Status, Failure, Title, Machine, Type, When, Took string
	Chain                                                 []link
	folded
	Shown, Waits string
	Old, Ended   bool
	Under        int
	Rows         []row
	// More is how many of the runs it started are not rows, and are
	// listed on its page.
	More int
}

// folded is a chain as a line shows it: a chain of more than three is its
// first, an ellipsis holding the whole chain, and its last.
type folded struct {
	Front, Back []link
	Between     string
}

func fold(chain []link) folded {
	if len(chain) <= 3 {
		return folded{Front: chain}
	}
	var names []string
	for _, c := range chain {
		names = append(names, c.Machine)
	}
	return folded{Front: chain[:1], Between: strings.Join(names, " › "), Back: chain[len(chain)-1:]}
}

// A row is one run a root started, as its line unfolds: its name, the chain
// below it, what it ended with or what the deepest waits on, how many runs
// are under it, when it started and how long it took. The root's own row,
// where it runs, comes first.
type row struct {
	ID, Status, Failure, Name, Machine, Type string
	Own                                      bool
	folded
	Shown, Says, When, Took string
	Under                   int
}

// most is how many runs a root started are rows of its line.
const most = 20

// nameOf is how a line or a row names a run: by its key, or by its
// machine where it has none.
func nameOf(r *client.Run) string {
	if r.Key != "" {
		return r.Key
	}
	return r.Machine
}

// listOf gives the Runs page from the runs and the latest step of each run
// of the UI's client, mine, that the page shows.
func listOf(runs []*client.Run, stepOf func(*client.Run) *instance.Step, n narrowing, at time.Time, mine string) listView {
	v := listView{Narrowing: n,
		Busy:     section{ID: "busy", Name: "Busy"},
		Idle:     section{ID: "idle", Name: "Idle"},
		Services: section{ID: "services", Name: "Services"},
		Ended:    section{ID: "ended", Name: "Ended"},
	}
	clients := map[string]bool{}
	byID := map[string]*client.Run{}
	under := map[string][]*client.Run{}
	// The current version of a file, for each client, is that of the
	// newest run with no parent that the client keyed by it.
	current := map[[2]string]*client.Run{}
	for _, r := range runs {
		clients[store.InstanceClient(r.ID)] = true
		byID[r.ID] = r
		under[r.Parent] = append(under[r.Parent], r)
		if file, ok := strings.CutPrefix(r.Key, client.KeyFile); ok && r.Parent == "" {
			at := [2]string{store.InstanceClient(r.ID), file}
			if c := current[at]; c == nil || r.Started.After(c.Started) {
				current[at] = r
			}
		}
	}
	for _, kids := range under {
		slices.SortFunc(kids, func(a, b *client.Run) int { return strings.Compare(a.ID, b.ID) })
	}
	for _, c := range slices.Sorted(maps.Keys(clients)) {
		v.Clients = append(v.Clients, choice{Name: c, Href: n.with(func(m *narrowing) { m.Client = c }), Current: n.Client == c})
	}
	for _, w := range windows {
		v.Ended.Windows = append(v.Ended.Windows, choice{Name: w.Name, Href: n.with(func(m *narrowing) { m.Window = w.Name }), Current: n.Window == w.Name})
	}
	old := func(r *client.Run) bool {
		c := current[[2]string{store.InstanceClient(r.ID), r.Labels[client.LabelMachine]}]
		return c != nil && r.Labels[client.LabelHash] != c.Labels[client.LabelHash]
	}
	from := at.Add(-n.long())
	var roots, ended []*client.Run
	for _, r := range runs {
		parent := byID[r.Parent]
		switch {
		case !n.keeps(r):
		case !r.HasEnded() && (parent == nil || parent.HasEnded()):
			roots = append(roots, r)
		case r.HasEnded() && parent == nil && !r.Ended.Before(from):
			ended = append(ended, r)
		}
	}
	slices.SortFunc(roots, func(a, b *client.Run) int { return strings.Compare(b.ID, a.ID) })
	slices.SortFunc(ended, func(a, b *client.Run) int { return b.Ended.Compare(a.Ended) })

	l := lister{byID: byID, under: under, stepOf: stepOf, from: from, at: at}
	for _, r := range roots {
		line := l.line(r)
		line.Old = old(r)
		line.Chain = descent(r, under, running)
		line.folded = fold(line.Chain)
		line.Shown, line.Waits = l.waits(byID[line.Chain[len(line.Chain)-1].ID], r.Key)
		switch {
		case strings.HasPrefix(r.Key, client.KeyFile):
			v.Services.Lines = append(v.Services.Lines, line)
		case r.Status == instance.Waiting || r.Jobs > 0 || len(r.Tickets) > 0 || len(running(under[r.ID])) > 0:
			v.Busy.Lines = append(v.Busy.Lines, line)
		default:
			v.Idle.Lines = append(v.Idle.Lines, line)
			if line.Old && mine != "" && store.InstanceClient(r.ID) == mine {
				if v.Idle.Cancel == nil {
					v.Idle.Cancel = &cancelling{}
				}
				v.Idle.Cancel.IDs = append(v.Idle.Cancel.IDs, r.ID)
			}
		}
	}
	if c := v.Idle.Cancel; c != nil {
		var commands []string
		for _, id := range c.IDs {
			commands = append(commands, cancelOf(id).String())
		}
		c.Commands = strings.Join(commands, "\n$ ")
	}
	for _, r := range ended {
		line := l.line(r)
		line.When = page.When(r.Ended, at)
		v.Ended.Lines = append(v.Ended.Lines, line)
	}
	return v
}

// A lister gives the lines of the Runs page from the runs under each run
// and the latest steps of the client's own.
type lister struct {
	byID   map[string]*client.Run
	under  map[string][]*client.Run
	stepOf func(*client.Run) *instance.Step
	from   time.Time
	at     time.Time
}

// line gives a root's line, with the rows it unfolds to: its own row where
// it runs, then the runs it started that run or ended in the window,
// newest first, the first twenty of them.
func (l lister) line(r *client.Run) rootLine {
	line := rootLine{ID: r.ID, Status: r.Status, Failure: r.Failure, Title: nameOf(r), Machine: r.Machine, Type: r.Type, Ended: r.HasEnded(), Under: l.count(r)}
	if r.HasEnded() {
		line.Took = took(r.Ended.Sub(r.Started))
	} else {
		line.When = page.When(r.Started, l.at)
		own := row{ID: r.ID, Status: r.Status, Failure: r.Failure, Name: r.Machine, Machine: r.Machine, Type: r.Type, Own: true, When: line.When}
		own.Shown, own.Says = l.waits(r, r.Key)
		line.Rows = append(line.Rows, own)
	}
	kids, shown := l.under[r.ID], 0
	for i := len(kids) - 1; i >= 0; i-- {
		switch k := kids[i]; {
		case k.HasEnded() && k.Ended.Before(l.from):
		case shown == most:
			line.More++
		default:
			line.Rows = append(line.Rows, l.row(k, r.Key))
			shown++
		}
	}
	return line
}

// row gives a run a root started: a running one with the chain of running
// runs below it and what the deepest waits on, an ended one with the chain
// of the newest run at each level below it and what it ended with.
func (l lister) row(r *client.Run, key string) row {
	w := row{ID: r.ID, Status: r.Status, Failure: r.Failure, Name: nameOf(r), Machine: r.Machine, Type: r.Type, Under: l.count(r), When: page.When(r.Started, l.at)}
	if !r.HasEnded() {
		chain := descent(r, l.under, running)
		w.folded = fold(chain)
		w.Shown, w.Says = l.waits(l.byID[chain[len(chain)-1].ID], key)
		return w
	}
	w.folded = fold(descent(r, l.under, all))
	w.Took = took(r.Ended.Sub(r.Started))
	w.Says = endOf(l.stepOf(r))
	return w
}

// waits gives the states a running run is in and what it waits on, from
// its latest step where the UI's client started it, events at the root's
// key left out.
func (l lister) waits(r *client.Run, key string) (shown, says string) {
	step := l.stepOf(r)
	return shownOf(step), inWords(listed(waitsOf(r, stateOf(step), running(l.under[r.ID]), l.at), key))
}

// count gives how many runs are under a run.
func (l lister) count(r *client.Run) int {
	n := 0
	for _, k := range l.under[r.ID] {
		n += 1 + l.count(k)
	}
	return n
}

// endOf gives what a run ended with, from its last step: its error, or
// the last event it took, as its record writes it.
func endOf(step *instance.Step) string {
	switch {
	case step == nil:
		return ""
	case step.State.Error != nil:
		return step.State.Error.Error
	case len(step.Events) == 0:
		return ""
	}
	e := step.Events[len(step.Events)-1]
	if e.From != "" {
		return e.Name + " from " + e.From
	}
	return e.Name
}

// descent gives a run and, at each level below it, the newest of the runs
// kids leaves.
func descent(r *client.Run, under map[string][]*client.Run, kids func([]*client.Run) []*client.Run) []link {
	var chain []link
	for r != nil {
		chain = append(chain, link{ID: r.ID, Machine: r.Machine, Type: r.Type})
		left := kids(under[r.ID])
		r = nil
		if len(left) > 0 {
			r = left[len(left)-1]
		}
	}
	return chain
}

// all gives every run of kids.
func all(kids []*client.Run) []*client.Run { return kids }

// listed gives waits as a root's line lists them: events at the root's key
// are left out, since the line's title is that key.
func listed(waits []wait, key string) []wait {
	return slices.DeleteFunc(waits, func(w wait) bool { return key != "" && w.Text == "events at key "+key })
}

// shownOf gives the states or stages a run is in, or what an agent waits
// for, from what its type shows of its latest step.
func shownOf(step *instance.Step) string {
	if step == nil {
		return ""
	}
	t, ok := machine.TypeOf(step.State.Type)
	if !ok {
		return ""
	}
	shown, _ := plain(t.Show(nil, step.State.Inner)).(map[string]any)
	var names []string
	if states, ok := shown["states"].([]any); ok {
		for _, s := range states {
			names = append(names, fmt.Sprint(s))
		}
	}
	if stages, ok := shown["stages"].(map[string]any); ok {
		for _, name := range slices.Sorted(maps.Keys(stages)) {
			if stages[name] == instance.Running {
				names = append(names, name)
			}
		}
	}
	if waiting, ok := shown["waiting"].(string); ok && waiting != "" {
		names = append(names, waiting)
	}
	return strings.Join(names, ", ")
}

// stateOf gives the state of a step, where there is one.
func stateOf(step *instance.Step) *instance.State {
	if step == nil {
		return nil
	}
	return &step.State
}

// counted is a thing and how many of it there are, where there are more
// than one.
func counted(n int, what string) string {
	if n == 1 {
		return what
	}
	return plural(n, what)
}


// cancelOf is the command that cancels a run the UI's client started.
func cancelOf(id string) page.Call {
	return page.Call{Words: []string{"run", "cancel"}, Args: []string{id}}
}

// cancel cancels runs the UI's client started, with one `corbie run
// cancel` for each, and answers with what they printed.
func (h *handler) cancel(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	ids := r.Form["id"]
	for _, id := range ids {
		if store.CheckInstance(id) != nil || !h.mine(id) {
			page.Error(w, http.StatusForbidden, "only the client that started a run cancels it: "+id)
			return
		}
	}
	var said, failed []string
	count := map[string]int{}
	for _, id := range ids {
		answer := h.Command(r.Context(), cancelOf(id))
		if answer.Status != 0 {
			failed = append(failed, id+": "+answer.Error())
			continue
		}
		text := receipt(answer)
		if count[text] == 0 {
			said = append(said, text)
		}
		count[text]++
	}
	for i, text := range said {
		said[i] = fmt.Sprintf("%s: %d", text, count[text])
	}
	if len(failed) > 0 {
		page.Error(w, http.StatusConflict, strings.Join(append(said, failed...), "; "))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, strings.Join(said, "; "))
}

// steps keeps the latest step of each run of the UI's client that the
// Runs page shows, by the step of its head it was read at: a step never
// changes once it is written.
type steps struct {
	mu   sync.Mutex
	read map[string]stepRead
}

type stepRead struct {
	at   int
	step *instance.Step
}

// A stepReader reads the steps one drawing of the Runs page shows. The
// page is drawn by one reader at a time.
type stepReader struct {
	ctx  context.Context
	c    *client.Client
	s    *steps
	runs []*client.Run
	kept map[string]stepRead
	errs []error
}

// reader reads steps of the runs listed, as one drawing of the page needs.
func (s *steps) reader(ctx context.Context, c *client.Client, runs []*client.Run) *stepReader {
	s.mu.Lock()
	return &stepReader{ctx: ctx, c: c, s: s, runs: runs, kept: map[string]stepRead{}}
}

// of gives the latest step of a run the client started, read again only
// where its head has moved on, and nothing for another client's run.
func (r *stepReader) of(run *client.Run) *instance.Step {
	if r.c == nil || r.c.ID == nil || store.InstanceClient(run.ID) != r.c.ID.Name {
		return nil
	}
	got, ok := r.kept[run.ID]
	if !ok {
		got, ok = r.s.read[run.ID]
	}
	if !ok || got.at != run.Step {
		step, err := instance.Latest(r.ctx, r.c.Store, r.c.Controller, r.c.ID, run.ID, run.Step)
		if err != nil && !errors.Is(err, identity.ErrNotForUs) {
			r.errs = append(r.errs, err)
			return nil
		}
		got = stepRead{at: run.Step, step: step}
	}
	r.kept[run.ID] = got
	return got.step
}

// done keeps the steps of the runs still listed, forgets the rest, and
// gives what could not be read.
func (r *stepReader) done() error {
	for _, run := range r.runs {
		if got, ok := r.s.read[run.ID]; ok && r.kept[run.ID] == (stepRead{}) {
			r.kept[run.ID] = got
		}
	}
	r.s.read = r.kept
	r.s.mu.Unlock()
	return errors.Join(r.errs...)
}

// middle cuts a long title in its middle, where keys that differ at their
// end keep it.
func middle(s string) string {
	const most = 32
	if r := []rune(s); len(r) > most {
		return string(r[:most/2-1]) + "…" + string(r[len(r)-most/2:])
	}
	return s
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

func pluralWord(n int, one string) string {
	if n == 1 {
		return one
	}
	return one + "s"
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
