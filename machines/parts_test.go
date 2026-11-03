package machines

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	return d.index().partsView(open)
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
	if v.Broken != 2 {
		t.Errorf("%d machines have problems, and not bad.yaml and odd.yaml", v.Broken)
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
	if got := strings.Join(openedBy(*shared), " "); got != "helper looker" || shared.File != "parts/part.yaml" {
		t.Errorf("the shared part in %q is opened by %q", shared.File, got)
	}
	helper, looker := shared.Opened[0], shared.Opened[1]
	if got := strings.Join(startsOf(helper), " "); got != "bridge" || helper.Starts[0].File != "telegram.yaml" {
		t.Errorf("helper is started by %q: %+v", got, helper.Starts)
	}
	if looker.File != "timed.yaml" || len(looker.Starts) != 1 || looker.Starts[0].File != "timed.yaml" {
		t.Errorf("an agent written in place is not started by the machine it is written in: %+v", looker)
	}
	if len(shared.Tools) != 1 || !shared.Tools[0].Held || shared.Tools[0].Job != "machine" || shared.Tools[0].File != ".kit/say.yaml" {
		t.Errorf("the tool is %+v", shared.Tools)
	}

	if len(own.Opened) != 1 || own.File != "" || len(own.Opened[0].Starts) != 0 {
		t.Errorf("an agent in its own file that nothing names is %+v", own)
	}
	if tool := own.Tools[0]; tool.Job != "script" || tool.File != "own.js" {
		t.Errorf("the tool is %+v", tool)
	}
	if s := zed.Opened[0].Starts; len(s) != 0 {
		t.Errorf("a machine in a dotted directory is listed as a start: %+v", s)
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
	if body := do(h, "GET", "/parts?part=zed", nil).Body.String(); !strings.Contains(body, ">by hand<") {
		t.Errorf("an agent no machine starts does not say so:\n%s", body)
	}
	body := do(h, "GET", "/parts?part=new", nil).Body.String()
	for _, want := range []string{`<span class="chip">not opened</span>`, "No agent opens it.", "needs approval", "2 machines with problems", `href="/machines?file=parts%2fnew%2fpart.yaml&amp;view=text"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not show %q:\n%s", want, body)
		}
	}
	for _, gone := range []string{`class="bad"`, "allow", "For<", "Approved by", "timer"} {
		if strings.Contains(body, gone) {
			t.Errorf("the page shows %q:\n%s", gone, body)
		}
	}
	if w := do(h, "GET", "/parts?part=gone", nil); w.Code != http.StatusNotFound {
		t.Errorf("a part that is not there answered %d", w.Code)
	}
	if w := do(h, "GET", "/parts?part=../../etc/passwd", nil); w.Code != http.StatusNotFound {
		t.Errorf("a path answered %d", w.Code)
	}
}

func Test_machines_parts_show_the_problems_in_their_file(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"a.yaml":              "agent: a\n" + agentHead + "open:\n  net: { include: ./parts/net/part.yaml }\n",
		"b.yaml":              "flow: b\nstages:\n  x: { job: { machine: ./a.yaml } }\n",
		"r.js":                "return 1",
		"parts/net/part.yaml": "does: the network\ntools:\n  r:\n    description: r\n    job: { script: r.js }\n",
	})
	v := partsView_(t, dir, "net")
	if v.Open == nil || len(v.Open.Opened) != 0 || v.Broken != 2 {
		t.Fatalf("the part is %+v, with %d machines with problems", v.Open, v.Broken)
	}
	if len(v.Problems) == 0 || v.Problems[0].File != "parts/net/part.yaml" || len(v.Problems[0].Breaks) != 2 || v.Problems[0].Line == 0 {
		t.Errorf("the problems in the part's file are %+v", v.Problems)
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
	write(t, dir, map[string]string{
		"a.yaml":              "agent: a\n" + agentHead + "open:\n  network: { include: ./parts/net/part.yaml }\n",
		"b.yaml":              "agent: b\n" + agentHead + "open:\n  net: { include: ./parts/net/part.yaml }\n",
		"say.yaml":            "flow: say\nstages:\n  a: { job: { script: ./r.js } }\n",
		"r.js":                "return 1",
		"parts/net/part.yaml": "does: the network\nask: { machine: say.yaml, input: { part: net } }\ntools:\n  r: { does: r, job: { script: r.js, approve: true } }\n",
	})
	v := partsView_(t, dir, "")
	if len(v.Parts) != 1 {
		t.Fatalf("one file is listed as %d parts: %+v", len(v.Parts), v.Parts)
	}
	p := v.Parts[0]
	if len(p.Opened) != 2 {
		t.Errorf("the part is %+v", p)
	}
}

func Test_machines_parts_file_by_default(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"a.yaml":       "agent: a\n" + agentHead + "open:\n  p: { include: ./parts/x.yaml, does: changed here }\n",
		"say.yaml":     "flow: say\nstages:\n  a: { job: { script: ./r.js } }\n",
		"r.js":         "return 1",
		"parts/x.yaml": "does: as written\nask: { machine: say.yaml, input: { part: \"${input.name}\" } }\ntools:\n  r: { does: r, job: { script: r.js, approve: true } }\n",
	})
	v := partsView_(t, dir, "")
	if len(v.Parts) != 1 || v.Parts[0].File != "parts/x.yaml" {
		t.Fatalf("a part whose agent changes its does: is %+v", v.Parts)
	}
}

func Test_machines_parts_in_place_keep_their_own_files(t *testing.T) {
	dir := t.TempDir()
	in := func(file string) string {
		return "        machine:\n          agent: in\n          model: { name: claude-sonnet-5-5, secret: KEY }\n          prompt: hello\n          open:\n            p: { include: " + file + " }\n"
	}
	write(t, dir, map[string]string{
		"two.yaml":              "flow: two\nstages:\n  a:\n    job:\n" + in("./parts/x.yaml") + "  b:\n    job:\n" + in("./parts/y.yaml"),
		"parts/x.yaml":          "does: x\ntools:\n  r: { does: r, job: { script: r.js } }\n",
		"parts/y.yaml":          "does: y\ntools:\n  r: { does: r, job: { script: r.js } }\n",
		"r.js":                  "return 1",
		"inline.yaml":           "agent: inline\n" + agentHead + "open:\n  notes: { does: other notes, tools: { r: { does: r, job: { script: ./r.js } } } }\n",
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
	write(t, dir, map[string]string{
		"a.yaml":       "agent: a\n" + agentHead + "open:\n  p: { include: ./parts/x.yaml }\n",
		"b.yaml":       "agent: b\n" + agentHead + "open:\n  p: { include: ./parts/x.yaml, approve: true }\n",
		"c.yaml":       "agent: c\n" + agentHead + "open:\n  p: { include: ./parts/x.yaml, ask: { machine: ./say.yaml, input: { part: other } } }\n",
		"say.yaml":     "flow: say\nstages:\n  a: { job: { script: ./r.js } }\n",
		"r.js":         "return 1",
		"parts/x.yaml": "does: x\nask: { machine: say.yaml, input: { part: x } }\ntools:\n  r: { does: r, job: { script: r.js, approve: true } }\n",
	})
	v := partsView_(t, dir, "")
	if len(v.Parts) != 1 {
		t.Fatalf("the parts are %+v", v.Parts)
	}
	if p := v.Parts[0]; !p.Opening || len(p.Opened) != 3 {
		t.Errorf("the part is %+v", p)
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
		t.Errorf("the parts are %+v", v.Parts)
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
