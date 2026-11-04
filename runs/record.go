package runs

import (
	"cmp"
	"encoding/json/v2"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zummed/corbie-ui/page"
	"github.com/zummed/corbie/client"
	"github.com/zummed/corbie/instance"
	"github.com/zummed/corbie/machine"
	"github.com/zummed/corbie/store"
)

// A line is one line of a run's record, in the words `corbie run show
// --record` prints: when, which run by its machine, the step, which way it
// went, and what. The line a run ends with has its status.
type line struct {
	Time    string
	Machine string
	Step    int
	Way     string // "in" for what arrived, "out" for what the step asked
	Text    string
	End     string
}

// lines gives an entry of a record as Corbie's command line prints it,
// which it keeps to itself, with the run named as name, and its time as
// the steps of a run that started on day show it.
func lines(e client.Entry, name string, day time.Time) []line {
	at := page.Clock(e.Time, day)
	if e.Step == 0 {
		data, _ := json.Marshal(e.Data, json.Deterministic(true))
		text := string(data)
		if s, ok := e.Data.(string); ok {
			text = s
		}
		return []line{{Time: at, Machine: name, Text: fmt.Sprintf("%s %s: %s", e.Job, e.Name, text)}}
	}
	var out []line
	add := func(way, text string) {
		out = append(out, line{Time: at, Machine: name, Step: e.Step, Way: way, Text: text})
	}
	for _, event := range e.Events {
		text := event.Name
		if event.From != "" {
			text += " from " + event.From
		}
		if failed, _ := event.Data.(map[string]any); failed["refused"] != nil {
			text += fmt.Sprintf(" denied: %v", failed["refused"])
		} else if event.Data != nil {
			data, _ := json.Marshal(event.Data, json.Deterministic(true))
			text += " " + clip(string(data))
		}
		add("in", text)
	}
	for _, ask := range e.Asks {
		text := ""
		switch ask.Kind {
		case instance.AskScript:
			text = fmt.Sprintf("job %s: a script, on %s", ask.Name, ask.Runner)
		case instance.AskChild:
			text = fmt.Sprintf("job %s: a machine, %s", ask.Name, ask.Child)
		case instance.AskCancel:
			text = "cancel " + ask.Name
		case instance.AskEvent:
			to := "its parent"
			if ask.Name != "" {
				to = ask.Name
			}
			if ask.Key != "" || ask.Text != "" {
				to = fmt.Sprintf("the key %q", ask.Key)
			}
			text = fmt.Sprintf("send %s to %s", ask.Event.Name, to)
			if ask.Undelivered {
				text += " (undelivered: " + cmp.Or(ask.Text, "it is not running") + ")"
			}
		case instance.AskFail:
			text = fmt.Sprintf("%s fails: %s", ask.Name, ask.Error.Error)
			if ask.Error.Refused != "" {
				text = fmt.Sprintf("%s fails, denied: %s", ask.Name, ask.Error.Refused)
			}
		case instance.AskHold:
			text = fmt.Sprintf("hold %s for the ask", ask.Name)
		case instance.AskNote:
			text = ask.Text
		default:
			continue
		}
		add("out", text)
	}
	if e.Status != instance.Running {
		add("", e.Status)
		out[len(out)-1].End = e.Status
	}
	return out
}

func clip(text string) string {
	if len(text) > 200 {
		return text[:200] + "…"
	}
	return text
}

// A stepRow is one thing that happened in a run, as Steps lists it: when,
// which run, and what, in words. A row that started a child holds the
// child's rows, and a send links to the run it reached.
type stepRow struct {
	Key   string
	Run   link
	Step  int
	At    time.Time
	Time  string
	Exact string
	Depth int
	What  []word
	// Kind colours a row: send and got for a send between runs, held,
	// failed, and the status a run ended with.
	Kind  string
	Group *group
	// What the row's details show: its facts, and what went in and came
	// out.
	Facts   []fact
	In, Out any
}

