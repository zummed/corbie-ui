package runs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zummed/corbie-ui/page"
	"github.com/zummed/corbie/client"
	"github.com/zummed/corbie/client/cli"
	"github.com/zummed/corbie/identity"
	"github.com/zummed/corbie/machine"
	"github.com/zummed/corbie/permission"
	"github.com/zummed/corbie/runner"
	"github.com/zummed/corbie/store"

	_ "github.com/zummed/corbie/agent"
	_ "github.com/zummed/corbie/flow"
	_ "github.com/zummed/corbie/statechart"
)

// A test's runs run their jobs in processes of this test binary, as a
// runner runs them in processes of the corbie binary.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == runner.JobArg {
		runner.JobMain()
		return
	}
	os.Exit(m.Run())
}

var at = time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)

func run(id, machine, kind, status string, started, ended time.Duration, more func(*client.Run)) *client.Run {
	r := &client.Run{ID: id, Machine: machine, Type: kind, Status: status, Step: 1, Started: at.Add(-started)}
	if ended != 0 {
		r.Ended = at.Add(-ended)
	}
	if more != nil {
		more(r)
	}
	return r
}

func titles(s section) []string {
	var out []string
	for _, l := range s.Lines {
		out = append(out, l.Title)
	}
	return out
}

func Test_runs_list_roots_by_section(t *testing.T) {
	file := map[string]string{client.LabelMachine: "bridges/slack.yaml", client.LabelHash: "new"}
	was := map[string]string{client.LabelMachine: "bridges/slack.yaml", client.LabelHash: "old"}
	runs := []*client.Run{
		run("a-client/01", "slack-bridge", "flow", "cancelled", 9*time.Hour, 5*time.Hour, func(r *client.Run) { r.Key, r.Labels = "file:bridges/slack.yaml", was }),
		run("a-client/02", "slack-bridge", "flow", "running", 5*time.Hour, 0, func(r *client.Run) { r.Key, r.Labels = "file:bridges/slack.yaml", file }),
		run("a-client/03", "slack-listen", "statechart", "running", 5*time.Hour, 0, func(r *client.Run) { r.Parent, r.Jobs, r.Labels = "a-client/02", 1, file }),
		run("a-client/04", "chat", "statechart", "running", 4*time.Hour, 0, func(r *client.Run) { r.Key, r.Labels = "slack:C1/1", was }),
		run("a-client/05", "message", "flow", "running", 4*time.Hour, 0, func(r *client.Run) { r.Parent, r.Labels = "a-client/04", was }),
		run("a-client/06", "question", "statechart", "running", 3*time.Hour, 0, func(r *client.Run) { r.Parent, r.Key, r.Timers = "a-client/05", "question:06", 1 }),
		run("a-client/07", "question", "statechart", "running", 2*time.Hour, 0, func(r *client.Run) { r.Parent, r.Timers = "a-client/05", 1 }),
		run("a-client/08", "who", "flow", "done", 4*time.Hour, 4*time.Hour, func(r *client.Run) { r.Parent = "a-client/05" }),
		run("a-client/09", "chat", "statechart", "running", 3*time.Hour, 0, func(r *client.Run) { r.Key, r.Timers, r.Labels = "slack:C1/2", 1, was }),
		run("a-client/10", "chat", "statechart", "running", time.Hour, 0, func(r *client.Run) { r.Key, r.Timers, r.Labels = "slack:C1/3", 1, file }),
		run("b-client/11", "door", "statechart", "waiting", 0, 0, nil),
		run("b-client/12", "digest", "flow", "failed", 20*time.Minute, 15*time.Minute, func(r *client.Run) { r.Failure = "error" }),
		run("b-client/13", "digest", "flow", "done", 30*time.Hour, 29*time.Hour, nil),
		run("b-client/14", "fetch", "flow", "done", 19*time.Minute, 18*time.Minute, func(r *client.Run) { r.Parent = "b-client/12" }),
	}
	v := listOf(runs, nil, narrowingOf(url.Values{}), at, "a-client")
	for _, c := range []struct {
		s    section
		want string
	}{{v.Busy, "door slack:C1/1"}, {v.Idle, "slack:C1/3 slack:C1/2"}, {v.Services, "file:bridges/slack.yaml"}, {v.Ended, "digest file:bridges/slack.yaml"}} {
		if got := strings.Join(titles(c.s), " "); got != c.want {
			t.Errorf("%s holds %s, not %s", c.s.Name, got, c.want)
		}
	}
	busy := v.Busy.Lines[1]
	var chain []string
	for _, l := range busy.Chain {
		chain = append(chain, l.Machine)
	}
	if strings.Join(chain, " ") != "chat message question" || busy.Chain[2].ID != "a-client/07" || busy.Waits != "timer" {
		t.Errorf("the chain to the deepest running run is %v, waiting on %q", busy.Chain, busy.Waits)
	}
	if len(busy.Tree) != 4 || busy.Tree[2].ID != "a-client/06" || busy.Tree[2].Depth != 2 || busy.Tree[1].Waits != "child question, child question" || busy.Tree[2].Waits != "timer, events at key question:06" {
		t.Errorf("the line unfolds to %+v", busy.Tree)
	}
	if !busy.Old || !v.Idle.Lines[1].Old || v.Idle.Lines[0].Old || v.Services.Lines[0].Old {
		t.Errorf("the runs on old versions are not the ones marked")
	}
	if got := v.Services.Lines[0].Waits; got != "job" {
		t.Errorf("a service's line waits on %q", got)
	}
	digest := v.Ended.Lines[0]
	if digest.Status != "failed" || digest.Failure != "error" || digest.Took != "5m 0s" || digest.Under != 1 {
		t.Errorf("an ended run is listed as %+v", digest)
	}
}

