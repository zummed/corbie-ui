package drawing

import (
	"html/template"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zummed/corbie/machine"

	_ "github.com/zummed/corbie/agent"
	_ "github.com/zummed/corbie/flow"
	_ "github.com/zummed/corbie/statechart"
)

func definition(t *testing.T, src string, files map[string]string) *machine.Definition {
	t.Helper()
	draft, err := machine.Parse([]byte(src), machine.Given(files))
	if err != nil {
		t.Fatalf("the definition does not check: %v", err)
	}
	return draft.Definition
}

func draw(t *testing.T, def *machine.Definition, run *Run) string {
	t.Helper()
	svg, err := Draw(def, run)
	if err != nil {
		t.Fatal(err)
	}
	return string(svg)
}

// centre gives where a name is written on a drawing.
func centre(t *testing.T, svg template.HTML, name string) (float64, float64) {
	t.Helper()
	m := regexp.MustCompile(`<text class="title" x="([0-9.]+)" y="([0-9.]+)" text-anchor="[a-z]+">` + regexp.QuoteMeta(name) + `</text>`).FindStringSubmatch(string(svg))
	if m == nil {
		t.Fatalf("%q is not drawn", name)
	}
	x, _ := strconv.ParseFloat(m[1], 64)
	y, _ := strconv.ParseFloat(m[2], 64)
	return x, y
}

// states gives what a statechart shows at a step: the states it is in.
func states(names ...string) map[string]any {
	var list []any
	for _, n := range names {
		list = append(list, n)
	}
	return map[string]any{"states": list}
}

// group gives the class of the shape that holds a name.
func group(t *testing.T, svg string, name string) string {
	t.Helper()
	m := regexp.MustCompile(`<g class="([^"]*)">(?:<rect[^>]*/>)+<text[^>]*>` + regexp.QuoteMeta(name) + `</text>`).FindStringSubmatch(svg)
	if m == nil {
		t.Fatalf("%q is not drawn as a shape", name)
	}
	return m[1]
}

const script = "return 1"

const review = `statechart: review
input: { document: string }
initial: drafting
states:
  drafting:
    job: { script: ./write.js, input: { document: "${input.document}" } }
    on:
      done: waiting
      failed: gave_up
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

func Test_drawing_flow_stage_follows_what_it_waits_for(t *testing.T) {
	def := definition(t, `flow: fan
stages:
  fetch:
    job: ./write.js
  sum:
    each: ${stages.fetch.output}
    job: ./write.js
  translate:
    job: { script: ./write.js, input: "${stages.fetch.output}" }
  send:
    follows: [sum]
    when: ${!stages.translate.failed}
    job: ./write.js
`, map[string]string{"write.js": script})
	svg, _ := Draw(def, nil)
	_, fetch := centre(t, svg, "fetch")
	_, sum := centre(t, svg, "sum")
	_, translate := centre(t, svg, "translate")
	_, send := centre(t, svg, "send")
	if !(fetch < sum && fetch < translate) {
		t.Errorf("sum at %v and translate at %v are not drawn after fetch at %v", sum, translate, fetch)
	}
	if !(sum < send && translate < send) {
		t.Errorf("send at %v is not drawn after sum at %v and translate at %v, which it waits for", send, sum, translate)
	}
	if sum != translate {
		t.Errorf("sum at %v and translate at %v run at the same time and are not drawn side by side", sum, translate)
	}
}

func Test_drawing_statechart_arrows_name_event_condition_and_timer(t *testing.T) {
	svg := draw(t, definition(t, review, map[string]string{"write.js": script}), nil)
	for _, label := range []string{">done<", ">failed<", ">verdict, when eve…<", ">too_long, 48h<"} {
		if !regexp.MustCompile(`<text class="on"[^>]*` + label).MatchString(svg) {
			t.Errorf("no arrow is labelled %s", label)
		}
	}
	if !strings.Contains(svg, `<text class="note"`) || !strings.Contains(svg, ">verdict ↩ drafting<") {
		t.Errorf("the transition back to an earlier state is not written in its box:\n%s", svg)
	}
	if !strings.Contains(svg, "<title>verdict, when event.data.ok</title>") {
		t.Errorf("a chip's whole label is not its tooltip:\n%s", svg)
	}
	if strings.Count(svg, `class="edge`) != 5 {
		t.Errorf("%d arrows for four forward transitions and the start:\n%s", strings.Count(svg, `class="edge`), svg)
	}
	if strings.Count(svg, `class="edge timer"`) != 1 {
		t.Errorf("the timer's arrow is not the one dashed arrow:\n%s", svg)
	}
	if !strings.Contains(svg, ">ticket verdict<") || !strings.Contains(svg, ">timer too_long<") {
		t.Errorf("the ticket and timer of waiting are not written in it")
	}
	if c := group(t, svg, "gave_up"); c != "state final failed" {
		t.Errorf("gave_up ends failed and is drawn as %q", c)
	}
	if strings.Count(svg, `class="tile`) != 1 {
		t.Errorf("a state with no job has an icon: only drafting, whose job is a script, has one:\n%s", svg)
	}
}

