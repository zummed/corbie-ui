package machines

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// partsProject adds to a project another agent that opens the part, written in
// place, on a timer; a part nothing opens; a machine with problems; a machine
// in a dotted directory that nothing names; and another part of the same name.
func partsProject(t *testing.T) string {
	t.Helper()
	dir := project(t)
	for name, text := range map[string]string{
		"timed.yaml": `statechart: timed
initial: waiting
states:
  waiting:
    timers: { b: tue 07:30 UTC, a: mon 07:30 UTC, c: 23:50 UTC }
    on: { a: working, b: working, c: working }
  working:
    job:
      machine:
        flow: check
        stages:
          look:
            job:
              machine:
                agent: looker
                model: { name: claude-sonnet-5-5, secret: KEY }
                prompt: look
                open:
                  messages: { include: ./parts/part.yaml }
          again:
            job:
              machine:
                agent: looker
                model: { name: claude-sonnet-5-5, secret: KEY }
                prompt: look again
                open:
                  messages: { include: ./parts/part.yaml }
    on: { done: waiting, failed: waiting }
`,
		"own.yaml": `agent: own
model: { name: claude-sonnet-5-5, secret: KEY }
prompt: hello
open:
  messages:
    does: other messages
    tools:
      read: { does: reads, job: { script: ./own.js, allow: ["net:example.com:443"] } }
`,
		"own.js":               "return 1",
		".kit/dead.yaml":       "flow: dead\nstages:\n  a: { job: { machine: behind.yaml } }\n",
		"behind.yaml":          "agent: behind\nmodel: { name: claude-sonnet-5-5, secret: KEY }\nprompt: hello\nopen:\n  zed: { does: zed, tools: { read: { does: reads, job: { script: ./own.js } } } }\n",
		"parts/new/part.yaml":  "does: something new\ntools:\n  try: { does: tries, job: { script: ./try.js, approve: true } }\n",
		"parts/new/try.js":     "return 1",
		".kit/part.yaml":       "does: kept in a dotted directory\ntools: {}\n",
		"side.yaml":            "flow: side\nstages:\n  a: { job: { machine: ./side.yml } }\n",
		"side.yml":             "agent: sider\nmodel: { name: claude-sonnet-5-5, secret: KEY }\nprompt: hello\nopen:\n  yml: { does: from a yml, tools: { read: { does: reads, job: { script: ./own.js } } } }\n",
		"based.yaml":           "agent: based\ninclude: ./shared.yaml\nmodel: { name: claude-sonnet-5-5, secret: KEY }\nprompt: hello\n",
		"shared.yaml":          "open:\n  kept: { include: ./parts/kept/part.yaml }\n",
		"parts/kept/part.yaml": "does: kept in a shared file\ntools:\n  read: { does: reads, job: { script: own.js } }\n",
		"odd.yaml":             "agent: odd\ndoes: a machine with problems\ntools: {}\n",
		"bad.yaml":             "flow: bad\nstages: {}\ncolour: blue\n",
		"data.yaml":            "does: not a part\n",
	} {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
		os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644)
	}
	return dir
}

func partsView_(t *testing.T, dir, open string) partsView {
	t.Helper()
	d, err := newDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return d.partsOf(open)
}

func byDoes(t *testing.T, v partsView, does string) part {
	t.Helper()
	for _, p := range v.Parts {
		if p.Does == does {
			return p
		}
	}
	t.Fatalf("no part does %q: %+v", does, v.Parts)
	return part{}
}

func openedBy(p part) []string {
	var names []string
	for _, o := range p.Opened {
		names = append(names, o.Name)
	}
	return names
}

func startsOf(o opener) []string {
	var names []string
	for _, s := range o.Starts {
		names = append(names, s.Name)
	}
	return names
}