// A word is part of what a row says; a name is bold, and the run a send
// went to or came from is a link to it.
type word struct {
	Text string
	Bold bool
	Run  *link
}

// A fact is one line of a row's details, with the run it names.
type fact struct {
	Name, Text string
	Run        *link
}

// A group is a child run under the row that started it: its status, how
// many steps it and the runs under it took, how long it took, and its
// rows.
type group struct {
	Run             link
	Status, Failure string
	Ended           bool
	Steps           int
	Took            string
	Rows            []stepRow
}

// Unix is when a row happened, as the page orders rows.
func (r stepRow) Unix() int64 { return r.At.UnixMilli() }

// Said is what a row says, as plain words.
func (r stepRow) Said() string {
	var b strings.Builder
	for _, w := range r.What {
		b.WriteString(w.Text)
	}
	return b.String()
}

// runName is what a run's page calls a run: in its title line, its path,
// its rows and their details.
func runName(r *client.Run) string { return r.Machine }

// linkOf is a run as a row names it: by its name where the list holds it,
// and by the end of its ID where it does not.
func linkOf(id string, byID map[string]*client.Run) link {
	if r := byID[id]; r != nil {
		return link{ID: id, Machine: runName(r), Type: r.Type}
	}
	return link{ID: id, Machine: id[strings.LastIndexByte(id, '-')+1:]}
}

// rowsOf gives a run's rows, from its record and its children's, each
// child's under the row that started it. A row's time has its date where it
// is not on day's date.
func rowsOf(id string, entries []client.Entry, byID map[string]*client.Run, now, day time.Time) []stepRow {
	of := map[string][]client.Entry{}
	for _, e := range entries {
		if e.Step > 0 {
			of[e.ID] = append(of[e.ID], e)
		}
	}
	b := &rowBuilder{of: of, byID: byID, now: now, day: day}
	return b.rows(id, 0)
}

type rowBuilder struct {
	of       map[string][]client.Entry
	byID     map[string]*client.Run
	now, day time.Time
}

// steps counts the steps of a run and of every run under it.
func (b *rowBuilder) steps(id string) int {
	n := len(b.of[id])
	for _, e := range b.of[id] {
		for _, a := range e.Asks {
			if a.Kind == instance.AskChild && a.Child != "" {
				n += b.steps(a.Child)
			}
		}
	}
	return n
}

