package runs

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zummed/corbie-ui/page"
	"github.com/zummed/corbie/client"
	"github.com/zummed/corbie/client/cli"
	"github.com/zummed/corbie/identity"
	"github.com/zummed/corbie/instance"
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

func noSteps(*client.Run) *instance.Step { return nil }

func lineNames(s section) []string {
	var out []string
	for _, l := range s.Lines {
		out = append(out, l.Name+l.Key)
	}
	return out
}

func Test_runs_list_roots_by_section(t *testing.T) {
	runs := []*client.Run{
		run("a-client/01", "slack-bridge", "flow", "cancelled", 9*time.Hour, 5*time.Hour, func(r *client.Run) { r.Location = "/rook/bridges/slack" }),
		run("a-client/02", "slack-bridge", "flow", "running", 5*time.Hour, 0, func(r *client.Run) { r.Location = "/rook/bridges/slack" }),
		run("a-client/03", "slack-listen", "statechart", "running", 5*time.Hour, 0, func(r *client.Run) { r.Parent, r.Jobs, r.Location = "a-client/02", 1, "/rook/bridges/slack" }),
		run("a-client/04", "chat", "statechart", "running", 4*time.Hour, 0, func(r *client.Run) { r.Location, r.Key = "/rook/chat", "slack:C1/1" }),
		run("a-client/05", "message", "flow", "running", 4*time.Hour, 0, func(r *client.Run) { r.Parent, r.Location = "a-client/04", "/rook/chat" }),
		run("a-client/06", "question", "statechart", "running", 3*time.Hour, 0, func(r *client.Run) { r.Parent, r.Timers, r.Location = "a-client/05", 1, "/rook/chat" }),
		run("a-client/07", "question", "statechart", "running", 2*time.Hour, 0, func(r *client.Run) { r.Parent, r.Timers, r.Location = "a-client/05", 1, "/rook/chat" }),
		run("a-client/08", "who", "flow", "done", 4*time.Hour, 4*time.Hour, func(r *client.Run) { r.Parent, r.Location = "a-client/05", "/rook/chat" }),
		run("a-client/09", "chat", "statechart", "running", 3*time.Hour, 0, func(r *client.Run) { r.Location, r.Key, r.Timers = "/rook/chat", "slack:C1/2", 1 }),
		run("a-client/10", "chat", "statechart", "running", time.Hour, 0, func(r *client.Run) { r.Location, r.Key, r.Timers = "/rook/chat", "slack:C1/3", 1 }),
		run("b-client/11", "door", "statechart", "waiting", 0, 0, func(r *client.Run) { r.Location = "/house/door" }),
		run("b-client/12", "digest", "flow", "failed", 20*time.Minute, 15*time.Minute, func(r *client.Run) { r.Failure, r.Location = "error", "/work/digest" }),
		run("b-client/13", "digest", "flow", "done", 30*time.Hour, 29*time.Hour, func(r *client.Run) { r.Location = "/work/digest" }),
		run("b-client/14", "fetch", "flow", "done", 19*time.Minute, 18*time.Minute, func(r *client.Run) { r.Parent, r.Location = "b-client/12", "/work/digest" }),
	}
	facts := map[string]fixed{
		"a-client/01": {Tree: "/rook", Lifetime: "singleton"},
		"a-client/02": {Tree: "/rook", Lifetime: "singleton"},
		"a-client/04": {Tree: "/rook", Lifetime: "keyed"},
	}
	v := listOf(runs, noSteps, facts, narrowingOf(url.Values{}), at, "a-client")
	for _, c := range []struct {
		s    section
		want string
	}{{v.Busy, "/house/door /rook/chat#slack:C1/1"}, {v.Idle, "/rook/chat#slack:C1/3 /rook/chat#slack:C1/2"}, {v.Singletons, "/rook/bridges/slack"}, {v.Ended, "/work/digest /rook/bridges/slack"}} {
		if got := strings.Join(lineNames(c.s), " "); got != c.want {
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
	if got := v.Singletons.Lines[0].Waits; got != "job" {
		t.Errorf("a singleton's line waits on %q", got)
	}
	digest := v.Ended.Lines[0]
	if digest.Status != "failed" || digest.Failure != "error" || digest.Took != "5m 0s" || digest.Under != 1 {
		t.Errorf("an ended run is listed as %+v", digest)
	}
	if digest.When != page.When(at.Add(-15*time.Minute), at) {
		t.Errorf("an ended line gives the time %q, not when it ended", digest.When)
	}
	// An ended line gives how many runs are under it only where there are
	// any.
	if html, err := render("ended", v); err != nil || !strings.Contains(string(html), "1 run under it") || strings.Contains(string(html), "0 runs") {
		t.Errorf("the ended lines are drawn as %v:\n%s", err, html)
	}
	long := []*client.Run{run("a-client/1", "chat", "statechart", "running", time.Hour, 0, nil)}
	for i, m := range []string{"message", "answer", "helper", "ask", "question"} {
		parent := long[i].ID
		long = append(long, run(fmt.Sprintf("a-client/%d", i+2), m, "flow", "running", time.Hour, 0, func(r *client.Run) { r.Parent = parent }))
	}
	l := listOf(long, noSteps, nil, narrowingOf(url.Values{}), at, "").Busy.Lines[0]
	if len(l.Front) != 1 || l.Between != "chat › message › answer › helper › ask › question" || len(l.Back) != 1 || l.Back[0].Machine != "question" {
		t.Errorf("a chain of six is shown as %v, %q and %v", l.Front, l.Between, l.Back)
	}
	if got := v.Idle.Lines[0].Waits; got != "timer" {
		t.Errorf("an idle keyed run waits on %q", got)
	}
}

func Test_runs_locations_are_listed_by_tree(t *testing.T) {
	runs := []*client.Run{
		run("lab-client/1", "boiler", "statechart", "running", time.Hour, 0, func(r *client.Run) { r.Location, r.Timers = "/home/devices/boiler", 1 }),
		run("lab-client/2", "thermostat", "statechart", "running", time.Hour, 0, func(r *client.Run) { r.Location, r.Key = "/home/rooms/thermostat", "kitchen" }),
		run("lab-client/3", "thermostat", "statechart", "running", time.Hour, 0, func(r *client.Run) { r.Location, r.Key = "/home/rooms/thermostat", "hall" }),
		run("lab-client/4", "away", "flow", "done", time.Hour, time.Minute, func(r *client.Run) { r.Location = "/home/scenes/away" }),
		run("lab-client/5", "step", "flow", "running", time.Hour, 0, func(r *client.Run) { r.Parent, r.Location = "lab-client/1", "/home/devices/boiler" }),
		run("ci-client/6", "digest", "flow", "running", time.Hour, 0, func(r *client.Run) { r.Location, r.Key, r.Jobs = "/work/digest", "daily", 1 }),
		run("ci-client/7", "prices", "flow", "running", time.Hour, 0, func(r *client.Run) { r.Location, r.Jobs = "/work/prices", 1 }),
	}
	facts := map[string]fixed{
		"lab-client/1": {Tree: "/home", Lifetime: "singleton"},
		"lab-client/2": {Tree: "/home", Lifetime: "keyed"},
		"lab-client/3": {Tree: "/home", Lifetime: "keyed"},
		"lab-client/4": {Tree: "/home", Lifetime: "transient"},
		"lab-client/5": {Tree: "/home"},
	}
	v := listOf(runs, noSteps, facts, narrowingOf(url.Values{}), at, "lab-client")
	var got []string
	for _, c := range v.Clients {
		for _, tr := range c.Trees {
			for _, p := range tr.Places {
				got = append(got, fmt.Sprintf("%s %s %s %s %s%s", c.Name, tr.Prefix, p.Name, p.Lifetime, p.Status, p.Says))
			}
		}
	}
	want := []string{
		"ci-client  /work/digest keyed 1 running",
		"ci-client  /work/prices  1 running",
		"lab-client /home devices/boiler singleton running",
		"lab-client /home rooms/thermostat keyed 2 running",
		"lab-client /home scenes/away transient ",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("Locations lists\n%s\nnot\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !v.Every.Current || v.Clients[1].Trees[0].Href != "/runs?at=%2Fhome" || v.Clients[1].Trees[0].Places[1].Href != "/runs?at=%2Fhome%2Frooms%2Fthermostat" {
		t.Errorf("the trees and locations narrow the page as %+v", v.Clients[1].Trees[0])
	}

	// The page draws Locations beside the sections.
	lc, viewer, _ := local(t)
	body := get(t, New(Config{List: &listed{from: viewer}, Lister: viewer, Client: lc.Client, Interval: time.Second, Shell: page.Shell{}}), "/runs")
	if !regexp.MustCompile(`<nav class="panel locs" id="locations"[\s\S]*<a class="tree" href="/runs\?at=%2Freview-[0-9a-f]+"`).MatchString(body) {
		t.Errorf("the page does not list the review's tree in Locations:\n%s", body)
	}
}

func Test_runs_line_unfolds_to_the_runs_its_root_started(t *testing.T) {
	runs := []*client.Run{run("a-client/00", "chat", "statechart", "running", 9*time.Hour, 0, func(r *client.Run) { r.Location, r.Key, r.Timers = "/rook/chat", "slack:C1", 1 })}
	// Thirty messages, one each quarter of an hour: the oldest six ended before
	// the window, and the newest runs.
	for i := range 30 {
		id := fmt.Sprintf("a-client/%02d", i+1)
		ago := time.Duration(30-i) * 15 * time.Minute
		ended := ago - time.Minute
		if i == 29 {
			ended = 0
		}
		status := map[bool]string{true: "running", false: "done"}[ended == 0]
		runs = append(runs, run(id, "message", "flow", status, ago, ended, func(r *client.Run) { r.Parent = "a-client/00" }))
	}
	runs = append(runs,
		run("a-client/40", "answer", "flow", "running", 5*time.Minute, 0, func(r *client.Run) { r.Parent = "a-client/30" }),
		run("a-client/41", "question", "statechart", "running", 4*time.Minute, 0, func(r *client.Run) { r.Parent, r.Tickets = "a-client/40", []string{"verdict"} }),
		run("a-client/42", "attend", "flow", "done", 25*time.Minute, 24*time.Minute, func(r *client.Run) { r.Parent = "a-client/28" }),
		run("a-client/43", "helper", "agent", "done", 24*time.Minute, 23*time.Minute, func(r *client.Run) { r.Parent = "a-client/42" }),
	)
	steps := map[string]*instance.Step{
		"a-client/28": {Events: []instance.Arrived{{Event: machine.Event{Name: "done", From: "attend"}}}},
		"a-client/27": {State: instance.State{Error: &machine.Error{Error: "the model refused"}}},
	}
	l := listOf(runs, func(r *client.Run) *instance.Step { return steps[r.ID] }, nil, narrowingOf(url.Values{}), at, "a-client").Busy.Lines[0]
	if len(l.Rows) != 21 || !l.Rows[0].Own || l.Rows[0].Says != "child message, timer" || l.More != 4 {
		t.Fatalf("the line unfolds to %d rows, the first %+v, with %d more", len(l.Rows), l.Rows[0], l.More)
	}
	var ids []string
	for _, r := range l.Rows[1:] {
		ids = append(ids, r.ID[len("a-client/"):])
	}
	if got := strings.Join(ids, " "); got != "30 29 28 27 26 25 24 23 22 21 20 19 18 17 16 15 14 13 12 11" {
		t.Errorf("the rows are %s, not the newest twenty", got)
	}
	if html, err := render("line", l); err != nil || !strings.Contains(string(html), ">2 runs</span>") || strings.Contains(string(html), "0 runs") || strings.Contains(string(html), "<i>·</i></span>") {
		t.Errorf("a row gives the runs under it where there are none, or %v:\n%s", err, html)
	}
	newest, attended, failed := l.Rows[1], l.Rows[3], l.Rows[4]
	if newest.Name != "message" || len(newest.Front) != 3 || newest.Front[2].Machine != "question" || newest.Says != "ticket verdict" || newest.Under != 2 || newest.Took != "" {
		t.Errorf("a running message is the row %+v", newest)
	}
	if len(attended.Front) != 3 || attended.Front[2].Machine != "helper" || attended.Says != "done from attend" || attended.Under != 2 || attended.Took != "1m 0s" || attended.When != page.When(at.Add(-45*time.Minute), at) {
		t.Errorf("an ended message is the row %+v", attended)
	}
	if failed.Says != "the model refused" {
		t.Errorf("a failed message ended with %q", failed.Says)
	}

	// On the page, a click on the line opens its root's page, and the
	// button at its start unfolds it.
	lc, viewer, _ := local(t)
	body := get(t, New(Config{List: &listed{from: viewer}, Lister: viewer, Client: lc.Client, Interval: time.Second, Shell: page.Shell{}}), "/runs")
	m := regexp.MustCompile(`<summary class="line" data-key="([^"]+)"[^>]*data-href="([^"]+)">`).FindStringSubmatch(body)
	if m == nil || m[2] != "/run/"+m[1] {
		t.Errorf("a line does not open its root's page:\n%s", body)
	}
	if !regexp.MustCompile(`<button class="unfold" type="button"[^>]*></button></summary>`).MatchString(body) {
		t.Errorf("a line has no button that unfolds it:\n%s", body)
	}
}

func Test_runs_own_runs_wait_by_name(t *testing.T) {
	l, viewer, _ := local(t)
	h := New(Config{List: &listed{from: viewer}, Lister: viewer, Client: l.Client, Interval: time.Second, Shell: page.Shell{}})
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
		run("ci-client/1", "digest", "flow", "running", time.Hour, 0, func(r *client.Run) { r.Location, r.Labels, r.Jobs = "/work/digest", data, 3 }),
		run("ci-client/2", "digest", "flow", "done", 2*time.Hour, time.Hour, func(r *client.Run) { r.Location, r.Labels = "/work/digest", data }),
		run("ci-client/3", "prices", "flow", "running", time.Hour, 0, func(r *client.Run) { r.Location, r.Jobs = "/work/prices", 1 }),
		run("ci-client/5", "other", "flow", "running", time.Hour, 0, func(r *client.Run) { r.Location, r.Labels, r.Jobs = "/workshop/x", data, 1 }),
		run("laptop-client/4", "review", "statechart", "running", time.Hour, 0, func(r *client.Run) { r.Location, r.Labels, r.Tickets = "/home/review", data, []string{"verdict"} }),
	}
	v := listOf(runs, noSteps, nil, narrowingOf(url.Values{"at": {"/work"}, "label": {"team=data"}}), at, "")
	if got := lineNames(v.Busy); len(got) != 1 || v.Busy.Lines[0].ID != "ci-client/1" || got[0] != "digest" || len(v.Ended.Lines) != 1 || len(v.Idle.Lines) != 0 {
		t.Errorf("the page holds %v and %v", got, lineNames(v.Ended))
	}
	if v.Every.Current || v.Every.Href != "/runs?label=team%3Ddata" {
		t.Errorf("every location is offered as %+v", v.Every)
	}
	v = listOf(runs, noSteps, nil, narrowingOf(url.Values{"label": {"team"}, "window": {"1h"}}), at, "")
	if got := strings.Join(lineNames(v.Busy), " "); got != "/home/review /workshop/x /work/digest" || len(v.Ended.Lines) != 1 {
		t.Errorf("narrowed to team in the last hour, the page holds %s and %v", got, lineNames(v.Ended))
	}
}

func Test_runs_keyed_runs_are_listed_by_key(t *testing.T) {
	runs := []*client.Run{
		run("lab-client/1", "thermostat", "statechart", "running", 2*time.Hour, 0, func(r *client.Run) { r.Location, r.Key, r.Timers = "/home/rooms/thermostat", "kitchen", 1 }),
		run("lab-client/2", "thermostat", "statechart", "running", time.Hour, 0, func(r *client.Run) {
			r.Location, r.Key, r.Timers, r.Title = "/home/rooms/thermostat", "hall", 1, "a thermostat"
		}),
		run("lab-client/3", "away", "flow", "running", time.Hour, 0, func(r *client.Run) { r.Location, r.Jobs = "/home/scenes/away", 1 }),
	}
	facts := map[string]fixed{"lab-client/1": {Tree: "/home", Lifetime: "keyed"}, "lab-client/2": {Tree: "/home", Lifetime: "keyed"}, "lab-client/3": {Tree: "/home", Lifetime: "transient"}}
	v := listOf(runs, noSteps, facts, narrowingOf(url.Values{"at": {"/home/rooms/thermostat"}}), at, "lab-client")
	if got := strings.Join(lineNames(v.Idle), " "); got != "a thermostat#hall thermostat#kitchen" || len(v.Busy.Lines) != 0 {
		t.Errorf("the keyed runs are listed as %s", got)
	}
	if h := v.Here; h == nil || h.Location != "/home/rooms/thermostat" || h.Lifetime != "keyed" || h.Tree != "/home" || !h.Send || h.Start || h.Cancel == nil || h.Cancel.String() != "corbie run cancel /home/rooms/thermostat" {
		t.Errorf("the location's head is %+v", v.Here)
	}
	v = listOf(runs, noSteps, facts, narrowingOf(url.Values{"at": {"/home"}}), at, "lab-client")
	if got := strings.Join(lineNames(v.Idle), " "); got != "a thermostat#hall rooms/thermostat#kitchen" || v.Here != nil || lineNames(v.Busy)[0] != "scenes/away" {
		t.Errorf("at /home the runs are named %s and %v, with the head %+v", got, lineNames(v.Busy), v.Here)
	}
	if h := listOf(runs, noSteps, facts, narrowingOf(url.Values{"at": {"/home/scenes/away"}}), at, "lab-client").Here; h == nil || h.Send || !h.Start {
		t.Errorf("a transient location's head is %+v", h)
	}
	if h := listOf(runs, noSteps, nil, narrowingOf(url.Values{"at": {"/home/rooms/thermostat"}}), at, "ci-client").Here; h == nil || h.Cancel != nil || !h.Send || h.Start {
		t.Errorf("another client's keyed location has the head %+v", h)
	}
}

func Test_runs_location_actions_run_their_commands(t *testing.T) {
	var calls []string
	command := func(_ context.Context, c page.Call) page.Answer {
		if found, _ := cli.Find(c.Words); found == nil || found.Name != strings.Join(c.Words, " ") {
			t.Errorf("%q is not one of Corbie's commands", strings.Join(c.Words, " "))
		}
		calls = append(calls, c.String())
		if c.Words[1] == "cancel" {
			return page.Answer{Out: `["lab-client/1  taken","lab-client/2  taken"]`}
		}
		if c.Words[1] == "submit" {
			return page.Answer{Out: `{"id":"lab-client/4"}`}
		}
		if strings.HasSuffix(c.Args[0], "#full") {
			return page.Answer{Status: 1, Out: `{"taken":false,"refused":"/home/rooms/thermostat is at its limit of 3 runs"}`}
		}
		return page.Answer{Out: `{"taken":true,"run":"lab-client/3"}`}
	}
	mine, err := identity.New("lab-client", identity.Client)
	if err != nil {
		t.Fatal(err)
	}
	list := &listed{from: client.New(nil, identity.Card{}, store.NewMemory().As(store.Party{Kind: store.Viewer}))}
	h := New(Config{List: list, Client: client.New(mine, identity.Card{}, nil), Command: command, Shell: page.Shell{}})
	post := func(act string, form url.Values) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/runs/at/"+act, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		h.ServeHTTP(w, r)
		return w
	}
	if w := post("cancel", url.Values{"at": {"/home/rooms/thermostat"}}); w.Code != 200 || w.Body.String() != "lab-client/1  taken; lab-client/2  taken" {
		t.Errorf("Cancel every run here answered %d %q", w.Code, w.Body.String())
	}
	if w := post("send", url.Values{"at": {"/home/rooms/thermostat"}, "key": {"hall"}, "event": {"set"}, "data": {` { "target" : 20 } `}}); w.Code != 200 || w.Body.String() != "taken by lab-client/3" {
		t.Errorf("Send answered %d %q", w.Code, w.Body.String())
	}
	if w := post("start", url.Values{"at": {"/home/scenes/away"}, "input": {`{"days": 2}`}}); w.Code != 200 || w.Body.String() != "started lab-client/4" {
		t.Errorf("Start answered %d %q", w.Code, w.Body.String())
	}
	// A send the controller refused fails the command, and the page gives
	// the reason in words.
	if w := post("send", url.Values{"at": {"/home/rooms/thermostat"}, "key": {"full"}, "event": {"set"}}); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "refused: /home/rooms/thermostat is at its limit of 3 runs") || strings.Contains(w.Body.String(), "{") {
		t.Errorf("a refused Send answered %d %q", w.Code, w.Body.String())
	}
	want := []string{
		"corbie run cancel /home/rooms/thermostat",
		`corbie run send /home/rooms/thermostat#hall set {"target":20}`,
		`corbie run submit /home/scenes/away --input={"days":2}`,
		"corbie run send /home/rooms/thermostat#full set",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("the actions ran\n%s", strings.Join(calls, "\n"))
	}
	calls = nil
	if w := post("cancel", url.Values{"at": {"home"}}); w.Code != http.StatusBadRequest || len(calls) != 0 {
		t.Errorf("a cancel of no location answered %d and ran %v", w.Code, calls)
	}
	if w := post("start", url.Values{"at": {"/home/x"}, "input": {"not json"}}); w.Code != http.StatusBadRequest || len(calls) != 0 {
		t.Errorf("an input that is not JSON answered %d and ran %v", w.Code, calls)
	}
	h = New(Config{List: list, Command: command, Shell: page.Shell{}})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/runs/at/cancel", strings.NewReader("at=%2Fhome"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if h.ServeHTTP(w, r); w.Code != http.StatusForbidden || len(calls) != 0 {
		t.Errorf("with no client, a cancel answered %d and ran %v", w.Code, calls)
	}
}