func Test_drawing_flow_has_one_arrow_for_each_wait_that_says_something(t *testing.T) {
	def := definition(t, `flow: chain
stages:
  a: { job: ./w.js }
  b: { job: ./w.js, follows: [a] }
  c: { job: ./w.js, follows: [a, b] }
`, map[string]string{"w.js": script})
	svg := draw(t, def, nil)
	if n := strings.Count(svg, `class="edge"`); n != 2 {
		t.Errorf("%d arrows: c waits for a through b, and needs none from a:\n%s", n, svg)
	}
}

func Test_drawing_chips_of_arrows_into_one_state_are_apart(t *testing.T) {
	chart := definition(t, `statechart: two
initial: s
states:
  s:
    ticket: go
    on: { left: a, right: b }
  a:
    ticket: more
    on: { ok: c }
  b:
    ticket: again
    on: { fine: c }
  c: { end: done }
`, nil)
	svg := draw(t, chart, nil)
	at := func(label string) string {
		m := regexp.MustCompile(`<text class="on" x="([0-9.]+)"[^>]*>` + label + `<`).FindStringSubmatch(svg)
		if m == nil {
			t.Fatalf("no chip says %s", label)
		}
		return m[1]
	}
	if at("ok") == at("fine") {
		t.Errorf("two chips are at x %s:\n%s", at("ok"), svg)
	}
}

func Test_drawing_holder_makes_room_for_its_notes(t *testing.T) {
	chart := definition(t, `statechart: held
initial: idle
states:
  idle:
    ticket: go
    on: { go: busy }
  busy:
    initial: one
    on: { failed: idle, cancel: idle, retry: busy }
    states:
      one:
        ticket: more
        on: { more: two }
      two: { end: done }
`, nil)
	svg := draw(t, chart, nil)
	last := 0.0
	for _, m := range regexp.MustCompile(`<text class="note" x="[0-9.]+" y="([0-9.]+)"`).FindAllStringSubmatch(svg, -1) {
		y, _ := strconv.ParseFloat(m[1], 64)
		last = max(last, y)
	}
	_, one := centre(t, template.HTML(svg), "one")
	if last == 0 || last+20 >= one {
		t.Errorf("the last note at %v overlaps the state inside at %v:\n%s", last, one, svg)
	}
	holder := regexp.MustCompile(`<g class="state holder"><rect x="[0-9.]+" y="([0-9.]+)" width="[0-9.]+" height="([0-9.]+)"`).FindStringSubmatch(svg)
	if holder == nil {
		t.Fatalf("busy is not drawn around its states:\n%s", svg)
	}
	top, _ := strconv.ParseFloat(holder[1], 64)
	high, _ := strconv.ParseFloat(holder[2], 64)
	_, two := centre(t, template.HTML(svg), "two")
	if top+high < two+16 {
		t.Errorf("the state inside at %v is below the holder's bottom at %v:\n%s", two, top+high, svg)
	}
}

func Test_drawing_of_a_big_machine_is_quick(t *testing.T) {
	var flow, chart strings.Builder
	flow.WriteString("flow: wide\nstages:\n")
	chart.WriteString("statechart: big\ninitial: s0\nstates:\n")
	for i := range 60 {
		var follows []string
		for j := range i {
			follows = append(follows, "f"+strconv.Itoa(j))
		}
		flow.WriteString("  f" + strconv.Itoa(i) + ": { job: ./w.js, follows: [" + strings.Join(follows, ", ") + "] }\n")
	}
	for i := range 200 {
		chart.WriteString("  s" + strconv.Itoa(i) + ":\n    ticket: t" + strconv.Itoa(i) + "\n    on:\n")
		for j := range 5 {
			chart.WriteString("      e" + strconv.Itoa(j) + ": s" + strconv.Itoa((i+j*j+1)%200) + "\n")
		}
	}
	for _, src := range []string{flow.String(), chart.String()} {
		def := definition(t, src, map[string]string{"w.js": script})
		began := time.Now()
		draw(t, def, nil)
		if took := time.Since(began); took > time.Second {
			t.Errorf("%s took %v to draw", def.Name, took)
		}
	}
}