func (b *rowBuilder) rows(id string, depth int) []stepRow {
	run := linkOf(id, b.byID)
	own := b.of[id]
	var out []stepRow
	var prev any
	turn := 0
	jobs := map[string]*instance.Ask{} // the latest ask that started each job, by its name
	for i, e := range own {
		n := 0
		add := func(r stepRow) *stepRow {
			if r.Key == "" {
				r.Key = e.ID + "/" + strconv.Itoa(e.Step) + "/" + strconv.Itoa(n)
				n++
			}
			r.Run, r.Step, r.At, r.Time, r.Depth = run, e.Step, e.Time, page.Clock(e.Time, b.day), depth
			r.Exact = e.Time.Local().Format("2 Jan 2006 15:04:05.000")
			out = append(out, r)
			return &out[len(out)-1]
		}
		if e.Step == 1 && depth == 0 {
			r := stepRow{Key: e.ID + "/started", What: say("started")}
			for _, ev := range e.Events {
				if ev.Name == machine.Start {
					r.In = ev.Data
				}
			}
			if h := b.byID[id]; h != nil {
				r.Out = h.Output
			}
			add(r)
		}
		for _, ev := range e.Events {
			switch {
			case ev.Name == machine.Start:
			case store.CheckInstance(ev.From) == nil:
				from := linkOf(ev.From, b.byID)
				add(stepRow{What: say("⇠ ", bold(ev.Name), " from ", linked(&from)), Kind: "got", In: ev.Data,
					Facts: []fact{{Name: "event", Text: ev.Name}, {Name: "from", Text: from.ID, Run: &from}}})
			case ev.Name == machine.Timer:
				r := stepRow{What: say("timer ", bold(jobOf(ev.From)), " fired"), Facts: []fact{{Name: "timer", Text: jobOf(ev.From)}}}
				if s, _ := ev.Data.(string); s != "" {
					if due, err := time.Parse(time.RFC3339Nano, s); err == nil {
						r.Facts = append(r.Facts, fact{Name: "due", Text: page.When(due, b.now)})
					}
				}
				add(r)
			case ev.Answer:
				add(stepRow{What: say("⇠ ", bold(ev.Name), " answered"), Kind: "got", In: ev.Data, Facts: []fact{{Name: "ticket", Text: ev.Name}}})
			case ev.Name == machine.Failed && run.Type != "flow":
				r := stepRow{What: say(bold(jobOf(ev.From)), " failed"), Kind: "failed", Out: ev.Data, Facts: jobFacts(jobs[named(ev.From)], b.byID)}
				if a := jobs[named(ev.From)]; a != nil && a.Job != nil {
					r.In = a.Job.Input
				}
				r.Facts = append(r.Facts, fact{Name: "error", Text: failure(ev.Data)})
				add(r)
			case run.Type == "agent" && ev.Name == machine.Done && isTurn(jobs[named(ev.From)]):
				data, _ := ev.Data.(map[string]any)
				calls, _ := data["calls"].([]any)
				for _, c := range calls {
					call, _ := c.(map[string]any)
					name, _ := call["name"].(string)
					add(stepRow{What: say("turn "+strconv.Itoa(turn)+" called ", bold(name), "("+argsOf(call["args"])+")"), In: call["args"], Out: b.resultOf(own[i+1:], name),
						Facts: []fact{{Name: "tool", Text: name}, {Name: "args", Text: compact(call["args"])}, {Name: "turn " + strconv.Itoa(turn), Text: turnFacts(data)}}})
				}
				if len(calls) == 0 {
					text, _ := data["text"].(string)
					add(stepRow{What: say("turn "+strconv.Itoa(turn)+" replied ", clip(text)), Out: ev.Data,
						Facts: []fact{{Name: "turn " + strconv.Itoa(turn), Text: turnFacts(data)}}})
				}
			case run.Type == "agent" && ev.Name == machine.Done:
				r := stepRow{What: say(bold(jobOf(ev.From)), " done"), Out: ev.Data, Facts: jobFacts(jobs[named(ev.From)], b.byID)}
				if a := jobs[named(ev.From)]; a != nil && a.Job != nil {
					r.In = a.Job.Input
				}
				add(r)
			}
		}
		switch run.Type {
		case "statechart":
			was, now := statesOf(prev), statesOf(e.Shown)
			left := slices.DeleteFunc(slices.Clone(was), func(s string) bool { return slices.Contains(now, s) })
			entered := slices.DeleteFunc(slices.Clone(now), func(s string) bool { return slices.Contains(was, s) })
			var w []word
			if len(left) > 0 {
				w = append(say("left "), names(left)...)
			}
			if len(entered) > 0 {
				if w != nil {
					w = append(w, word{Text: ", "})
				}
				w = append(append(w, word{Text: "entered "}), names(entered)...)
			}
			if w != nil {
				add(stepRow{What: w, Facts: []fact{{Name: "states", Text: strings.Join(now, ", ")}}})
			}
		case "flow":
			was, now := stagesOf(prev), stagesOf(e.Shown)
			for _, s := range slices.Sorted(maps.Keys(now)) {
				st := now[s]
				if st == was[s] || (st != instance.Done && st != instance.Failed && st != "skipped") {
					continue
				}
				r := stepRow{What: say("stage ", bold(s), " "+st), Facts: []fact{{Name: "stage", Text: s}, {Name: "status", Text: st}}}
				if st == instance.Failed {
					r.Kind = "failed"
				}
				for _, ev := range e.Events {
					if jobOf(ev.From) == s && (ev.Name == machine.Done || ev.Name == machine.Failed) {
						r.Out = ev.Data
						if ev.Name == machine.Failed {
							r.Facts = append(r.Facts, fact{Name: "error", Text: failure(ev.Data)})
						}
						if a := jobs[named(ev.From)]; a != nil && a.Job != nil {
							r.In = a.Job.Input
						}
					}
				}
				add(r)
			}
		}
		for k := range e.Asks {
			a := &e.Asks[k]
			if a.Name != "" && (a.Kind == instance.AskScript || a.Kind == instance.AskChild || a.Kind == instance.AskHold) {
				jobs[named(a.Name)] = a
			}
			switch a.Kind {
			case instance.AskChild:
				child := linkOf(a.Child, b.byID)
				name := jobOf(a.Name)
				var w []word
				switch {
				case run.Type == "flow":
					w = say("stage ", bold(name), " started ", bold(child.Machine))
				case run.Type == "statechart":
					w = say("state ", bold(name), " started ", bold(child.Machine))
				case name == instance.AskName:
					w = say("ask started ", bold(child.Machine))
				default:
					w = say(bold(name), " started ", bold(child.Machine))
				}
				r := stepRow{What: w, Facts: jobFacts(a, b.byID), Out: b.resultOf(own[i+1:], a.Name)}
				if a.Job != nil {
					r.In = a.Job.Input
				}
				g := &group{Run: child, Steps: b.steps(a.Child), Rows: b.rows(a.Child, depth+1)}
				if h := b.byID[a.Child]; h != nil {
					g.Status, g.Failure, g.Ended = h.Status, h.Failure, h.HasEnded()
					if g.Ended {
						g.Took = took(h.Ended.Sub(h.Started))
					}
				}
				r.Group = g
				add(r)
			case instance.AskScript:
				if run.Type == "agent" && isTurn(a) {
					turn++
					model := modelOf(a.Job.Input)
					add(stepRow{What: say("turn "+strconv.Itoa(turn)+" to ", bold(model)), Facts: []fact{{Name: "turn", Text: strconv.Itoa(turn)}, {Name: "model", Text: model}, {Name: "runner", Text: a.Runner}}})
				}
			case instance.AskWake:
				if name, ok := strings.CutPrefix(a.What, "timer."); ok {
					name = jobOf(name)
					add(stepRow{What: say("timer ", bold(name), " set, due "+page.When(a.At, b.now)), Facts: []fact{{Name: "timer", Text: name}, {Name: "due", Text: page.When(a.At, b.now)}}})
				}
			case instance.AskEvent:
				add(b.sendOf(a))
			case instance.AskHold:
				r := stepRow{What: say("held ", bold(jobOf(a.Name)), " for approval"), Kind: "held", Facts: jobFacts(a, b.byID)}
				if a.Job != nil {
					r.In = a.Job.Input
				}
				for _, c := range e.Asks {
					if c.Kind == instance.AskChild && jobOf(c.Name) == instance.AskName {
						ask := linkOf(c.Child, b.byID)
						r.Facts = append(r.Facts, fact{Name: "held for", Text: c.Child + statusOf(b.byID[c.Child]), Run: &ask})
					}
				}
				add(r)
			case instance.AskFail:
				r := stepRow{What: say(bold(jobOf(a.Name)), " failed"), Kind: "failed", Facts: []fact{{Name: "job", Text: a.Name}}}
				if a.Error != nil {
					r.Facts = append(r.Facts, fact{Name: "error", Text: a.Error.Error})
					if a.Error.Refused != "" {
						r.What = say(bold(jobOf(a.Name)), " denied")
						r.Facts = append(r.Facts, fact{Name: "refused", Text: a.Error.Refused}, fact{Name: "fix", Text: a.Error.Fix})
					}
				}
				add(r)
			case instance.AskCancel:
				add(stepRow{What: say("cancelled ", bold(jobOf(a.Name))), Facts: []fact{{Name: "job", Text: a.Name}}})
			case instance.AskNote:
				add(stepRow{What: say(a.Text), Facts: []fact{{Name: "note", Text: a.Text}}})
			}
		}
		if e.Status != instance.Running && e.Status != "" {
			r := stepRow{What: say(e.Status), Kind: e.Status, Facts: []fact{{Name: "status", Text: e.Status}}}
			if h := b.byID[id]; h != nil {
				r.Out = h.Output
				if h.Failure != "" {
					r.What = say(e.Status + " (" + h.Failure + ")")
					r.Facts = append(r.Facts, fact{Name: "failure", Text: h.Failure})
				}
				if h.Error != "" {
					r.Facts = append(r.Facts, fact{Name: "error", Text: h.Error})
				}
			}
			add(r)
		}
		prev = e.Shown
	}
	return out
}