// listed is a list that holds the runs of a store at the moment it is
// asked.
type listed struct {
	from    *client.Client
	changed chan struct{}
}

func (f *listed) Runs() ([]*client.Run, error) {
	return f.from.List(context.Background(), client.Filter{Finished: true})
}
func (f *listed) Changed() <-chan struct{} { return f.changed }

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
	id, err := l.Run(ctx, draft, client.Start{})
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
	// Each ask a step starts is named by where it is written, in the order
	// the step held the first job of each, and a job's failure that ended
	// the run is noted.
	tell := &machine.Job{Name: "tell", AskAt: "open.messages.ask"}
	entries := []client.Entry{{ID: "a-client/1", Step: 1,
		Events: []instance.Arrived{{Event: machine.Event{Name: "failed", From: "turn#1"}}},
		Asks: []instance.Ask{
			{Kind: instance.AskHold, Name: "tell#1", Job: tell},
			{Kind: instance.AskHold, Name: "look#1", Job: &machine.Job{Name: "look"}},
			{Kind: instance.AskChild, Name: "ask#1", Child: "a-client/2"},
			{Kind: instance.AskChild, Name: "ask#2", Child: "a-client/3"},
		}}}
	byID := map[string]*client.Run{
		"a-client/2": {ID: "a-client/2", Machine: "approval", Type: "flow"},
		"a-client/3": {ID: "a-client/3", Machine: "own", Type: "flow"},
	}
	st := &instance.State{Jobs: map[string]*instance.Going{
		"tell":  {Job: *tell, Held: true},
		"ask#1": {Job: machine.Job{Name: "ask"}, ForAsk: []string{"tell"}},
	}}
	m := marksOf("a-client/1", entries, "failed", st, byID)
	if m.Children["open.messages.ask"].ID != "a-client/2" || m.Children["ask"].ID != "a-client/3" {
		t.Errorf("the asks' runs are known as %+v", m.Children)
	}
	if strings.Join(m.Going, " ") != "open.messages.ask tell" || strings.Join(m.Steps[0].Failed, " ") != "turn" || !m.Failed {
		t.Errorf("the run is marked as %+v", m)
	}

	// A child's rows are under the row that started it, and an ended child
	// is folded.
	now := at
	tree := []client.Entry{
		{ID: "a-client/1", Step: 1, Time: now, Shown: map[string]any{"states": []string{"answering"}},
			Events: []instance.Arrived{{Event: machine.Event{Name: "start"}}},
			Asks:   []instance.Ask{{Kind: instance.AskChild, Name: "answering", Child: "a-client/2"}}},
		{ID: "a-client/2", Step: 1, Time: now, Shown: map[string]any{"stages": map[string]any{"who": "running"}}},
		{ID: "a-client/2", Step: 2, Time: now, Status: "done", Shown: map[string]any{"stages": map[string]any{"who": "done"}},
			Events: []instance.Arrived{{Event: machine.Event{Name: "done", From: "who"}}}},
	}
	byID = map[string]*client.Run{
		"a-client/1": {ID: "a-client/1", Machine: "chat", Type: "statechart", Status: "running"},
		"a-client/2": {ID: "a-client/2", Machine: "message", Type: "flow", Status: "done", Parent: "a-client/1", Started: now, Ended: now.Add(time.Second)},
	}
	rows := rowsOf("a-client/1", tree, byID, nil, now, now)
	var said []string
	for _, r := range rows {
		said = append(said, r.Said())
	}
	if strings.Join(said, "; ") != "started; entered answering; state answering started message" {
		t.Fatalf("the run's rows are %q", said)
	}
	g := rows[2].Group
	if g == nil || !g.Ended || g.Steps != 2 || len(g.Rows) != 2 || g.Rows[0].Said() != "stage who done" || g.Rows[0].Depth != 1 || g.Rows[1].Said() != "done" {
		t.Errorf("the child's rows are not under the row that started it: %+v", g)
	}

	l, viewer, id := local(t)
	h := New(Config{List: &listed{from: viewer}, Lister: viewer, Client: l.Client, Command: nil, Interval: time.Second, Shell: page.Shell{}})
	body := get(t, h, "/run/"+id)
	tickets, err := l.Tickets(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{tickets["verdict"], "<span>timer too_long due ", "entered <b>drafting</b>", "left <b>drafting</b>, entered <b>waiting</b>", "<b>&lt;-</b> done from drafting", `<span class="i" title="review">review</span>`, `class="state now"`, `class="edge taken"`, `action="/run/` + id + `/send"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the run's own page does not show %s", want)
		}
	}
	if strings.Contains(body, "events at") {
		t.Errorf("the run's page lists events at a key as something it waits on")
	}
}

func Test_runs_a_step_shows_its_run_as_it_was(t *testing.T) {
	// The drawing of the first step has the run in drafting, and no
	// transition taken.
	l, viewer, id := local(t)
	h := New(Config{List: &listed{from: viewer}, Lister: viewer, Client: l.Client, Interval: time.Second, Shell: page.Shell{}})
	var parts []map[string]string
	if err := json.Unmarshal([]byte(get(t, h, "/run/"+id+"/at?row="+url.QueryEscape(id+"/1/0"))), &parts); err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 || parts[0]["id"] != "view" || parts[1]["id"] != "details" {
		t.Fatalf("the step is answered with %v", parts)
	}
	view, details := parts[0]["html"], parts[1]["html"]
	now := regexp.MustCompile(`<g class="state now">.*?<text class="title"[^>]*>([^<]*)</text>`).FindStringSubmatch(view)
	if !strings.Contains(view, "as of step 1") || now == nil || now[1] != "drafting" || strings.Contains(view, "edge taken") {
		t.Errorf("the run is not drawn as of its first step: %v\n%s", now, view)
	}
	if !strings.Contains(details, "entered drafting") || !strings.Contains(details, "<b>&lt;-</b> start") || strings.Contains(details, "done from drafting") {
		t.Errorf("the details are not those of the first step:\n%s", details)
	}

	// A send names its address and links to the run that took it or that
	// it started, and its record says what `corbie run show --record` says;
	// one that went nowhere says why. An event a run sent links to that
	// run, a call links to the run it started, and the path begins with the
	// run whose send started the root.
	sent := at.Add(-time.Hour)
	key := "slack:C1"
	byID := map[string]*client.Run{
		"a-client/1": {ID: "a-client/1", Machine: "listen", Type: "statechart", Status: "running", Location: "/rook/bridges/slack"},
		"a-client/2": {ID: "a-client/2", Machine: "question", Type: "statechart", Status: "done", Location: "/rook/question"},
		"a-client/3": {ID: "a-client/3", Machine: "boiler", Type: "statechart", Status: "running", Location: "/home/devices/boiler"},
		"a-client/4": {ID: "a-client/4", Machine: "chat", Type: "statechart", Status: "running", StartedBy: "a-client/1", Location: "/rook/chat", Key: key},
		"a-client/5": {ID: "a-client/5", Machine: "message", Type: "flow", Status: "running", Parent: "a-client/4", Location: "/rook/chat"},
		"a-client/6": {ID: "a-client/6", Machine: "weather", Type: "flow", Status: "done", Location: "/home/weather"},
	}
	sends := client.Entry{ID: "a-client/1", Step: 3, Time: sent, Status: "running",
		Events: []instance.Arrived{
			{Event: machine.Event{Name: "answer", Sender: &machine.Address{At: "/rook/chat", Key: &key, Run: "a-client/4"}}},
			{Event: machine.Event{Name: "poke", Sender: &machine.Address{Client: "panel"}}},
		},
		Asks: []instance.Ask{
			{Kind: instance.AskEvent, Location: "/rook/chat", Key: key, Run: "a-client/4", To: "a-client/4", Event: &machine.Event{Name: "message"}},
			{Kind: instance.AskEvent, Location: "/home/devices/boiler", To: "a-client/3", Event: &machine.Event{Name: "set"}},
			{Kind: instance.AskEvent, Location: "/rook/chat", Key: "slack:C2", Undelivered: true, Text: "the limit of 2 runs at /rook/chat is reached", Event: &machine.Event{Name: "message"}},
			{Kind: instance.AskCall, Name: "weather", Location: "/home/weather", Run: "a-client/6", Job: &machine.Job{Input: map[string]any{"days": 2}}},
		}}
	rows := rowsOf("a-client/1", []client.Entry{sends}, byID, nil, at, sent)
	var said []string
	for _, r := range rows {
		said = append(said, r.Said())
	}
	if strings.Join(said, "\n") != strings.Join([]string{
		"⇠ answer from /rook/chat#slack:C1",
		"⇠ poke from client:panel",
		"⇢ sent message to /rook/chat#slack:C1 · started chat",
		"⇢ sent set to /home/devices/boiler · boiler",
		"⇢ sent message to /rook/chat#slack:C2 · undelivered",
		"⇢ called /home/weather · weather",
	}, "\n") {
		t.Errorf("the sends are\n%s", strings.Join(said, "\n"))
	}
	linkIn := func(r stepRow) string {
		for _, w := range r.What {
			if w.Run != nil {
				return w.Run.ID
			}
		}
		return ""
	}
	if linkIn(rows[0]) != "a-client/4" || linkIn(rows[1]) != "" || linkIn(rows[2]) != "a-client/4" || linkIn(rows[3]) != "a-client/3" || linkIn(rows[5]) != "a-client/6" {
		t.Errorf("the sends link to %q %q %q %q %q", linkIn(rows[0]), linkIn(rows[1]), linkIn(rows[2]), linkIn(rows[3]), linkIn(rows[5]))
	}
	if rows[4].Facts[len(rows[4].Facts)-1].Text != "the limit of 2 runs at /rook/chat is reached" || rows[5].In == nil || rows[2].Kind != "send" || rows[4].Kind != "send undelivered" || rows[0].Kind != "got" {
		t.Errorf("an undelivered send says %+v as %q, and a call was given %v", rows[4].Facts, rows[4].Kind, rows[5].In)
	}
	said = nil
	for _, l := range lines(sends, "listen", sent) {
		said = append(said, l.Text)
	}
	if strings.Join(said, "\n") != strings.Join([]string{
		"answer from /rook/chat#slack:C1 a-client/4",
		"poke from client:panel",
		"send message to /rook/chat#slack:C1 · started a-client/4",
		"send set to /home/devices/boiler · a-client/3",
		"send message to /rook/chat#slack:C2 · undelivered: the limit of 2 runs at /rook/chat is reached",
	}, "\n") {
		t.Errorf("the record of the sends says\n%s", strings.Join(said, "\n"))
	}
	if rows[0].Time != sent.Local().Format("15:04:05") {
		t.Errorf("a step's time is %q", rows[0].Time)
	}
	started := rowsOf("a-client/4", []client.Entry{{ID: "a-client/4", Step: 1, Time: sent, Status: "running"}}, byID, nil, at, sent)
	if len(started) != 1 || started[0].Said() != "started by /rook/bridges/slack" || linkIn(started[0]) != "a-client/1" {
		t.Errorf("a run a send started begins with %+v", started)
	}

	// The controller notes an event that was not delivered, or that went to
	// another run, in a message named for the send: the send's row says so,
	// and the note has no row of its own. A send to a keyed run by its ID
	// gives the run's key.
	notes := []client.Entry{{ID: "a-client/1", Step: 4, Time: sent, Status: "running", Asks: []instance.Ask{
		{Kind: instance.AskEvent, Location: "/home/devices/boiler", To: "a-client/3", Event: &machine.Event{Name: "off"}, Message: "a-client.1.00000004.0001.send"},
		{Kind: instance.AskEvent, Location: "/rook/chat", Key: key, To: "a-client/7", Event: &machine.Event{Name: "message"}, Message: "a-client.1.00000004.0002.send"},
		{Kind: instance.AskEvent, Location: "/rook/chat", To: "a-client/4", Direct: true, Event: &machine.Event{Name: "pong"}, Message: "a-client.1.00000004.0003.send"},
	}}}
	for i, n := range []struct{ message, text string }{
		{"a-client.1.00000004.0001.send.note", "the event off sent to /home/devices/boiler was not delivered: /home/devices/boiler does not receive off from the run a-client/1 at /rook/bridges/slack"},
		{"a-client.1.00000004.0002.send.moved.4", "the event message sent to /rook/chat#slack:C1 went to the run a-client/4, not a-client/7"},
	} {
		notes = append(notes, client.Entry{ID: "a-client/1", Step: 5 + i, Time: sent, Status: "running",
			Events: []instance.Arrived{{Event: machine.Event{Name: "note", Data: n.text}, Source: "ctl/inbox/ctl/" + n.message, Own: true}},
			Asks:   []instance.Ask{{Kind: instance.AskNote, Text: n.text}}})
	}
	rows = rowsOf("a-client/1", notes, byID, nil, at, sent)
	said = nil
	for _, r := range rows {
		said = append(said, r.Said())
	}
	if strings.Join(said, "\n") != strings.Join([]string{
		"⇢ sent off to /home/devices/boiler · undelivered",
		"⇢ sent message to /rook/chat#slack:C1 · chat",
		"⇢ sent pong to /rook/chat#slack:C1 · chat",
	}, "\n") {
		t.Errorf("the noted sends are\n%s", strings.Join(said, "\n"))
	}
	if len(rows) == 3 && (rows[0].Facts[len(rows[0].Facts)-1].Text != "/home/devices/boiler does not receive off from the run a-client/1 at /rook/bridges/slack" || linkIn(rows[1]) != "a-client/4" || linkIn(rows[0]) != "") {
		t.Errorf("an undelivered send says %+v, and a moved one links to %q", rows[0].Facts, linkIn(rows[1]))
	}

	// A job asked again has its own input and output: Corbie writes the
	// ask as answering#2, and the end as from answering.
	again := func(typ string, end string) []stepRow {
		job := func(name, child, input string) instance.Ask {
			if typ == "agent" {
				return instance.Ask{Kind: instance.AskScript, Name: name, Job: &machine.Job{Script: "work.sh", Input: input}}
			}
			return instance.Ask{Kind: instance.AskChild, Name: name, Child: child, Job: &machine.Job{Machine: "work", Input: input}}
		}
		ended := func(data string) []instance.Arrived {
			return []instance.Arrived{{Event: machine.Event{Name: end, From: "answering", Data: data}}}
		}
		runs := map[string]*client.Run{"a-client/1": {ID: "a-client/1", Machine: "chat", Type: typ, Status: "running"}}
		return rowsOf("a-client/1", []client.Entry{
			{ID: "a-client/1", Step: 1, Time: sent, Status: "running", Asks: []instance.Ask{job("answering", "a-client/2", "first")}},
			{ID: "a-client/1", Step: 2, Time: sent, Status: "running", Events: ended("one"), Asks: []instance.Ask{job("answering#2", "a-client/3", "second")}},
			{ID: "a-client/1", Step: 3, Time: sent, Status: "running", Events: ended("two")},
		}, runs, nil, at, sent)
	}
	if rows := again("statechart", "done"); len(rows) != 3 || rows[1].In != "first" || rows[1].Out != "one" || rows[2].In != "second" || rows[2].Out != "two" {
		t.Errorf("a job asked again is shown as %+v", rows)
	}
	if rows := again("agent", "failed"); len(rows) != 3 || rows[1].In != "first" || rows[2].In != "second" || rows[2].Facts[0].Text != "answering#2" {
		t.Errorf("an agent's job asked again is shown as %+v", rows)
	}

	// A step's details hold the values its run's jobs emitted since the
	// step before.
	record := []client.Entry{
		{ID: "a-client/1", Step: 1, Time: sent, Status: "running"},
		{ID: "a-client/1", Time: sent, Job: "hearing", Name: "early", Data: "no"},
		{ID: "a-client/1", Step: 2, Time: sent, Status: "running"},
		{ID: "a-client/1", Time: sent, Job: "hearing#2", Name: "answer", Data: "yes"},
		{ID: "a-client/2", Step: 1, Time: sent, Status: "running"},
		{ID: "a-client/1", Step: 3, Time: sent, Status: "running"},
		{ID: "a-client/1", Time: sent, Job: "hearing#2", Name: "late", Data: "no"},
	}
	cut, own := cutAt(record, &stepRow{Run: link{ID: "a-client/1"}, Step: 3})
	if len(cut) != 3 || len(own) != 2 || lines(own[0], "listen", sent)[0].Text != "hearing#2 answer: yes" || own[1].Step != 3 {
		t.Errorf("step 3 is cut as %+v, and its details hold %+v", cut, own)
	}

	under := map[string][]*client.Run{"a-client/4": {byID["a-client/5"]}}
	var path []string
	for _, l := range pathOf(byID["a-client/5"], byID, under, map[string]fixed{"a-client/4": {}}) {
		path = append(path, fmt.Sprintf("%s %q", l.Machine, l.Sent))
	}
	if strings.Join(path, ", ") != `listen "", chat "a send", message ""` {
		t.Errorf("the path is %v", path)
	}
	if got := pathOf(byID["a-client/4"], byID, under, nil)[1].Sent; got != "a send or a call" {
		t.Errorf("a root another client's tree holds was started by %q", got)
	}
}

func Test_runs_other_clients_run_shows_no_record(t *testing.T) {
	_, viewer, id := local(t)
	other, err := identity.New("other-client", identity.Client)
	if err != nil {
		t.Fatal(err)
	}
	someone := client.New(other, viewer.Controller, viewer.Store)
	h := New(Config{List: &listed{from: viewer}, Lister: viewer, Client: someone, Interval: time.Second, Shell: page.Shell{}})
	body := get(t, h, "/run/"+id)
	if !strings.Contains(body, "running") || !strings.Contains(body, "a run in another client's tree: only its status can be read") || strings.Contains(body, `id="steps"`) {
		t.Errorf("another client's run does not show its status alone:\n%s", body)
	}
	for _, never := range []string{"<b>&lt;-</b> start", "ticket verdict </span><code", `class="state now"`, "/cancel"} {
		if strings.Contains(body, never) {
			t.Errorf("another client's run shows %s", never)
		}
	}
	if !strings.Contains(body, `action="/run/`+id+`/send"`) {
		t.Errorf("another client's running statechart is not offered Send, whose receipt says whether it is taken")
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
	list := &listed{from: client.New(nil, identity.Card{}, store.NewMemory().As(store.Party{Kind: store.Viewer}))}
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
	post("/run/"+other+"/send", url.Values{"event": {"poke"}})
	want := []string{
		"corbie run reply " + own + ` e346df80 {"ok":true}`,
		"corbie run send " + own + " poke [1,2]",
		"corbie run cancel " + own,
		"corbie run reply " + other + " 77aa 3",
		"corbie run send " + other + " poke",
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

// counting is a store that counts its reads.
type counting struct {
	store.Store
	reads atomic.Int64
}

func (c *counting) Read(ctx context.Context, name string) ([]byte, store.Mark, error) {
	c.reads.Add(1)
	return c.Store.Read(ctx, name)
}

func Test_runs_a_run_is_named_by_its_title(t *testing.T) {
	l, viewer, _ := local(t)
	ctx := context.Background()
	submit(t, l, "/house", map[string]string{"door.yaml": "statechart: door\nlifetime: keyed\ntitle: \"door for ${input.who}\"\ninitial: shut\nstates:\n  shut:\n    on: { open: opened }\n  opened: { end: done }\n"})
	key := "door:7"
	if _, err := l.Send(ctx, machine.Address{At: "/house/door", Key: &key}, "knock", map[string]any{"who": "zummed"}, 0); err != nil {
		t.Fatal(err)
	}
	id := waitFor(t, viewer, func(r *client.Run) bool { return r.Location == "/house/door" && r.Step > 0 })

	// The client that started the run lists it by its title followed by
	// its key, and heads its page by its title, with the key in the tooltip
	// and beside the title. A list drawn again reads nothing more.
	counted := &counting{Store: l.Client.Store}
	c := client.New(l.Client.ID, l.Client.Controller, counted)
	h := New(Config{List: &listed{from: viewer}, Lister: viewer, Client: c, Interval: time.Second, Shell: page.Shell{}})
	list := get(t, h, "/runs")
	if once := counted.reads.Load(); get(t, h, "/runs") != list || counted.reads.Load() != once {
		t.Errorf("the list drawn again read %d more times", counted.reads.Load()-once)
	}
	if want := "<span class=\"title named\" title=\"door for zummed\n/house/door#door:7\n" + id + "\" style=\"--key: 7ch\"><span>door for zummed</span><span class=\"key\">#door:7</span></span>"; !strings.Contains(list, want) {
		t.Errorf("the run's line is not named by its title, %s:\n%s", want, list)
	}
	body := get(t, h, "/run/"+id)
	for _, want := range []string{`<span class="title" title="door for zummed">door for zummed</span>`, `<a class="key" href="/runs?at=%2Fhouse%2Fdoor" title="the runs at /house/door">/house/door#door:7</a>`, "<title>door for zummed · Corbie</title>", "<span class=\"who\" title=\"door for zummed\n/house/door#door:7\n" + id + "\">"} {
		if !strings.Contains(body, want) {
			t.Errorf("the run's page does not show %s", want)
		}
	}

	// A UI that acts as no client reads no title, and names the run by
	// its key.
	h = New(Config{List: &listed{from: viewer}, Lister: viewer, Interval: time.Second, Shell: page.Shell{}})
	list, body = get(t, h, "/runs"), get(t, h, "/run/"+id)
	if !strings.Contains(list, "title=\"/house/door#door:7\n"+id+"\"><span>/house/door#door:7</span></span>") || strings.Contains(list+body, "door for") {
		t.Errorf("with no client the run is not named by its address:\n%s", list)
	}

	// Nor does a UI acting as another client, on the same store.
	other, err := identity.New("other-client", identity.Client)
	if err != nil {
		t.Fatal(err)
	}
	h = New(Config{List: &listed{from: viewer}, Lister: viewer, Client: client.New(other, l.Client.Controller, l.Client.Store), Interval: time.Second, Shell: page.Shell{}})
	list, body = get(t, h, "/runs"), get(t, h, "/run/"+id)
	if !strings.Contains(list, "title=\"/house/door#door:7\n"+id+"\"><span>/house/door#door:7</span></span>") || strings.Contains(list+body, "door for") {
		t.Errorf("another client's run is not named by its address:\n%s", list)
	}

	// Each title is read once while the run is listed, though its head
	// moves on, and forgotten once it is not listed.
	runs, err := (&listed{from: viewer}).Runs()
	if err != nil {
		t.Fatal(err)
	}
	kept, before := &titles{}, counted.reads.Load()
	if _, _, err := kept.of(ctx, c, runs); err != nil {
		t.Fatal(err)
	}
	once := counted.reads.Load()
	got, facts, err := kept.of(ctx, c, runs)
	if err != nil || counted.reads.Load() != once || once == before {
		t.Errorf("the titles were read %d times, then %d more: %v", once-before, counted.reads.Load()-once, err)
	}
	if i := slices.IndexFunc(got, func(r *client.Run) bool { return r.ID == id }); i < 0 || got[i].Title != "door for zummed" || runs[i].Title != "" {
		t.Errorf("the listed runs are titled %+v", got)
	}
	if f := facts[id]; f.Tree != "/house" || f.Lifetime != "keyed" || f.Called {
		t.Errorf("what is fixed of the door is %+v", f)
	}
	for _, r := range runs {
		r.Step++
	}
	if _, _, err := kept.of(ctx, c, runs); err != nil || counted.reads.Load() != once {
		t.Errorf("a head that moved on had its title read %d more times: %v", counted.reads.Load()-once, err)
	}
	if kept.of(ctx, c, nil); len(kept.read) != 0 {
		t.Errorf("the titles of runs no longer listed are kept: %v", kept.read)
	}
}

