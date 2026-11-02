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

func ids(rows []row) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

func Test_runs_list_running_then_ended_in_the_window(t *testing.T) {
	parent := run("a-client/1", "nightly", "statechart", "running", 3*time.Hour, 0, nil)
	child := run("a-client/2", "report", "flow", "running", time.Hour, 0, func(r *client.Run) { r.Parent = parent.ID })
	endedChild := run("a-client/3", "report", "flow", "done", 2*time.Hour, 90*time.Minute, func(r *client.Run) { r.Parent = parent.ID })
	later := run("b-client/4", "digest", "flow", "running", 10*time.Minute, 0, nil)
	old := run("b-client/5", "digest", "flow", "done", 30*time.Hour, 29*time.Hour, nil)
	recent := run("b-client/6", "digest", "flow", "failed", 20*time.Minute, 15*time.Minute, func(r *client.Run) { r.Failure = "error" })
	v := listOf([]*client.Run{parent, child, endedChild, later, old, recent}, narrowingOf(url.Values{}), at)
	if got := strings.Join(ids(v.Running), " "); got != "b-client/4 a-client/1 a-client/2" {
		t.Errorf("the running runs are listed as %s", got)
	}
	if !v.Running[2].Child || v.Running[1].Child {
		t.Errorf("the running child is not listed under its running parent")
	}
	if got := strings.Join(ids(v.Ended), " "); got != "b-client/6 a-client/3" {
		t.Errorf("the runs that ended in the window are listed as %s", got)
	}
	if v.Ended[0].Status != "failed" || v.Ended[0].Failure != "error" || v.Ended[0].Took != "5m 0s" {
		t.Errorf("an ended run is listed as %+v", v.Ended[0])
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
	v := listOf(runs, narrowingOf(url.Values{"client": {"ci-client"}, "label": {"team=data"}}), at)
	if v.Now.Running != 1 || v.Now.Jobs != 3 || v.Now.Tickets != 0 {
		t.Errorf("the counts beside the timeline are %+v", v.Now)
	}
	if len(v.Running) != 1 || v.Running[0].ID != "ci-client/1" || len(v.Ended) != 1 {
		t.Errorf("the list holds %v and %v", ids(v.Running), ids(v.Ended))
	}
	if len(v.Timeline.Lanes) != 1 || v.Timeline.Lanes[0].Key != "digest" || len(v.Timeline.Lanes[0].Bars) != 2 {
		t.Errorf("the timeline holds %+v", v.Timeline.Lanes)
	}
	v = listOf(runs, narrowingOf(url.Values{"status": {"running"}}), at)
	counts := map[string]int{}
	for _, s := range v.Statuses {
		counts[s.Name] = s.Count
	}
	if counts["All"] != 4 || counts["Running"] != 3 || counts["Done"] != 1 || len(v.Ended) != 0 {
		t.Errorf("narrowed to running, the statuses count %v and %d ended runs are listed", counts, len(v.Ended))
	}
}

func Test_runs_timeline_lanes_and_bars(t *testing.T) {
	runs := []*client.Run{
		run("ci-client/1", "digest", "flow", "running", 2*time.Hour, 0, func(r *client.Run) { r.Labels = map[string]string{"team": "data"} }),
		run("ci-client/2", "digest", "flow", "done", 3*time.Hour, 90*time.Minute, func(r *client.Run) { r.Labels = map[string]string{"team": "data"} }),
		run("laptop-client/3", "door", "statechart", "running", 9*time.Hour, 0, nil),
	}
	v := listOf(runs, narrowingOf(url.Values{}), at)
	lanes := v.Timeline.Lanes
	if len(lanes) != 2 || lanes[0].Key != "digest" || lanes[1].Key != "door" || lanes[0].Type != "flow" {
		t.Fatalf("the lanes by machine are %+v", lanes)
	}
	a, b := lanes[0].Bars[0], lanes[0].Bars[1]
	if a.Top == b.Top || lanes[0].Height <= lanes[1].Height {
		t.Errorf("two runs of digest that overlap are drawn on one line: %+v %+v", a, b)
	}
	door := lanes[1].Bars[0]
	if !door.Clipped || door.Left != 0 || door.Width != 100 {
		t.Errorf("a run that started before the window and runs on is drawn as %+v", door)
	}
	v = listOf(runs, narrowingOf(url.Values{"lanes": {"team"}}), at)
	if len(v.Timeline.Lanes) != 2 || v.Timeline.Lanes[0].Key != "data" || v.Timeline.Lanes[1].Name != "no team=" {
		t.Errorf("the lanes by team= are %+v", v.Timeline.Lanes)
	}
	v = listOf(runs, narrowingOf(url.Values{"lanes": {"client"}}), at)
	if len(v.Timeline.Lanes) != 2 || v.Timeline.Lanes[0].Key != "ci-client" || v.Timeline.Lanes[1].Key != "laptop-client" {
		t.Errorf("the lanes by client are %+v", v.Timeline.Lanes)
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
	for _, want := range []string{tickets["verdict"], "timer too_long, 48h", "<b>&lt;-</b> start", "<b>-&gt;</b> job drafting: a script, on runner", `class="state now"`, `class="edge taken"`, `action="/run/` + id + `/send"`} {
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