func Test_drawing_run_is_marked_on_the_drawing(t *testing.T) {
	chart := definition(t, review, map[string]string{"write.js": script})
	svg := draw(t, chart, &Run{Steps: []Step{{Shown: states("drafting")}, {Shown: states("waiting"), Events: []string{"done"}}}})
	for name, want := range map[string]string{"waiting": "state now", "drafting": "state done", "approved": "state never final"} {
		if got := group(t, svg, name); got != want {
			t.Errorf("%s is drawn as %q, not %q", name, got, want)
		}
	}
	ended := draw(t, chart, &Run{Steps: []Step{{Shown: states("drafting")}, {Shown: states("waiting")}, {Shown: states("approved")}}, Ended: true})
	if got := group(t, ended, "approved"); got != "state done final" {
		t.Errorf("the state a run ended in is drawn as %q", got)
	}

	flow := definition(t, "flow: two\nstages:\n  a: { job: ./w.js }\n  b: { job: ./w.js, follows: [a] }\n  c: { job: ./w.js, follows: [b] }\n", map[string]string{"w.js": script})
	svg = draw(t, flow, &Run{Steps: []Step{{Shown: map[string]any{"stages": map[string]any{"a": "done", "b": "running", "c": "waiting"}}}}})
	for name, want := range map[string]string{"a": "state stage done", "b": "state stage now", "c": "state stage never"} {
		if got := group(t, svg, name); got != want {
			t.Errorf("stage %s is drawn as %q, not %q", name, got, want)
		}
	}

	librarian := definition(t, `agent: librarian
model: { name: claude-sonnet-5-5, secret: KEY }
prompt: hello
tools:
  shelf: { does: a shelf, args: { shelf: string }, job: ./w.js }
  helper: { does: a helper, args: { topic: string }, job: { machine: ./two.yaml } }
`, map[string]string{"w.js": script, "two.yaml": "flow: two\nstages:\n  a: { job: ./w.js }\n"})
	svg = draw(t, librarian, &Run{Steps: []Step{{Shown: map[string]any{"turn": 2.0, "waiting": "tools"}, Asked: []string{"shelf"}}}, Going: []string{"shelf"}})
	if got := group(t, svg, "shelf"); got != "state tool now" {
		t.Errorf("the tool the agent waits on is drawn as %q", got)
	}
	if got := group(t, svg, "helper"); got != "state tool never" {
		t.Errorf("a tool not in use is drawn as %q", got)
	}
	if !strings.Contains(svg, ">turn 2<") || !strings.Contains(svg, `class="tag" x=`) {
		t.Errorf("the agent's turn is not written beside its model")
	}
	if !strings.Contains(svg, ">machine: ./two.yaml<") {
		t.Errorf("the tool whose job is a machine does not say which")
	}

	// A state passed through within one step was entered: the run started
	// in drafting and left it in its first step.
	svg = draw(t, chart, &Run{Steps: []Step{{Shown: states("waiting"), Events: []string{"start", "done"}}}})
	if got := group(t, svg, "drafting"); got != "state done" {
		t.Errorf("the state the run started in and left in its first step is drawn as %q", got)
	}

	// An agent waiting on the ask: the ask is going, the tool it holds is
	// held, and a tool used at an earlier step is done.
	helper := definition(t, `agent: helper
model: { name: claude-sonnet-5-5, secret: KEY }
prompt: hello
tools:
  look: { does: looks, job: ./w.js }
  idle: { does: idles, job: ./w.js }
open:
  messages:
    does: messages
    ask: { machine: ./approval.yaml }
    tools:
      tell: { does: tells someone, job: { script: ./w.js, approve: true } }
`, map[string]string{"w.js": script, "approval.yaml": "flow: approval\nstages:\n  a: { job: ./w.js }\n"})
	svg = draw(t, helper, &Run{
		Steps: []Step{{Asked: []string{"look"}}, {Shown: map[string]any{"waiting": "tools"}, Asked: []string{"ask"}}},
		Going: []string{"tell", "ask"}, Held: []string{"tell"},
	})
	for name, want := range map[string]string{"ask": "state ask now", "tell": "state tool held", "look": "state tool done", "idle": "state tool never"} {
		if got := group(t, svg, name); got != want {
			t.Errorf("%s is drawn as %q, not %q", name, got, want)
		}
	}
}