func Test_runs_a_child_is_named_by_its_title(t *testing.T) {
	// A child with a title is named by it on its row and in Children, and
	// one with none by its machine, though it is at its parent's location.
	chat := run("c-client/1", "chat", "statechart", "running", time.Hour, 0, func(r *client.Run) { r.Location, r.Key = "/rook/chat", "slack:D1" })
	titled := run("c-client/2", "message", "flow", "running", time.Minute, 0, func(r *client.Run) { r.Parent, r.Location, r.Title = chat.ID, "/rook/chat", "Zummed: hello" })
	bare := run("c-client/4", "say", "flow", "running", 3*time.Minute, 0, func(r *client.Run) { r.Parent, r.Location = chat.ID, "/rook/chat" })
	v := listOf([]*client.Run{chat, titled, bare}, noSteps, nil, narrowing{}, at, "")
	var names []string
	for _, r := range v.Busy.Lines[0].Rows[1:] {
		names = append(names, r.Name)
	}
	if got := strings.Join(names, ", "); got != "say, Zummed: hello" || v.Busy.Lines[0].Name != "/rook/chat#slack:D1" {
		t.Errorf("the line %s unfolds to rows named %s", v.Busy.Lines[0].Name, got)
	}
	// A keyed run with a title is named by it, followed by its key.
	dm := run("c-client/5", "chat", "statechart", "running", time.Hour, 0, func(r *client.Run) { r.Location, r.Key, r.Title = "/rook/chat", "slack:D2", "Zummed in DM" })
	for _, c := range []struct {
		r              *client.Run
		name, key, tip string
	}{{titled, "Zummed: hello", "", "Zummed: hello\n/rook/chat\nc-client/2"}, {bare, "say", "", "/rook/chat\nc-client/4"}, {chat, "/rook/chat#slack:D1", "", "/rook/chat#slack:D1\nc-client/1"}, {dm, "Zummed in DM", "#slack:D2", "Zummed in DM\n/rook/chat#slack:D2\nc-client/5"}} {
		if got := childOf(c.r, at); got.Name != c.name || got.Key != c.key || got.Tip != c.tip || got.Machine != c.r.Machine {
			t.Errorf("Children lists %s as %+v", c.r.ID, got)
		}
	}
}