func Test_machines_parts_show_what_opens_each(t *testing.T) {
	dir := partsProject(t)
	v := partsView_(t, dir, "")
	var ids []string
	for _, p := range v.Parts {
		ids = append(ids, p.ID)
	}
	if got := strings.Join(ids, " "); got != "kept messages messages-2 new yml zed" {
		t.Fatalf("the parts are %q: %+v", got, v.Unread)
	}
	if v.Open == nil || v.Open.ID != "kept" {
		t.Errorf("the first part is not open: %+v", v.Open)
	}
	if !slices.Equal(v.Problem, []string{"bad.yaml", "odd.yaml"}) {
		t.Errorf("the machines with problems are %v", v.Problem)
	}

	var shared, own, unopened, zed, yml *part
	for i := range v.Parts {
		switch v.Parts[i].ID {
		case "new":
			unopened = &v.Parts[i]
		case "zed":
			zed = &v.Parts[i]
		case "yml":
			yml = &v.Parts[i]
		}
		if v.Parts[i].Does == "messages" {
			shared = &v.Parts[i]
		}
		if v.Parts[i].Does == "other messages" {
			own = &v.Parts[i]
		}
	}
	if shared == nil || own == nil {
		t.Fatalf("a part is missing: %+v", v.Parts)
	}
	if v.Parts[0].File != "parts/kept/part.yaml" {
		t.Errorf("a part under an `open:` in an included file has the file %q", v.Parts[0].File)
	}
	if got := strings.Join(openedBy(*shared), " "); got != "helper looker" {
		t.Errorf("the shared part is opened by %q", got)
	}
	if shared.File != "parts/part.yaml" || shared.Ask != ".kit/say.yaml" {
		t.Errorf("the shared part's file is %q and its ask %q", shared.File, shared.Ask)
	}
	helper, looker := shared.Opened[0], shared.Opened[1]
	if got := strings.Join(startsOf(helper), " "); got != "bridge" {
		t.Errorf("helper is started by %q", got)
	}
	if helper.Starts[0].File != "telegram.yaml" {
		t.Errorf("the start is %+v", helper.Starts[0])
	}
	if looker.File != "timed.yaml" || len(looker.Starts) != 1 {
		t.Fatalf("looker is %+v", looker)
	}
	if s := looker.Starts[0]; s.Timer != "23:50 UTC +2" || s.Timers != "23:50 UTC, mon 07:30 UTC, tue 07:30 UTC" {
		t.Errorf("the timers are %q and %q", s.Timer, s.Timers)
	}
	if len(shared.Tools) != 1 || !shared.Tools[0].Held || shared.Tools[0].Job != "machine" || shared.Tools[0].File != ".kit/say.yaml" {
		t.Errorf("the tool is %+v", shared.Tools)
	}

	if len(own.Opened) != 1 || own.File != "" {
		t.Fatalf("the agent's own part is %+v", own)
	}
	if s := own.Opened[0].Starts; len(s) != 0 {
		t.Errorf("an agent in its own file that nothing names has a machine above it: %+v", s)
	}
	if s := zed.Opened[0].Starts; len(s) != 0 {
		t.Errorf("a machine in a dotted directory is listed as a start: %+v", s)
	}
	if tool := own.Tools[0]; tool.Job != "script" || tool.File != "own.js" || !slices.Equal(tool.Allow, []string{"net:example.com:443"}) {
		t.Errorf("the tool is %+v", tool)
	}

	if s := yml.Opened[0].Starts; len(s) != 1 || s[0].File != "side.yaml" {
		t.Errorf("the machine above a .yml file is %+v", s)
	}
	if len(unopened.Opened) != 0 || unopened.File != "parts/new/part.yaml" || !unopened.Tools[0].Held || unopened.Tools[0].File != "parts/new/try.js" {
		t.Errorf("the part nothing opens is %+v", unopened)
	}

	h, err := New(Config{Dir: dir, Interval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	body := do(h, "GET", "/parts?part=new", nil).Body.String()
	if body := do(h, "GET", "/parts?part=zed", nil).Body.String(); !strings.Contains(body, "only by hand") {
		t.Errorf("an agent no machine starts does not say so:\n%s", body)
	}
	for _, want := range []string{"No agent that can be read opens it", "needs approval", "bad.yaml", `href="/machines?file=parts%2fnew%2fpart.yaml&amp;view=text"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not show %q:\n%s", want, body)
		}
	}
	if w := do(h, "GET", "/parts?part=gone", nil); w.Code != http.StatusNotFound {
		t.Errorf("a part that is not there answered %d", w.Code)
	}
	if w := do(h, "GET", "/parts?part=../../etc/passwd", nil); w.Code != http.StatusNotFound {
		t.Errorf("a path answered %d", w.Code)
	}
}

func Test_machines_parts_allow_covers_the_machines_a_job_runs(t *testing.T) {
	dir := project(t)
	os.WriteFile(filepath.Join(dir, ".kit/say.yaml"), []byte(`flow: say
allow: ["net:top.example:443"]
stages:
  a: { job: { script: ./hear.js, allow: ["net:stage.example:443"] } }
  b: { job: { machine: ./deep.yaml } }
`), 0o644)
	os.WriteFile(filepath.Join(dir, ".kit/deep.yaml"), []byte("flow: deep\nstages:\n  a: { job: { script: ./hear.js, allow: [\"secret:KEY@net:deep.example:443\"] } }\n"), 0o644)
	v := partsView_(t, dir, "")
	if len(v.Parts) != 1 {
		t.Fatalf("the parts are %+v %v", v.Parts, v.Unread)
	}
	want := []string{"net:stage.example:443", "net:top.example:443", "secret:KEY@net:deep.example:443"}
	if got := v.Parts[0].Tools[0].Allow; !slices.Equal(got, want) {
		t.Errorf("allow is %v", got)
	}
}

func Test_machines_parts_show_rules_only_in_a_rook_project(t *testing.T) {
	dir := partsProject(t)
	os.WriteFile(filepath.Join(dir, "rules.yaml"), []byte("agents:\n  - { name: helper, for: [alice, staff] }\n  - { name: nobody, for: [other] }\napprove:\n  messages: [alice]\n"), 0o644)

	v := partsView_(t, dir, "")
	if p := byDoes(t, v, "messages"); v.Rook || p.Approvers != nil || p.Opened[0].For != nil {
		t.Errorf("a directory with no .rook/ read the rules: %+v", v)
	}
	h, _ := New(Config{Dir: dir, Interval: time.Second})
	if body := do(h, "GET", "/parts?part="+byDoes(t, v, "messages").ID, nil).Body.String(); strings.Contains(body, "<th>For</th>") || strings.Contains(body, "Approved by") {
		t.Errorf("a directory with no .rook/ shows the rules:\n%s", body)
	}

	os.MkdirAll(filepath.Join(dir, ".rook"), 0o755)
	v = partsView_(t, dir, "")
	if !v.Rook {
		t.Fatalf("a directory with .rook/ did not read the rules: %v", v.Unread)
	}
	shared := byDoes(t, v, "messages")
	if got := strings.Join(shared.Opened[0].For, " "); got != "alice staff" {
		t.Errorf("helper is for %q", got)
	}
	if shared.Opened[1].For != nil {
		t.Errorf("the agent on a timer is for %v", shared.Opened[1].For)
	}
	if !slices.Equal(shared.Approvers, []string{"alice"}) || shared.Unnamed {
		t.Errorf("the approvers are %v, unnamed %v", shared.Approvers, shared.Unnamed)
	}
	unopened := byDoes(t, v, "something new")
	body := do(h, "GET", "/parts?part="+byDoes(t, v, "messages").ID, nil).Body.String()
	for _, want := range []string{"<th>For</th>", "Approved by", "<code>staff</code>"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not show %q:\n%s", want, body)
		}
	}
	if unopened.Unnamed || unopened.Approvers != nil {
		t.Errorf("a part nothing opens has no name to look up: %+v", unopened)
	}

	os.WriteFile(filepath.Join(dir, "rules.yaml"), []byte("agents: []\n"), 0o644)
	if v = partsView_(t, dir, ""); !byDoes(t, v, "messages").Unnamed || !strings.Contains(do(h, "GET", "/parts?part="+byDoes(t, v, "messages").ID, nil).Body.String(), "names no one") {
		t.Errorf("a part that needs approval with no one named is not said: %+v", byDoes(t, v, "messages"))
	}
	if byDoes(t, v, "other messages").Unnamed {
		t.Errorf("a part that needs no approval is said to need it")
	}

	os.WriteFile(filepath.Join(dir, "rules.yaml"), []byte("agents: 3\n"), 0o644)
	v = partsView_(t, dir, "")
	if v.Rook || len(v.Unread) != 1 || !strings.HasPrefix(v.Unread[0], "rules.yaml: ") {
		t.Errorf("a rules.yaml that cannot be read gave %+v", v.Unread)
	}
	if len(v.Parts) != 6 {
		t.Errorf("a rules.yaml that cannot be read lost the parts: %+v", v.Parts)
	}
	os.Remove(filepath.Join(dir, "rules.yaml"))
	if v = partsView_(t, dir, ""); len(v.Unread) != 1 {
		t.Errorf("a missing rules.yaml gave %+v", v.Unread)
	}
}

func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, text := range files {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const agentHead = "model: { name: claude-sonnet-5-5, secret: KEY }\nprompt: hello\n"

func Test_machines_parts_are_one_by_their_file(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".rook"), 0o755)
	write(t, dir, map[string]string{
		"a.yaml":              "agent: a\n" + agentHead + "open:\n  network: { include: ./parts/net/part.yaml }\n",
		"b.yaml":              "agent: b\n" + agentHead + "open:\n  net: { include: ./parts/net/part.yaml }\n",
		"say.yaml":            "flow: say\nstages:\n  a: { job: { script: ./r.js } }\n",
		"r.js":                "return 1",
		"rules.yaml":          "approve:\n  net: [alice]\n",
		"parts/net/part.yaml": "does: the network\nask: { machine: say.yaml, input: { part: net } }\ntools:\n  r: { does: r, job: { script: r.js, approve: true } }\n",
	})
	v := partsView_(t, dir, "")
	if len(v.Parts) != 1 {
		t.Fatalf("one file is listed as %d parts: %+v", len(v.Parts), v.Parts)
	}
	p := v.Parts[0]
	if len(p.Opened) != 2 || !slices.Equal(p.Approvers, []string{"alice"}) || p.Unnamed {
		t.Errorf("the part is %+v", p)
	}
}

func Test_machines_parts_file_and_approvers_by_default(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".rook"), 0o755)
	write(t, dir, map[string]string{
		"a.yaml":       "agent: a\n" + agentHead + "open:\n  p: { include: ./parts/x.yaml, does: changed here }\n",
		"say.yaml":     "flow: say\nstages:\n  a: { job: { script: ./r.js } }\n",
		"r.js":         "return 1",
		"rules.yaml":   "approve:\n  p: [alice]\n",
		"parts/x.yaml": "does: as written\nask: { machine: say.yaml, input: { part: \"${input.name}\" } }\ntools:\n  r: { does: r, job: { script: r.js, approve: true } }\n",
	})
	v := partsView_(t, dir, "")
	if len(v.Parts) != 1 || v.Parts[0].File != "parts/x.yaml" {
		t.Fatalf("a part whose agent changes its does: is %+v", v.Parts)
	}
	if !slices.Equal(v.Parts[0].Approvers, []string{"alice"}) {
		t.Errorf("an ask's part given by an expression gave %v", v.Parts[0].Approvers)
	}
}

func Test_machines_parts_in_place_keep_their_own_files(t *testing.T) {
	dir := t.TempDir()
	in := func(file string) string {
		return "        machine:\n          agent: in\n          model: { name: claude-sonnet-5-5, secret: KEY }\n          prompt: hello\n          open:\n            p: { include: " + file + " }\n"
	}
	write(t, dir, map[string]string{
		"two.yaml":             "flow: two\nstages:\n  a:\n    job:\n" + in("./parts/x.yaml") + "  b:\n    job:\n" + in("./parts/y.yaml"),
		"parts/x.yaml":         "does: x\ntools:\n  r: { does: r, job: { script: r.js } }\n",
		"parts/y.yaml":         "does: y\ntools:\n  r: { does: r, job: { script: r.js } }\n",
		"r.js":                 "return 1",
		"inline.yaml":          "agent: inline\n" + agentHead + "open:\n  notes: { does: other notes, tools: { r: { does: r, job: { script: ./r.js } } } }\n",
		"notes.yaml":            "agent: notes\n" + agentHead + "open:\n  notes: { does: the notes, tools: { r: { does: r, job: { script: ./r.js } } } }\n",
		"parts/notes/part.yaml": "does: the notes\ntools:\n  r: { does: r, job: ./r.js }\n",
		"parts/notes/r.js":      "return 1",
	})
	v := partsView_(t, dir, "")
	files := map[string]string{}
	for _, p := range v.Parts {
		files[p.Does+" "+p.ID] = p.File
	}
	if files["x p"] != "parts/x.yaml" || files["y p-2"] != "parts/y.yaml" {
		t.Errorf("the files are %v", files)
	}
	if !strings.Contains(fmt.Sprint(files), "parts/notes/part.yaml") {
		t.Errorf("a file nothing opens is hidden by a part with its does: %v", files)
	}
	if len(v.Parts) != 5 {
		t.Errorf("two parts of one name with different does: are one: %v", files)
	}
	for _, p := range v.Parts {
		if p.File == "parts/notes/part.yaml" && (len(p.Tools) != 1 || p.Tools[0].File != "parts/notes/r.js") {
			t.Errorf("a job written as a path gave %+v", p.Tools)
		}
	}
}

func Test_machines_parts_allow_leaves_out_data(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"a.yaml":             "agent: a\n" + agentHead + "open:\n  p: { does: p, tools: { r: { does: r, job: { machine: ./run.yaml, input: { allow: [x] } } } } }\n",
		"run.yaml":           "flow: run\nstages:\n  a: { job: { script: ./r.js, allow: [\"net:real.example:443\"], input: { allow: [\"fs.read:/etc/**\"] } } }\n",
		"r.js":               "return 1",
		"unopened/part.yaml": "does: unopened\ntools:\n  u: { does: u, job: { machine: run.yaml } }\n",
	})
	v := partsView_(t, dir, "")
	if len(v.Parts) != 2 {
		t.Fatalf("the parts are %+v %v %v", v.Parts, v.Problem, v.Unread)
	}
	want := []string{"net:real.example:443"}
	if got := v.Parts[0].Tools[0].Allow; !slices.Equal(got, want) {
		t.Errorf("allow is %v", got)
	}
	if got := v.Parts[1].Tools[0].Allow; !slices.Equal(got, want) {
		t.Errorf("allow of a part nothing opens is %v", got)
	}
}

func Test_machines_parts_keep_what_is_above_a_machine_with_problems(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".rook"), 0o755)
	outside := filepath.Join(t.TempDir(), "out.yaml")
	os.WriteFile(outside, []byte("flow: outside\n"), 0o644)
	write(t, dir, map[string]string{
		"bridge.yaml": "flow: bridge\nstages:\n  a: { job: { machine: ./broken.yaml } }\n",
		"broken.yaml": "flow: broken\nstages:\n  a: { job: { machine: ./helper.yaml } }\n  b: { job: { script: ./missing.js } }\n",
		"helper.yaml": "agent: helper\n" + agentHead + "open:\n  p: { does: p, tools: { r: { does: r, job: { script: ./r.js } } } }\n",
		"r.js":        "return 1",
		"junk.yaml":   "a: [\n",
		"rules.yaml":  "agents:\n  - { name: check, for: [staff] }\n",
		"timed.yaml":  "flow: timed\nstages:\n  a:\n    job:\n      machine:\n        flow: check\n        stages:\n          b:\n            job:\n              machine:\n                agent: in\n                model: { name: claude-sonnet-5-5, secret: KEY }\n                prompt: hello\n                open:\n                  q: { does: q, tools: { r: { does: r, job: { script: ./r.js } } } }\n",
	})
	if err := os.Symlink(outside, filepath.Join(dir, "link.yaml")); err != nil {
		t.Fatal(err)
	}
	v := partsView_(t, dir, "q")
	if !slices.Equal(v.Problem, []string{"bridge.yaml", "broken.yaml"}) {
		t.Errorf("the machines with problems are %v", v.Problem)
	}
	if len(v.Parts) != 2 {
		t.Fatalf("the parts are %+v", v.Parts)
	}
	if s := v.Parts[0].Opened[0].Starts; len(s) != 1 || s[0].File != "bridge.yaml" {
		t.Errorf("helper is started by %+v", s)
	}
	if got := v.Open.Opened[0].For; !slices.Equal(got, []string{"staff"}) {
		t.Errorf("a rule that names a machine written in place gave %v", got)
	}
}

func Test_machines_parts_of_an_agent_are_found_at_that_agent(t *testing.T) {
	dir := t.TempDir()
	in := func(name, file string) string {
		return "        machine:\n          agent: " + name + "\n          model: { name: claude-sonnet-5-5, secret: KEY }\n          prompt: hello\n          open:\n            p: { include: " + file + " }\n"
	}
	inner := "              machine:\n                agent: inner\n                model: { name: claude-sonnet-5-5, secret: KEY }\n                prompt: hello\n                open:\n                  w: { include: ./parts/w.yaml }\n"
	write(t, dir, map[string]string{
		"two.yaml":     "flow: two\nstages:\n  a:\n    job:\n" + in("in-a", "./parts/x.yaml") + "  b:\n    job:\n" + in("in-b", "./parts/y.yaml"),
		"outer.yaml":   "agent: outer\n" + agentHead + "open:\n  w:\n    does: written here\n    tools:\n      r:\n        does: r\n        job:\n" + inner,
		"parts/x.yaml": "does: same\ntools:\n  r: { does: r, job: { script: r.js } }\n",
		"parts/y.yaml": "does: same\ntools:\n  r: { does: r, job: { script: r.js } }\n",
		"parts/w.yaml": "does: from a file\ntools:\n  q: { does: q, job: { script: r.js } }\n",
		"r.js":         "return 1",
	})
	v := partsView_(t, dir, "")
	if p := byDoes(t, v, "written here"); p.File != "" || len(p.Tools) != 1 || p.Tools[0].Name != "r" {
		t.Errorf("a part written in place is %+v", p)
	}
	if p := byDoes(t, v, "from a file"); p.File != "parts/w.yaml" || p.Tools[0].Name != "q" {
		t.Errorf("a part in a file is %+v", p)
	}
	for _, p := range v.Parts {
		if p.Does == "same" && len(p.Opened) != 1 {
			t.Errorf("two files with one does: are one part: %+v", p)
		}
	}
	if len(v.Parts) != 4 {
		t.Errorf("the parts are %+v", v.Parts)
	}

	head := "        machine:\n          agent: same\n          model: { name: claude-sonnet-5-5, secret: KEY }\n          prompt: hello\n          open:\n"
	write(t, dir, map[string]string{
		"same.yaml": "flow: same\nstages:\n  a:\n    job:\n" + head + "            p: { does: in place, tools: { r: { does: r, job: { script: ./r.js } } } }\n" +
			"  b:\n    job:\n" + head + "            p: { include: ./parts/z.yaml }\n",
		"parts/z.yaml": "does: in a file\ntools:\n  r: { does: r, job: { script: r.js } }\n",
	})
	v = partsView_(t, dir, "")
	if p := byDoes(t, v, "in place"); p.File != "" {
		t.Errorf("a part written in place has the file %q", p.File)
	}
	if p := byDoes(t, v, "in a file"); p.File != "parts/z.yaml" {
		t.Errorf("a part in a file has the file %q", p.File)
	}
}

func Test_machines_parts_add_what_each_opener_writes(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".rook"), 0o755)
	write(t, dir, map[string]string{
		"a.yaml":       "agent: a\n" + agentHead + "open:\n  p: { include: ./parts/x.yaml }\n",
		"b.yaml":       "agent: b\n" + agentHead + "open:\n  p: { include: ./parts/x.yaml, approve: true }\n",
		"c.yaml":       "agent: c\n" + agentHead + "open:\n  p: { include: ./parts/x.yaml, ask: { machine: ./say.yaml, input: { part: other } } }\n",
		"say.yaml":     "flow: say\nstages:\n  a: { job: { script: ./r.js } }\n",
		"r.js":         "return 1",
		"rules.yaml":   "approve:\n  x: [alice]\n  other: [sam, alice]\n",
		"parts/x.yaml": "does: x\nask: { machine: say.yaml, input: { part: x } }\ntools:\n  r: { does: r, job: { script: r.js, approve: true } }\n",
	})
	v := partsView_(t, dir, "")
	if len(v.Parts) != 1 {
		t.Fatalf("the parts are %+v", v.Parts)
	}
	if p := v.Parts[0]; !p.Opening || !slices.Equal(p.Approvers, []string{"alice", "sam"}) {
		t.Errorf("the part is %+v", p)
	}
}

func Test_machines_parts_say_what_is_not_known_of_machines_with_problems(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"a.yaml":       "flow: a\nstages:\n  a: { job: { machine: ./b.yaml } }\n",
		"b.yaml":       "flow: b\nstages:\n  a: { job: { machine: ./a.yaml } }\n  b: { job: { machine: ./helper.yaml } }\n",
		"helper.yaml":  "agent: helper\n" + agentHead + "open:\n  p: { does: p, tools: { r: { does: r, job: { script: ./r.js } } } }\n",
		"timed.yaml":   "statechart: timed\ninitial: w\nstates:\n  w:\n    timers: { b: tue 07:30 UTC, a: mon 07:30 UTC }\n    on: { a: x, b: x }\n    job: { machine: ./helper2.yaml }\n  x: { end: done, colour: blue }\n",
		"helper2.yaml": "agent: helper2\n" + agentHead + "open:\n  q: { does: q, tools: { r: { does: r, job: { script: ./r.js } } } }\n",
		"r.js":         "return 1",
	})
	v := partsView_(t, dir, "")
	p, q := byDoes(t, v, "p"), byDoes(t, v, "q")
	if o := p.Opened[0]; len(o.Starts) != 0 || !o.Unclear {
		t.Errorf("machines with problems that name each other gave %+v", o)
	}
	if s := q.Opened[0].Starts; len(s) != 1 || s[0].Timers != "mon 07:30 UTC, tue 07:30 UTC" {
		t.Errorf("a machine with problems at the top is %+v", s)
	}
	if o := q.Opened[0]; o.Unclear {
		t.Errorf("a start was found, and the page says none: %+v", o)
	}
}

func Test_machines_parts_field_that_does_not_fit_takes_no_other_with_it(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"top.yaml":    "statechart: top\ninitial: a\nstates:\n  a: { timers: { x: mon 07:30 UTC, y: { at: 3 } }, job: { machine: ./helper.yaml } }\n  b: 3\n",
		"helper.yaml": "agent: helper\n" + agentHead + "open:\n  p: { does: p, tools: { r: { does: r, job: { script: ./r.js } } } }\n",
		"r.js":        "return 1",
	})
	v := partsView_(t, dir, "")
	if s := byDoes(t, v, "p").Opened[0].Starts; len(s) != 1 || s[0].Timers != "mon 07:30 UTC" {
		t.Errorf("the start is %+v", s)
	}
}

func Test_machines_parts_agent_named_as_yaml_reads_a_value(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"a.yaml":       "agent: true\n" + agentHead + "open:\n  p: { include: ./parts/x.yaml }\n",
		"parts/x.yaml": "does: x\ntools:\n  r: { does: r, job: { script: r.js } }\n",
		"r.js":         "return 1",
	})
	v := partsView_(t, dir, "")
	if len(v.Parts) != 1 || v.Parts[0].File != "parts/x.yaml" {
		t.Errorf("the parts are %+v %v", v.Parts, v.Problem)
	}
}

func Test_machines_parts_page_follows_the_directory(t *testing.T) {
	dir := project(t)
	h, err := New(Config{Dir: dir, Interval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/parts/stream?part=messages", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	for range 2 {
		if _, _, err := conn.Read(ctx); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dir, "parts/part.yaml"), []byte("does: changed on disk\ntools: {}\n"), 0o644)
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "changed on disk") {
			return
		}
	}
}