func Test_runs_own_runs_wait_by_name(t *testing.T) {
	l, viewer, _ := local(t)
	h := New(Config{List: &fixed{from: viewer}, Lister: viewer, Client: l.Client, Interval: time.Second, Shell: page.Shell{}})
	body := get(t, h, "/runs")
	for _, want := range []string{`review</a><span class="dim">[waiting]</span>`, "ticket verdict, timer too_long due "} {
		if !strings.Contains(body, want) {
			t.Errorf("the line of the client's own run does not show %s:\n%s", want, body)
		}
	}
}

func Test_runs_narrowing_holds_for_the_whole_page(t *testing.T) {
	data := map[string]string{"team": "data"}
	runs := []*client.Run{
		run("ci-client/1", "digest", "flow", "running", time.Hour, 0, func(r *client.Run) { r.Labels = data; r.Jobs = 3 }),
		run("ci-client/2", "digest", "flow", "done", 2*time.Hour, time.Hour, func(r *client.Run) { r.Labels = data }),
		run("ci-client/3", "prices", "flow", "running", time.Hour, 0, func(r *client.Run) { r.Jobs = 1 }),
		run("laptop-client/4", "review", "statechart", "running", time.Hour, 0, func(r *client.Run) { r.Labels = data; r.Tickets = []string{"verdict"} }),
	}
	v := listOf(runs, nil, narrowingOf(url.Values{"client": {"ci-client"}, "label": {"team=data"}}), at, "")
	if got := titles(v.Busy); len(got) != 1 || v.Busy.Lines[0].ID != "ci-client/1" || len(v.Ended.Lines) != 1 || len(v.Idle.Lines) != 0 {
		t.Errorf("the page holds %v and %v", got, titles(v.Ended))
	}
	if len(v.Clients) != 2 || !v.Clients[0].Current {
		t.Errorf("the clients offered are %+v", v.Clients)
	}
	v = listOf(runs, nil, narrowingOf(url.Values{"label": {"team"}, "window": {"1h"}}), at, "")
	if got := strings.Join(titles(v.Busy), " "); got != "review digest" || len(v.Ended.Lines) != 1 {
		t.Errorf("narrowed to team in the last hour, the page holds %s and %v", got, titles(v.Ended))
	}
}