func Test_drawing_transition_just_taken_is_marked(t *testing.T) {
	chart := definition(t, review, map[string]string{"write.js": script})
	svg := draw(t, chart, &Run{Steps: []Step{{Shown: states("drafting")}, {Shown: states("waiting"), Events: []string{"done"}}}})
	taken := regexp.MustCompile(`class="edge taken"`).FindAllString(svg, -1)
	if len(taken) != 1 {
		t.Fatalf("%d arrows are marked as taken, not one", len(taken))
	}
	if !regexp.MustCompile(`<path class="edge taken" d="M[^"]*"[^>]*/><rect class="chip"[^>]*/><text class="on"[^>]*>done</text>`).MatchString(svg) {
		t.Errorf("the arrow marked as taken is not the one done goes by")
	}
}

func Test_drawing_transition_back_is_marked_in_its_card(t *testing.T) {
	chart := definition(t, review, map[string]string{"write.js": script})
	svg := draw(t, chart, &Run{Steps: []Step{{Shown: states("waiting")}, {Shown: states("drafting"), Events: []string{"verdict"}}}})
	if !strings.Contains(svg, `class="note taken"`) || strings.Contains(svg, `class="edge taken"`) {
		t.Errorf("the transition back is not marked in its card alone:\n%s", svg)
	}
}

func Test_drawing_statechart_send_is_a_box(t *testing.T) {
	chart := definition(t, `statechart: listen
initial: hearing
states:
  hearing:
    job: ./w.js
    on:
      message: { send: { to: { machine: ./chat.yaml, key: "a" }, event: said } }
      again: hearing
      done: over
  over: { end: done }
`, map[string]string{"w.js": script, "chat.yaml": "flow: chat\nstages:\n  a: { job: ./w.js }\n"})
	svg, _ := Draw(chart, nil)
	_, hearing := centre(t, svg, "hearing")
	_, box := centre(t, svg, "chat.yaml")
	if box <= hearing {
		t.Errorf("the send's box at %v is not below the state at %v", box, hearing)
	}
	if !regexp.MustCompile(`<path class="edge send" d="[^"]*"[^>]*/><rect class="chip"[^>]*/><text class="on"[^>]*>message</text>`).MatchString(string(svg)) {
		t.Errorf("the send's arrow is not dashed and labelled with the event:\n%s", svg)
	}
	if !strings.Contains(group(t, string(svg), "chat.yaml"), "sends") {
		t.Errorf("the send's box is not marked as one")
	}
	if !strings.Contains(string(svg), ">again ↻<") || strings.Contains(string(svg), `>again</text>`) {
		t.Errorf("a rule back to the same state is not written in its card alone:\n%s", svg)
	}
}

func Test_drawing_agent_is_a_loop_of_model_and_tools(t *testing.T) {
	def := definition(t, `agent: helper
model: { name: claude-sonnet-5-5, secret: KEY }
prompt: hello
limits: { turns: 7 }
tools:
  look: { does: looks at a thing, job: ./w.js }
open:
  messages:
    does: messages
    ask: { machine: ./approval.yaml }
    tools:
      tell: { does: tells someone, job: { script: ./w.js, approve: true } }
`, map[string]string{"w.js": script, "approval.yaml": "flow: approval\nstages:\n  a: { job: ./w.js }\n"})
	svg, _ := Draw(def, nil)
	_, model := centre(t, svg, "claude-sonnet-5-5")
	_, tell := centre(t, svg, "tell")
	_, look := centre(t, svg, "look")
	_, ask := centre(t, svg, "ask")
	_, reply := centre(t, svg, "reply")
	if !(model < tell && model < look && tell < ask && ask < reply && look < reply) {
		t.Errorf("model %v, tools %v and %v, ask %v and reply %v are not drawn top to bottom", model, tell, look, ask, reply)
	}
	text := string(svg)
	for _, want := range []string{">messages<", ">tools<", ">7 turns<", ">approve: true<", ">machine: ./approval.yaml<", "<title>tells someone</title>"} {
		if !strings.Contains(text, want) {
			t.Errorf("the drawing does not hold %s", want)
		}
	}
	x := func(name string) float64 { v, _ := centre(t, svg, name); return v }
	if tools, parts := regexp.MustCompile(`class="name"[^>]*>tools<`).FindStringIndex(text), regexp.MustCompile(`class="name"[^>]*>messages<`).FindStringIndex(text); parts == nil || tools == nil {
		t.Errorf("the part and the tools are not held in boxes of their own")
	}
	if x("tell") == x("look") {
		t.Errorf("the part and the tools are not side by side")
	}
	if n := strings.Count(text, `class="edge`); n != 2 {
		t.Errorf("%d arrows, and an agent has one from its model to its tools and one to its reply:\n%s", n, text)
	}
	running, _ := Draw(def, &Run{Steps: []Step{{Shown: map[string]any{"waiting": "tools"}, Asked: []string{"blob_read"}}}, Going: []string{"blob_read"}})
	for _, svg := range []string{text, string(running)} {
		if strings.Contains(svg, "blob_read") || strings.Contains(svg, ">open<") {
			t.Errorf("a tool of Corbie's own is drawn, and no definition wrote it")
		}
	}
}

