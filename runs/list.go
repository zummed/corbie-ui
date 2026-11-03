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
// deepest, what the deepest waits on, and the tree it unfolds to.
type rootLine struct {
	ID, Status, Failure, Title, Machine, Type, When, Took string
	Chain                                                 []link
	Shown, Waits                                          string
	Old, Ended                                            bool
	Under                                                 int
	Tree                                                  []node
}

// A node is one run of a tree, indented by its depth under the root.
type node struct {
	ID, Status, Failure, Machine, Type, Shown, Waits, When string
	Depth                                                  int
}

// listOf gives the Runs page from the runs and the latest step of each
// running run the UI's client, mine, started.
func listOf(runs []*client.Run, steps map[string]*instance.Step, n narrowing, at time.Time, mine string) listView {
	v := listView{Narrowing: n,
		Busy:     section{ID: "busy", Name: "Busy"},
		Idle:     section{ID: "idle", Name: "Idle"},
		Services: section{ID: "services", Name: "Services"},
		Ended:    section{ID: "ended", Name: "Ended"},
	}
	clients := map[string]bool{}
	byID := map[string]*client.Run{}
	under := map[string][]*client.Run{}
	// The current version of a file is that of the newest root keyed by it.
	current := map[string]*client.Run{}
	for _, r := range runs {
		clients[store.InstanceClient(r.ID)] = true
		byID[r.ID] = r
		under[r.Parent] = append(under[r.Parent], r)
		if file, ok := strings.CutPrefix(r.Key, client.KeyFile); ok && r.Parent == "" {
			if c := current[file]; c == nil || r.ID > c.ID {
				current[file] = r
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
		c := current[r.Labels[client.LabelMachine]]
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

	for _, r := range roots {
		l := lineOf(r, at)
		l.Old = old(r)
		l.Chain = chainOf(r, under)
		deepest := byID[l.Chain[len(l.Chain)-1].ID]
		l.Shown = shownOf(steps[deepest.ID])
		l.Waits = inWords(waitsOf(deepest, stateOf(steps[deepest.ID]), running(under[deepest.ID]), at))
		l.Tree = treeOf(r, under, running, steps, at)
		switch {
		case strings.HasPrefix(r.Key, client.KeyFile):
			v.Services.Lines = append(v.Services.Lines, l)
		case r.Status == instance.Waiting || r.Jobs > 0 || len(r.Tickets) > 0 || len(running(under[r.ID])) > 0:
			v.Busy.Lines = append(v.Busy.Lines, l)
		default:
			v.Idle.Lines = append(v.Idle.Lines, l)
			if l.Old && mine != "" && store.InstanceClient(r.ID) == mine {
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
	all := func(kids []*client.Run) []*client.Run { return kids }
	for _, r := range ended {
		l := lineOf(r, at)
		l.Tree = treeOf(r, under, all, steps, at)
		l.Under = len(l.Tree) - 1
		v.Ended.Lines = append(v.Ended.Lines, l)
	}
	return v
}

func lineOf(r *client.Run, at time.Time) rootLine {
	l := rootLine{ID: r.ID, Status: r.Status, Failure: r.Failure, Title: r.Key, Machine: r.Machine, Type: r.Type, Ended: r.HasEnded()}
	if l.Title == "" {
		l.Title = r.Machine
	}
	if r.HasEnded() {
		l.When, l.Took = when(r.Ended, at), took(r.Ended.Sub(r.Started))
	} else if !r.Started.IsZero() {
		l.When = when(r.Started, at)
	}
	return l
}

// treeOf gives a root and the runs under it that kids leaves, each under
// its parent, oldest first.
func treeOf(root *client.Run, under map[string][]*client.Run, kids func([]*client.Run) []*client.Run, steps map[string]*instance.Step, at time.Time) []node {
	var tree []node
	var add func(r *client.Run, depth int)
	add = func(r *client.Run, depth int) {
		n := node{ID: r.ID, Status: r.Status, Failure: r.Failure, Machine: r.Machine, Type: r.Type, Depth: depth}
		if !r.HasEnded() {
			n.Shown = shownOf(steps[r.ID])
			n.Waits = inWords(waitsOf(r, stateOf(steps[r.ID]), running(under[r.ID]), at))
		}
		if !r.Started.IsZero() {
			n.When = when(r.Started, at)
		}
		tree = append(tree, n)
		for _, k := range kids(under[r.ID]) {
			add(k, depth+1)
		}
	}
	add(root, 0)
	return tree
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

// due is when a timer is due: the time of day, with the date when it is
// not today.
func due(t, at time.Time) string {
	t = t.Local()
	if y, m, d := at.Local().Date(); t.Year() == y && t.Month() == m && t.Day() == d {
		return t.Format("15:04")
	}
	return t.Format("2 Jan 15:04")
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
	h.List.Poke()
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

// steps keeps the latest step of each running run the UI's client
// started, by the step of its head it was read at: a step never changes
// once it is written.
type steps struct {
	mu   sync.Mutex
	read map[string]stepRead
}

type stepRead struct {
	at   int
	step *instance.Step
}

// of gives the latest step of each running run the client started, read
// again only where its head has moved on.
func (s *steps) of(ctx context.Context, c *client.Client, runs []*client.Run) (map[string]*instance.Step, error) {
	if c == nil || c.ID == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	kept, out := map[string]stepRead{}, map[string]*instance.Step{}
	for _, r := range runs {
		if r.HasEnded() || store.InstanceClient(r.ID) != c.ID.Name {
			continue
		}
		got, ok := s.read[r.ID]
		if !ok || got.at != r.Step {
			step, err := instance.Latest(ctx, c.Store, c.Controller, c.ID, r.ID, r.Step)
			if err != nil && !errors.Is(err, identity.ErrNotForUs) {
				return nil, err
			}
			got = stepRead{at: r.Step, step: step}
		}
		kept[r.ID], out[r.ID] = got, got.step
	}
	s.read = kept
	return out, nil
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