// sendOf is the row of an event a run sent: to a key, to a job's child, or
// to the run that started it, linked to the run it went to.
func (b *rowBuilder) sendOf(a *instance.Ask) stepRow {
	event := ""
	var data any
	if a.Event != nil {
		event, data = a.Event.Name, a.Event.Data
	}
	r := stepRow{In: data, Facts: []fact{{Name: "event", Text: event}}}
	var to *link
	if a.To != "" {
		l := linkOf(a.To, b.byID)
		to = &l
	}
	switch {
	case a.Key != "" || a.Text != "":
		r.Kind = "send"
		r.What = say("⇢ sent ", bold(event), " to the key ", bold(a.Key))
		if to != nil {
			r.What[3].Run = to
			r.What = append(r.What, word{Text: " · "}, linked(to))
		}
		r.Facts = append(r.Facts, fact{Name: "key", Text: a.Key})
	case to != nil:
		r.What = say("sent ", bold(event), " to ", linked(to))
	case a.Name != "":
		r.What = say("sent ", bold(event), " to ", bold(a.Name))
	default:
		r.What = say("sent ", bold(event), " to its parent")
	}
	if to != nil {
		r.Facts = append(r.Facts, fact{Name: "reached", Text: to.ID + statusOf(b.byID[to.ID]), Run: to})
	}
	if a.Undelivered {
		r.What = append(r.What, word{Text: " · undelivered"})
		r.Facts = append(r.Facts, fact{Name: "undelivered", Text: cmp.Or(a.Text, "it is not running")})
	}
	return r
}