func Test_drawing_machine_unfolds_in_its_box(t *testing.T) {
	files := map[string]string{
		"w.js":       script,
		"sub/c.yaml": "flow: c\nstages:\n  deep: { job: w.js }\n",
		"b.yaml":     "flow: b\nstages:\n  inner: { job: { machine: ./sub/c.yaml } }\n",
	}
	src := `flow: a
stages:
  outer: { job: { machine: ./b.yaml } }
  plain: { job: ./w.js }
  named: { job: { machine: "${input.m}" } }
  here:
    job:
      machine:
        flow: written
        stages:
          there: { job: ./w.js }
`
	draft, err := machine.Parse([]byte(src), machine.Given(files))
	if err != nil {
		t.Fatal(err)
	}
	unfold := func(open ...string) string {
		u := Unfold{Open: map[string]bool{}, Href: func(path string) string { return "/go?to=" + path }}
		for _, p := range open {
			u.Open[p] = true
		}
		svg, err := Unfolded(draft, "dir/a.yaml", u)
		if err != nil {
			t.Fatal(err)
		}
		return string(svg)
	}
	folded := unfold()
	if !strings.Contains(folded, `<a href="/go?to=outer">`) || !strings.Contains(folded, `<a href="/go?to=here">`) {
		t.Errorf("a box whose job names a machine is not a link:\n%s", folded)
	}
	if strings.Contains(folded, "/go?to=plain") || strings.Contains(folded, "/go?to=named") {
		t.Errorf("a script or a machine that is an expression has a link:\n%s", folded)
	}
	if strings.Contains(folded, ">inner<") {
		t.Errorf("a box that is not unfolded shows what is inside it")
	}
	if !strings.Contains(folded, ">flow: written<") {
		t.Errorf("a machine written in place does not say what it is:\n%s", folded)
	}
	one := unfold("outer")
	if !strings.Contains(one, ">inner<") || strings.Contains(one, ">deep<") {
		t.Errorf("one box unfolded does not show one level:\n%s", one)
	}
	if !strings.Contains(one, `<a href="/go?to=outer/inner">`) {
		t.Errorf("a box inside an unfolded box has no link by its path:\n%s", one)
	}
	two := unfold("outer", "outer/inner", "here")
	for _, want := range []string{">deep<", ">there<"} {
		if !strings.Contains(two, want) {
			t.Errorf("%s is not drawn when its boxes are unfolded", want)
		}
	}
	if strings.Contains(unfold("outer/inner"), ">deep<") {
		t.Errorf("a box inside a folded box is drawn")
	}
	if want := `height="`; !strings.Contains(two, want) {
		t.Fatal(two)
	}
	size := func(svg string) float64 {
		m := regexp.MustCompile(`height="([0-9.]+)"`).FindStringSubmatch(svg)
		v, _ := strconv.ParseFloat(m[1], 64)
		return v
	}
	if size(one) <= size(folded) {
		t.Errorf("an unfolded box does not make room for what it holds: %v and %v", size(one), size(folded))
	}
	if !strings.Contains(folded, ">flow: b<") {
		t.Errorf("a box whose job names a machine does not say its type and name:\n%s", folded)
	}

	// On a run's page, a card links to the run its job started, and says
	// what that run is, as on the Machines page.
	run := string(must(Draw(draft.Definition, &Run{Children: map[string]Child{"outer": {ID: "a-client/1", Machine: "b", Type: "flow"}}})))
	if !strings.Contains(run, `<a href="/run/a-client/1">`) || !strings.Contains(run, ">flow: b<") {
		t.Errorf("a card whose job started a run does not link to it:\n%s", run)
	}
	if strings.Contains(run, "/go?to=") || strings.Count(run, "<a href") != 1 {
		t.Errorf("a card whose job started no run has a link on a run's page:\n%s", run)
	}
}

func must(svg template.HTML, err error) template.HTML {
	if err != nil {
		panic(err)
	}
	return svg
}