func Test_runs_cancel_idle_runs_on_old_versions(t *testing.T) {
	was := map[string]string{client.LabelMachine: "chat.yaml", client.LabelHash: "old"}
	runs := []*client.Run{
		run("lab-client/1", "chat", "statechart", "done", 9*time.Hour, 8*time.Hour, func(r *client.Run) {
			r.Key, r.Labels = "file:chat.yaml", map[string]string{client.LabelHash: "new"}
		}),
		run("lab-client/2", "chat", "statechart", "running", 7*time.Hour, 0, func(r *client.Run) { r.Timers, r.Labels = 1, was }),
		run("lab-client/3", "chat", "statechart", "running", 6*time.Hour, 0, func(r *client.Run) { r.Timers, r.Labels = 1, was }),
		run("lab-client/4", "chat", "statechart", "running", 6*time.Hour, 0, func(r *client.Run) { r.Tickets, r.Labels = []string{"go"}, was }),
		run("ci-client/5", "chat", "statechart", "running", 6*time.Hour, 0, func(r *client.Run) { r.Timers, r.Labels = 1, was }),
	}
	v := listOf(runs, nil, narrowingOf(url.Values{}), at, "lab-client")
	c := v.Idle.Cancel
	if c == nil || strings.Join(c.IDs, " ") != "lab-client/3 lab-client/2" || c.Commands != "corbie run cancel lab-client/3\n$ corbie run cancel lab-client/2" {
		t.Fatalf("Cancel on the idle runs on old versions is %+v", c)
	}
	if v = listOf(runs, nil, narrowingOf(url.Values{}), at, ""); v.Idle.Cancel != nil {
		t.Errorf("with no client the page offers %+v", v.Idle.Cancel)
	}

	var calls []string
	command := func(_ context.Context, c page.Call) page.Answer {
		calls = append(calls, c.String())
		return page.Answer{Out: `{"taken":true}`}
	}
	mine, err := identity.New("lab-client", identity.Client)
	if err != nil {
		t.Fatal(err)
	}
	list := &fixed{from: client.New(nil, identity.Card{}, store.NewMemory().As(store.Party{Kind: store.Viewer}))}
	h := New(Config{List: list, Client: client.New(mine, identity.Card{}, nil), Command: command, Shell: page.Shell{}})
	post := func(ids ...string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/runs/cancel", strings.NewReader(url.Values{"id": ids}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		h.ServeHTTP(w, r)
		return w
	}
	own := "lab-client/20261015T091204.118Z-3be1c07a2f"
	if w := post(own, own); w.Code != 200 || w.Body.String() != "taken: 2" || len(calls) != 2 || calls[0] != "corbie run cancel "+own {
		t.Errorf("Cancel answered %d %q and ran %v", w.Code, w.Body.String(), calls)
	}
	calls = nil
	if w := post(own, "ci-client/20261015T091204.118Z-3be1c07a2f"); w.Code != http.StatusForbidden || len(calls) != 0 {
		t.Errorf("Cancel with another client's run answered %d and ran %v", w.Code, calls)
	}
}

// fixed is a list that holds the runs of a store at the moment it is
// asked.
type fixed struct {
	mu      sync.Mutex
	from    *client.Client
	changed chan struct{}
}

func (f *fixed) Runs() ([]*client.Run, error) {
	return f.from.List(context.Background(), client.Filter{Finished: true})
}
func (f *fixed) Changed() <-chan struct{} { return f.changed }
func (f *fixed) Poke()                    {}

const review = `statechart: review
initial: drafting
states:
  drafting:
    job: ./write.js
    on: { done: waiting }
  waiting:
    ticket: verdict
    timers: { too_long: 48h }
    on:
      verdict:
        - { go: approved, when: "${event.data.ok}" }
        - { go: drafting }
      too_long: gave_up
  approved: { end: done }
  gave_up: { end: failed }
`