// resultOf gives what a job gave, from the first of entries that took its
// end: its output, or its error.
func (b *rowBuilder) resultOf(entries []client.Entry, job string) any {
	for _, e := range entries {
		for _, ev := range e.Events {
			if (ev.Name == machine.Done || ev.Name == machine.Failed) && (named(ev.From) == named(job) || jobOf(ev.From) == job) {
				return ev.Data
			}
		}
	}
	return nil
}

// jobFacts are what a row's details say of the job an ask started.
func jobFacts(a *instance.Ask, byID map[string]*client.Run) []fact {
	if a == nil {
		return nil
	}
	facts := []fact{{Name: "job", Text: a.Name}}
	if j := a.Job; j != nil {
		switch {
		case j.Machine != "":
			facts = append(facts, fact{Name: "machine", Text: j.Machine})
		case j.Script != "":
			facts = append(facts, fact{Name: "script", Text: j.Script})
		}
		if j.Approve {
			facts = append(facts, fact{Name: "approve", Text: "true"})
		}
		if shown, _ := j.Shown.(map[string]any); shown != nil {
			if part, _ := shown["part"].(string); part != "" {
				facts = append(facts, fact{Name: "part", Text: part})
			}
		}
	}
	if a.Runner != "" {
		facts = append(facts, fact{Name: "runner", Text: a.Runner})
	}
	if a.Child != "" {
		child := linkOf(a.Child, byID)
		facts = append(facts, fact{Name: "run", Text: a.Child + statusOf(byID[a.Child]), Run: &child})
	}
	return facts
}