// submit records a directory of files as a tree at prefix, and waits until
// the controller has taken it.
func submit(t *testing.T, l *client.Local, prefix string, files map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tree, err := client.ReadTree(dir, prefix)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range tree.Machines {
		if m.Problem != nil {
			t.Fatalf("%s: %v", m.Path, m.Problem)
		}
	}
	ctx := context.Background()
	message, err := l.Submit(ctx, tree, 0)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := l.WaitReceipt(ctx, message, 0, client.LocalInterval)
	if err != nil || !receipt.Taken {
		t.Fatalf("the submit was not taken: %+v %v", receipt, err)
	}
}

// waitFor waits until the store holds a run that matches, and gives its ID.
func waitFor(t *testing.T, c *client.Client, match func(*client.Run) bool) string {
	t.Helper()
	for deadline := time.Now().Add(20 * time.Second); ; {
		runs, err := c.List(context.Background(), client.Filter{Finished: true})
		for _, r := range runs {
			if match(r) {
				return r.ID
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no run came to match: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// finished waits until the run with the ID has ended.
func finished(t *testing.T, c *client.Client, id string) *client.Run {
	t.Helper()
	r, err := c.Wait(context.Background(), id, client.LocalInterval)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// groupOf gives the address from which a run's page reads the rows of the
// group of the row that says text.
func groupOf(t *testing.T, body, text string) string {
	t.Helper()
	for _, m := range regexp.MustCompile(`(?s)<li class="group trace"[^>]* data-load="([^"]+)"[^>]*>(.*?)</div>`).FindAllStringSubmatch(body, -1) {
		if strings.Contains(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(m[2], ""), text) {
			return strings.ReplaceAll(m[1], "&amp;", "&")
		}
	}
	t.Fatalf("no group says %s:\n%s", text, body)
	return ""
}

func Test_runs_a_send_shows_the_rows_it_caused(t *testing.T) {
	l, viewer, _ := local(t)
	ctx := context.Background()
	submit(t, l, "/house", map[string]string{
		"room.yaml": "statechart: room\nlifetime: keyed\ninitial: open\nstates:\n  open:\n    on:\n      knock: open\n      leave: closed\n  closed: { end: done }\n",
		"desk.yaml": `statechart: desk
allow: [send:room]
initial: first
states:
  first:
    entry:
      send:
        - { to: { at: room, key: "1" }, event: knock, data: { by: ana } }
    timers: { pause: 1s }
    on: { pause: second }
  second:
    entry:
      send:
        - { to: { at: room, key: "1" }, event: leave }
    timers: { pause: 1s }
    on: { pause: last }
  last: { end: done }
`})
	desk, err := l.Start(ctx, client.Start{At: "/house/desk"})
	if err != nil {
		t.Fatal(err)
	}
	finished(t, l.Client, desk)
	room := waitFor(t, viewer, func(r *client.Run) bool { return r.Location == "/house/room" && r.HasEnded() })

	h := New(Config{List: &listed{from: viewer}, Lister: viewer, Client: l.Client, Interval: time.Second, Shell: page.Shell{}})
	body := get(t, h, "/run/"+desk)
	if !strings.Contains(body, `data-run="`+room+`"`) || strings.Contains(body, "entered <b>closed</b>") {
		t.Errorf("the desk's page does not fold the room's rows into its sends, read later:\n%s", body)
	}
	started := get(t, h, groupOf(t, body, "knock to /house/room#1 · started room"))
	for _, want := range []string{"started by", "knock</b> from", "--d: 1"} {
		if !strings.Contains(started, want) {
			t.Errorf("the group of the send that started the room does not show %s:\n%s", want, started)
		}
	}
	if strings.Contains(started, "leave</b> from") {
		t.Errorf("the group of the first send holds the second:\n%s", started)
	}
	if text := regexp.MustCompile(`<[^>]+>`).ReplaceAllString(started, ""); !strings.Contains(text, "⇠ knock from /house/desk") {
		t.Errorf("the room's row of the knock does not say who sent it:\n%s", text)
	}
	left := get(t, h, groupOf(t, body, "leave to /house/room#1 · room"))
	if !strings.Contains(left, "leave</b> from") || !strings.Contains(left, "entered <b>closed</b>") || strings.Contains(left, "started by") {
		t.Errorf("the group of the second send is not the room from the step that took it:\n%s", left)
	}

	// Another client reads neither run, and its page offers no group.
	other, err := identity.New("other-client", identity.Client)
	if err != nil {
		t.Fatal(err)
	}
	h = New(Config{List: &listed{from: viewer}, Lister: viewer, Client: client.New(other, viewer.Controller, viewer.Store), Interval: time.Second, Shell: page.Shell{}})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", groupOf(t, body, "leave to"), nil))
	if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "closed") {
		t.Errorf("another client read the group, %d:\n%s", w.Code, w.Body.String())
	}

	// A send's group ends at the next step that took another sent event.
	key := "1"
	from := &machine.Address{At: "/house/desk", Run: desk}
	entries := []client.Entry{
		{ID: room, Step: 1, Time: at, Events: []instance.Arrived{{Event: machine.Event{Name: "start"}}}},
		{ID: room, Step: 2, Time: at, Events: []instance.Arrived{{Event: machine.Event{Name: "knock", Sender: from}, Source: "x/y.knock.send"}}},
		{ID: room, Step: 3, Time: at, Events: []instance.Arrived{{Event: machine.Event{Name: "timer", From: "pause"}}}},
		{ID: room, Step: 4, Time: at, Events: []instance.Arrived{{Event: machine.Event{Name: "knock", Sender: &machine.Address{At: "/house/hall", Key: &key, Run: "lab-client/9"}}}}},
	}
	byID := map[string]*client.Run{room: {ID: room, Machine: "room", Type: "statechart"}}
	var steps []int
	for _, r := range traced(&trace{Run: link{ID: room}, Message: "y.knock.send"}, entries, byID, nil, at, at, 1) {
		steps = append(steps, r.Step)
	}
	if fmt.Sprint(steps) != "[2 3]" {
		t.Errorf("the send's group holds the rows of steps %v", steps)
	}
}

func Test_runs_a_call_shows_the_run_it_called(t *testing.T) {
	l, viewer, _ := local(t)
	ctx := context.Background()
	submit(t, l, "/lab", map[string]string{
		"trip.yaml":    "flow: trip\nallow: [send:weather]\nstages:\n  forecast:\n    job: { call: { at: weather }, input: { days: 2 } }\noutput: ${stages.forecast.output}\n",
		"weather.yaml": "flow: weather\ntitle: weather for ${input.days} days\nstages:\n  look:\n    job: { script: ./look.js, input: \"${input}\" }\noutput: ${stages.look.output}\n",
		"look.js":      "return { temp: 12, days: input.days }",
	})
	trip, err := l.Start(ctx, client.Start{At: "/lab/trip"})
	if err != nil {
		t.Fatal(err)
	}
	if r := finished(t, l.Client, trip); r.Status != "done" {
		t.Fatalf("the trip ended %s: %s", r.Status, r.Error)
	}
	weather := waitFor(t, viewer, func(r *client.Run) bool { return r.Location == "/lab/weather" && r.HasEnded() })

	h := New(Config{List: &listed{from: viewer}, Lister: viewer, Client: l.Client, Interval: time.Second, Shell: page.Shell{}})
	body := get(t, h, "/run/"+trip)
	group := get(t, h, groupOf(t, body, "called /lab/weather · weather"))
	for _, want := range []string{"started by", "stage <b>look</b> done", `data-run="` + weather + `"`, "--d: 1"} {
		if !strings.Contains(group, want) {
			t.Errorf("the call's group does not show %s:\n%s", want, group)
		}
	}
	if text := regexp.MustCompile(`<[^>]+>`).ReplaceAllString(group, ""); !strings.Contains(text, "stage look done") {
		t.Errorf("the call's group does not read stage look done:\n%s", text)
	}
	if !strings.Contains(body, `<span class="name">weather for 2 days</span> · <span class="st done">done</span>`) {
		t.Errorf("the call's row does not end with the called run's title and status:\n%s", body)
	}
	// A group opened inside another is one level below the row that opened
	// it, however deep that row is.
	if deep := get(t, h, strings.Replace(groupOf(t, body, "called /lab/weather · weather"), "depth=0", "depth=3", 1)); !strings.Contains(deep, "--d: 4") || strings.Contains(deep, "--d: 1") {
		t.Errorf("a group opened at depth 3 is not at depth 4:\n%s", deep)
	}

	// The weather's page begins with the trip, which called it.
	if page := get(t, h, "/run/"+weather); !strings.Contains(page, `<span class="sep send" title="a call of the run before started it">⇢</span>`) {
		t.Errorf("the called run's path does not begin with the call:\n%s", page)
	}
}