// local runs a store in memory, with a controller and a runner, and starts
// the review on it, which waits on its ticket once its job is done.
func local(t *testing.T) (*client.Local, *client.Client, string) {
	t.Helper()
	ctx := context.Background()
	l, err := client.NewLocal(ctx, permission.Layer{Allow: []string{"all"}}, func(string, error) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	l.Controller.AnyRunner = true
	draft, err := machine.Parse([]byte(review), machine.Given(map[string]string{"write.js": "return 'a draft'"}))
	if err != nil {
		t.Fatal(err)
	}
	id, err := l.Start(ctx, draft, client.Start{})
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(20 * time.Second); ; {
		r, err := l.Show(ctx, id)
		if err == nil && len(r.Tickets) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the review never waited on its ticket: %+v %v", r, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	viewer := client.New(nil, l.Client.Controller, l.Memory.As(store.Party{Kind: store.Viewer, Controller: l.Client.Controller.Name}))
	return l, viewer, id
}

func get(t *testing.T, h http.Handler, path string) string {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s answered %d: %s", path, w.Code, w.Body.String())
	}
	return w.Body.String()
}

func Test_runs_own_run_shown_in_full(t *testing.T) {
	l, viewer, id := local(t)
	h := New(Config{List: &fixed{from: viewer}, Lister: viewer, Client: l.Client, Command: nil, Interval: time.Second, Shell: page.Shell{}})
	body := get(t, h, "/run/"+id)
	tickets, err := l.Tickets(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{tickets["verdict"], "<span>timer too_long due ", "<b>&lt;-</b> start", "<b>-&gt;</b> job drafting: a script, on runner", `<span class="i" title="review">review</span>`, `class="state now"`, `class="edge taken"`, `action="/run/` + id + `/send"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the run's own page does not show %s", want)
		}
	}
}

func Test_runs_other_clients_run_shows_no_record(t *testing.T) {
	_, viewer, id := local(t)
	other, err := identity.New("other-client", identity.Client)
	if err != nil {
		t.Fatal(err)
	}
	someone := client.New(other, viewer.Controller, viewer.Store)
	h := New(Config{List: &fixed{from: viewer}, Lister: viewer, Client: someone, Interval: time.Second, Shell: page.Shell{}})
	body := get(t, h, "/run/"+id)
	if !strings.Contains(body, "running") || !strings.Contains(body, "only the client that started a run can read its record") {
		t.Errorf("another client's run does not show its status alone:\n%s", body)
	}
	for _, never := range []string{"<b>&lt;-</b> start", "ticket verdict </span><code", `class="state now"`, "/send"} {
		if strings.Contains(body, never) {
			t.Errorf("another client's run shows %s", never)
		}
	}
	if !strings.Contains(body, `placeholder="a ticket's value"`) {
		t.Errorf("another client's running run does not take a ticket's value to answer it:\n%s", body[strings.Index(body, `id="waits"`):])
	}
}

func Test_runs_actions_run_their_commands(t *testing.T) {
	var calls []page.Call
	command := func(_ context.Context, c page.Call) page.Answer {
		if found, _ := cli.Find(c.Words); found == nil || found.Name != strings.Join(c.Words, " ") {
			t.Errorf("%q is not one of Corbie's commands", strings.Join(c.Words, " "))
		}
		calls = append(calls, c)
		return page.Answer{Out: `{"taken":true}`}
	}
	mine, err := identity.New("lab-client", identity.Client)
	if err != nil {
		t.Fatal(err)
	}
	list := &fixed{from: client.New(nil, identity.Card{}, store.NewMemory().As(store.Party{Kind: store.Viewer}))}
	h := New(Config{List: list, Client: client.New(mine, identity.Card{}, nil), Command: command, Shell: page.Shell{}})
	own, other := "lab-client/20261015T091204.118Z-3be1c07a2f", "ci-client/20261015T091204.118Z-3be1c07a2f"
	post := func(path string, form url.Values) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		h.ServeHTTP(w, r)
		return w
	}
	if w := post("/run/"+own+"/reply", url.Values{"ticket": {"e346df80"}, "value": {` { "ok" : true } `}}); w.Code != 200 || w.Body.String() != "taken" {
		t.Errorf("Reply answered %d %q", w.Code, w.Body.String())
	}
	post("/run/"+own+"/send", url.Values{"event": {"poke"}, "data": {"[1, 2]"}})
	post("/run/"+own+"/cancel", nil)
	post("/run/"+other+"/reply", url.Values{"ticket": {"77aa"}, "value": {"3"}})
	want := []string{
		"corbie run reply " + own + ` e346df80 {"ok":true}`,
		"corbie run send " + own + " poke [1,2]",
		"corbie run cancel " + own,
		"corbie run reply " + other + " 77aa 3",
	}
	if len(calls) != len(want) {
		t.Fatalf("the actions ran %v", calls)
	}
	for i, c := range calls {
		if c.String() != want[i] {
			t.Errorf("an action ran %q, not %q", c.String(), want[i])
		}
	}
	if w := post("/run/"+other+"/cancel", nil); w.Code != http.StatusForbidden {
		t.Errorf("Cancel on another client's run, with no controller, answered %d", w.Code)
	}
	if w := post("/run/"+other+"/send", url.Values{"event": {"poke"}}); w.Code != http.StatusForbidden {
		t.Errorf("Send on another client's run answered %d", w.Code)
	}
	if w := post("/run/"+own+"/reply", url.Values{"ticket": {"aa"}, "value": {"not json"}}); w.Code != http.StatusBadRequest {
		t.Errorf("an answer that is not JSON answered %d", w.Code)
	}
	for kind, offered := range map[string]bool{"statechart": true, "flow": false, "agent": false} {
		if sends(&client.Run{Type: kind, Status: "running"}) != offered {
			t.Errorf("Send on a running %s is offered: %v", kind, !offered)
		}
	}
	h = New(Config{List: list, Client: client.New(mine, identity.Card{}, nil), Controller: true, Command: command, Shell: page.Shell{}})
	calls = nil
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/run/"+other+"/cancel", nil))
	if len(calls) != 1 || calls[0].String() != "corbie controller cancel "+other {
		t.Errorf("with the controller, Cancel on another client's run ran %v", calls)
	}
}

func Test_runs_result_file_is_a_download(t *testing.T) {
	w := httptest.NewRecorder()
	download(w, "pages[2]", strings.NewReader("<script>alert(1)</script>"))
	h := w.Header()
	if h.Get("Content-Type") != "application/octet-stream" || h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Content-Disposition") != `attachment; filename=pages-2` {
		t.Errorf("a result file is sent with %v", h)
	}
}