// statusOf is a run's status after its ID, as a fact gives it.
func statusOf(r *client.Run) string {
	if r == nil {
		return ""
	}
	if r.Failure != "" {
		return ", " + r.Status + " (" + r.Failure + ")"
	}
	return ", " + r.Status
}

// isTurn says whether a job is an agent's turn: Corbie's own script that
// asks the model.
func isTurn(a *instance.Ask) bool {
	return a != nil && a.Job != nil && jobOf(a.Name) == "turn" && strings.HasPrefix(a.Job.Script, "corbie:")
}

// modelOf gives the name of the model a turn asks.
func modelOf(input any) string {
	in, _ := input.(map[string]any)
	model, _ := in["model"].(map[string]any)
	name, _ := model["name"].(string)
	return cmp.Or(name, "the model")
}

// turnFacts are what a turn's details say of it: why it stopped, its
// text, and what it used.
func turnFacts(data map[string]any) string {
	var facts []string
	if stop, _ := data["stop"].(string); stop != "" {
		facts = append(facts, "stop: "+stop)
	}
	if text, _ := data["text"].(string); text == "" {
		facts = append(facts, "no text")
	} else {
		facts = append(facts, clip(text))
	}
	if usage, _ := data["usage"].(map[string]any); usage != nil {
		facts = append(facts, fmt.Sprintf("%v in, %v out", usage["input"], usage["output"]))
	}
	return strings.Join(facts, " · ")
}

// argsOf gives a call's arguments in a few words: name: value, each value
// cut short.
func argsOf(args any) string {
	m, ok := args.(map[string]any)
	if !ok {
		return compact(args)
	}
	var out []string
	for _, k := range slices.Sorted(maps.Keys(m)) {
		v := compact(m[k])
		if s, ok := m[k].(string); ok {
			v = "“" + s + "”"
		}
		if len(v) > 40 {
			v = v[:40] + "…"
		}
		out = append(out, k+": "+v)
	}
	return strings.Join(out, ", ")
}

// compact gives a value as JSON on one line.
func compact(v any) string {
	if v == nil {
		return ""
	}
	data, _ := json.Marshal(v, json.Deterministic(true))
	return string(data)
}

// failure gives the error a failed event carries.
func failure(data any) string {
	m, _ := data.(map[string]any)
	if refused, _ := m["refused"].(string); refused != "" {
		return "denied: " + refused
	}
	if text, _ := m["error"].(string); text != "" {
		return text
	}
	return compact(data)
}

func statesOf(shown any) []string {
	m, _ := plain(shown).(map[string]any)
	list, _ := m["states"].([]any)
	var out []string
	for _, s := range list {
		if s, ok := s.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func stagesOf(shown any) map[string]string {
	m, _ := plain(shown).(map[string]any)
	stages, _ := m["stages"].(map[string]any)
	out := map[string]string{}
	for k, v := range stages {
		out[k], _ = v.(string)
	}
	return out
}

// say makes words of strings and words.
func say(parts ...any) []word {
	var out []word
	for _, p := range parts {
		switch p := p.(type) {
		case string:
			out = append(out, word{Text: p})
		case word:
			out = append(out, p)
		}
	}
	return out
}

func bold(text string) word { return word{Text: text, Bold: true} }

// linked is a run named in bold, as a link to it.
func linked(l *link) word { return word{Text: l.Machine, Bold: true, Run: l} }

// named gives a job's name as the event of its end gives it: without the
// number of the ask or the attempt that Corbie writes after it.
func named(name string) string {
	if i := strings.IndexAny(name, "#@"); i > 0 {
		return name[:i]
	}
	return name
}

// names gives names in bold, joined by "and".
func names(list []string) []word {
	var out []word
	for i, s := range list {
		if i > 0 {
			out = append(out, word{Text: " and "})
		}
		out = append(out, bold(s))
	}
	return out
}
