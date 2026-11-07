package machines

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// toolkitsProject adds to a project another agent that offers the toolkit, written in
// place, on a timer; a toolkit nothing offers; a machine with problems; a machine
// in a dotted directory that nothing names; and another toolkit of the same name.
func toolkitsProject(t *testing.T) string {
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
                toolkits:
                  messages: { include: ./toolkits/toolkit.yaml }
          again:
            job:
              machine:
                agent: looker
                model: { name: claude-sonnet-5-5, secret: KEY }
                prompt: look again
                toolkits:
                  messages: { include: ./toolkits/toolkit.yaml }
    on: { done: waiting, failed: waiting }
`,
		"own.yaml": `agent: own
model: { name: claude-sonnet-5-5, secret: KEY }
prompt: hello
toolkits:
  messages:
    description: other messages
    tools:
      read: { description: reads, job: { script: ./own.js, allow: ["net:example.com:443"] } }
`,
		"own.js":                     "return 1",
		".kit/dead.yaml":             "flow: dead\nstages:\n  a: { job: { machine: behind.yaml } }\n",
		"behind.yaml":                "agent: behind\nmodel: { name: claude-sonnet-5-5, secret: KEY }\nprompt: hello\ntoolkits:\n  zed: { description: zed, tools: { read: { description: reads, job: { script: ./own.js } } } }\n",
		"toolkits/new.yaml":          "description: something new\ntools:\n  try: { description: tries, job: { script: ./try.js, approve: true } }\n",
		"toolkits/try.js":            "return 1",
		".kit/toolkit.yaml":          "description: kept in a dotted directory\ntools: {}\n",
		"side.yaml":                  "flow: side\nstages:\n  a: { job: { machine: ./side.yml } }\n",
		"side.yml":                   "agent: sider\nmodel: { name: claude-sonnet-5-5, secret: KEY }\nprompt: hello\ntoolkits:\n  yml: { description: from a yml, tools: { read: { description: reads, job: { script: ./own.js } } } }\n",
		"based.yaml":                 "agent: based\ninclude: ./shared.yaml\nmodel: { name: claude-sonnet-5-5, secret: KEY }\nprompt: hello\n",
		"shared.yaml":                "toolkits:\n  kept: { include: ./toolkits/kept/toolkit.yaml }\n",
		"toolkits/kept/toolkit.yaml": "description: kept in a shared file\ntools:\n  read: { description: reads, job: { script: own.js } }\n",
		"odd.yaml":                   "agent: odd\ndoes: a machine with problems\ntools: {}\n",
		"bad.yaml":                   "flow: bad\nstages: {}\ncolour: blue\n",
		"data.yaml":                  "description: not a toolkit\n",
		".corbieignore":              "toolkits/\npeople.yaml\nshared/\nshared.yaml\ndata.yaml\n.kit/toolkit.yaml\n",
	} {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
		os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644)
	}
	return dir
}

func toolkitsView_(t *testing.T, dir, open string) toolkitsView {
	t.Helper()
	d, err := newDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return d.index("").toolkitsView(open)
}

func byDescription(t *testing.T, v toolkitsView, description string) toolkit {
	t.Helper()
	for _, p := range v.Toolkits {
		if p.Description == description {
			return p
		}
	}
	t.Fatalf("no toolkit has the description %q: %+v", description, v.Toolkits)
	return toolkit{}
}

func offeredBy(p toolkit) []string {
	var names []string
	for _, o := range p.Offered {
		names = append(names, o.Name)
	}
	return names
}

func startsOf(o offer) []string {
	var names []string
	for _, s := range o.Starts {
		names = append(names, s.Name)
	}
	return names
}

func Test_machines_toolkits_show_what_offers_each(t *testing.T) {
	dir := toolkitsProject(t)
	v := toolkitsView_(t, dir, "")
	var ids []string
	for _, p := range v.Toolkits {
		ids = append(ids, p.ID)
	}
	if got := strings.Join(ids, " "); got != "kept messages messages-2 new toolkit yml zed" {
		t.Fatalf("the toolkits are %q: %+v", got, v.Unread)
	}
	if v.Open == nil || v.Open.ID != "kept" {
		t.Errorf("the first toolkit is not open: %+v", v.Open)
	}

	var kept, shared, own, unoffered, zed, yml *toolkit
	for i := range v.Toolkits {
		switch v.Toolkits[i].ID {
		case "kept":
			kept = &v.Toolkits[i]
		case "new":
			unoffered = &v.Toolkits[i]
		case "zed":
			zed = &v.Toolkits[i]
		case "yml":
			yml = &v.Toolkits[i]
		}
		if v.Toolkits[i].Description == "messages" {
			shared = &v.Toolkits[i]
		}
		if v.Toolkits[i].Description == "other messages" {
			own = &v.Toolkits[i]
		}
	}
	if shared == nil || own == nil {
		t.Fatalf("a toolkit is missing: %+v", v.Toolkits)
	}
	if kept.File != "toolkits/kept/toolkit.yaml" {
		t.Errorf("a toolkit under a `toolkits:` in an included file has the file %q", kept.File)
	}
	if got := strings.Join(offeredBy(*shared), " "); got != "helper looker" || shared.File != "toolkits/toolkit.yaml" {
		t.Errorf("the shared toolkit in %q is offered by %q", shared.File, got)
	}
	helper, looker := shared.Offered[0], shared.Offered[1]
	if got := strings.Join(startsOf(helper), " "); got != "chat" || helper.Starts[0].File != ".kit/chat.yaml" || helper.Lifetime != "transient" {
		t.Errorf("helper is started by %q: %+v", got, helper)
	}
	if looker.File != "timed.yaml" || len(looker.Starts) != 1 || looker.Starts[0].File != "timed.yaml" || looker.Lifetime != "" {
		t.Errorf("an agent written in place is not started by the machine it is written in: %+v", looker)
	}
	if len(shared.Tools) != 1 || !shared.Tools[0].Approve || shared.Tools[0].Job != "machine" || shared.Tools[0].File != ".kit/say.yaml" {
		t.Errorf("the tool is %+v", shared.Tools)
	}

	if len(own.Offered) != 1 || own.File != "" || len(own.Offered[0].Starts) != 0 || own.Offered[0].Lifetime != "transient" {
		t.Errorf("an agent in its own file that nothing names is %+v", own)
	}
	if tool := own.Tools[0]; tool.Job != "script" || tool.File != "own.js" {
		t.Errorf("the tool is %+v", tool)
	}
	if s := zed.Offered[0].Starts; len(s) != 1 || s[0].File != ".kit/dead.yaml" {
		t.Errorf("a machine in a dotted directory is not listed as a start: %+v", s)
	}
	if s := yml.Offered[0].Starts; len(s) != 1 || s[0].File != "side.yaml" {
		t.Errorf("the machine above a .yml file is %+v", s)
	}
	if len(unoffered.Offered) != 0 || unoffered.File != "toolkits/new.yaml" || !unoffered.Tools[0].Approve || unoffered.Tools[0].File != "toolkits/try.js" {
		t.Errorf("the toolkit nothing offers is %+v", unoffered)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if linked := toolkitsView_(t, link, ""); len(linked.Toolkits) != len(v.Toolkits) {
		t.Errorf("through a link to the directory the toolkits are %+v", linked.Toolkits)
	}

	h, err := New(Config{Dir: dir, Interval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if body := do(h, "GET", "/toolkits?toolkit=zed", nil).Body.String(); !strings.Contains(body, ">dead</a>") || !strings.Contains(body, `<span class="dim">transient: a call, or corbie run submit /`+filepath.Base(dir)+`/behind, starts each run</span>`) {
		t.Errorf("an agent does not say what starts it:\n%s", body)
	}
	body := do(h, "GET", "/toolkits?toolkit=new", nil).Body.String()
	for _, want := range []string{`<small>1 tool · something new</small>`, `<span class="chip">not offered</span>`, `<p class="dim">not offered</p>`, "needs approval", `<div class="problems"><h2>`, `href="/machines?file=bad.yaml&amp;view=text"`, `<a href="/toolkits?toolkit=new&amp;view=text">Text</a>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not show %q:\n%s", want, body)
		}
	}
	for _, gone := range []string{`class="bad"`, "allow", "For<", "Approved by", "timer"} {
		if strings.Contains(body, gone) {
			t.Errorf("the page shows %q:\n%s", gone, body)
		}
	}
	text := do(h, "GET", "/toolkits?toolkit=new&view=text", nil).Body.String()
	if !strings.Contains(text, `<pre class="code" aria-label="toolkits/new.yaml"><span class="l" id="L1"><span class="k">description</span>`) || strings.Contains(text, `class="toolcards"`) {
		t.Errorf("the toolkit's Text tab does not show its file:\n%s", text)
	}
	if w := do(h, "GET", "/toolkits?toolkit=gone", nil); w.Code != http.StatusNotFound {
		t.Errorf("a toolkit that is not there answered %d", w.Code)
	}
	if w := do(h, "GET", "/toolkits?toolkit=../../etc/passwd", nil); w.Code != http.StatusNotFound {
		t.Errorf("a path answered %d", w.Code)
	}
}

func Test_machines_toolkits_show_the_problems_in_their_file(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		".corbieignore":     "toolkits/\n",
		"a.yaml":            "agent: a\n" + agentHead + "toolkits:\n  net: { include: ./toolkits/net.yaml }\n",
		"b.yaml":            "flow: b\nstages:\n  x: { job: { machine: ./a.yaml } }\n",
		"r.js":              "return 1",
		"toolkits/net.yaml": "description: the network\ntools:\n  r:\n    does: r\n    job: { script: r.js }\n",
	})
	v := toolkitsView_(t, dir, "net")
	// The agent that offers the toolkit has problems, so it offers nothing,
	// and the toolkit's file is marked as having problems, not as unoffered.
	if v.Open == nil || len(v.Open.Offered) != 0 || !v.Open.Broken || v.Open.File != "toolkits/net.yaml" {
		t.Fatalf("the toolkit is %+v", v.Open)
	}
	if len(v.Found) == 0 || v.Found[0].File != "toolkits/net.yaml" || len(v.Found[0].Breaks) != 2 || v.Found[0].Line == 0 {
		t.Errorf("the problems in the toolkit's file are %+v", v.Found)
	}
	if !slices.EqualFunc(v.Problems, v.Found, func(a, b problem) bool { return a.File == b.File && a.Line == b.Line && a.Text == b.Text }) {
		t.Errorf("the Toolkits list gives the problems %+v, and not the directory's", v.Problems)
	}
	h, err := New(Config{Dir: dir, Interval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if body := do(h, "GET", "/toolkits?toolkit=net", nil).Body.String(); !strings.Contains(body, `<span class="chip">has problems</span>`) || !strings.Contains(body, "offered only by machines with problems") || strings.Contains(body, "not offered") {
		t.Errorf("a toolkit offered only by machines with problems is shown as\n%s", body)
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

func Test_machines_toolkits_are_one_by_their_file(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		".corbieignore":             "toolkits/\n",
		"a.yaml":                    "agent: a\n" + agentHead + "toolkits:\n  network: { include: ./toolkits/net/toolkit.yaml }\n",
		"b.yaml":                    "agent: b\n" + agentHead + "toolkits:\n  net: { include: ./toolkits/net/toolkit.yaml }\n",
		"say.yaml":                  "flow: say\nstages:\n  a: { job: { script: ./r.js } }\n",
		"r.js":                      "return 1",
		"toolkits/net/toolkit.yaml": "description: the network\nask: { machine: say.yaml, input: { toolkit: net } }\ntools:\n  r: { description: r, job: { script: r.js, approve: true } }\n",
	})
	v := toolkitsView_(t, dir, "")
	if len(v.Toolkits) != 1 {
		t.Fatalf("one file is listed as %d toolkits: %+v", len(v.Toolkits), v.Toolkits)
	}
	p := v.Toolkits[0]
	if len(p.Offered) != 2 {
		t.Errorf("the toolkit is %+v", p)
	}
}

func Test_machines_toolkits_file_by_default(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		".corbieignore":   "toolkits/\n",
		"a.yaml":          "agent: a\n" + agentHead + "toolkits:\n  p: { include: ./toolkits/x.yaml, description: changed here }\n",
		"say.yaml":        "flow: say\nstages:\n  a: { job: { script: ./r.js } }\n",
		"r.js":            "return 1",
		"toolkits/x.yaml": "description: as written\nask: { machine: say.yaml, input: { toolkit: \"${input.name}\" } }\ntools:\n  r: { description: r, job: { script: r.js, approve: true } }\n",
	})
	v := toolkitsView_(t, dir, "")
	if len(v.Toolkits) != 1 || v.Toolkits[0].File != "toolkits/x.yaml" {
		t.Fatalf("a toolkit whose agent changes its description: is %+v", v.Toolkits)
	}
}

func Test_machines_toolkits_in_place_keep_their_own_files(t *testing.T) {
	dir := t.TempDir()
	in := func(file string) string {
		return "        machine:\n          agent: in\n          model: { name: claude-sonnet-5-5, secret: KEY }\n          prompt: hello\n          toolkits:\n            p: { include: " + file + " }\n"
	}
	write(t, dir, map[string]string{
		".corbieignore":               "toolkits/\n",
		"two.yaml":                    "flow: two\nstages:\n  a:\n    job:\n" + in("./toolkits/x.yaml") + "  b:\n    job:\n" + in("./toolkits/y.yaml"),
		"toolkits/x.yaml":             "description: x\ntools:\n  r: { description: r, job: { script: r.js } }\n",
		"toolkits/y.yaml":             "description: y\ntools:\n  r: { description: r, job: { script: r.js } }\n",
		"r.js":                        "return 1",
		"inline.yaml":                 "agent: inline\n" + agentHead + "toolkits:\n  notes: { description: other notes, tools: { r: { description: r, job: { script: ./r.js } } } }\n",
		"notes.yaml":                  "agent: notes\n" + agentHead + "toolkits:\n  notes: { description: the notes, tools: { r: { description: r, job: { script: ./r.js } } } }\n",
		"toolkits/notes/toolkit.yaml": "description: the notes\ntools:\n  r: { description: r, job: ./r.js }\n",
		"toolkits/notes/r.js":         "return 1",
	})
	v := toolkitsView_(t, dir, "")
	files := map[string]string{}
	for _, p := range v.Toolkits {
		files[p.Description+" "+p.ID] = p.File
	}
	if files["x p"] != "toolkits/x.yaml" || files["y p-2"] != "toolkits/y.yaml" {
		t.Errorf("the files are %v", files)
	}
	if !strings.Contains(fmt.Sprint(files), "toolkits/notes/toolkit.yaml") {
		t.Errorf("a file nothing offers is hidden by a toolkit with its description: %v", files)
	}
	if len(v.Toolkits) != 5 {
		t.Errorf("two toolkits of one name with different description: are one: %v", files)
	}
	for _, p := range v.Toolkits {
		if p.File == "toolkits/notes/toolkit.yaml" && (len(p.Tools) != 1 || p.Tools[0].File != "toolkits/notes/r.js") {
			t.Errorf("a job written as a path gave %+v", p.Tools)
		}
	}
}

func Test_machines_toolkits_of_an_agent_are_found_at_that_agent(t *testing.T) {
	dir := t.TempDir()
	in := func(name, file string) string {
		return "        machine:\n          agent: " + name + "\n          model: { name: claude-sonnet-5-5, secret: KEY }\n          prompt: hello\n          toolkits:\n            p: { include: " + file + " }\n"
	}
	inner := "              machine:\n                agent: inner\n                model: { name: claude-sonnet-5-5, secret: KEY }\n                prompt: hello\n                toolkits:\n                  w: { include: ./toolkits/w.yaml }\n"
	write(t, dir, map[string]string{
		".corbieignore":   "toolkits/\n",
		"two.yaml":        "flow: two\nstages:\n  a:\n    job:\n" + in("in-a", "./toolkits/x.yaml") + "  b:\n    job:\n" + in("in-b", "./toolkits/y.yaml"),
		"outer.yaml":      "agent: outer\n" + agentHead + "toolkits:\n  w:\n    description: written here\n    tools:\n      r:\n        description: r\n        job:\n" + inner,
		"toolkits/x.yaml": "description: same\ntools:\n  r: { description: r, job: { script: r.js } }\n",
		"toolkits/y.yaml": "description: same\ntools:\n  r: { description: r, job: { script: r.js } }\n",
		"toolkits/w.yaml": "description: from a file\ntools:\n  q: { description: q, job: { script: r.js } }\n",
		"r.js":            "return 1",
	})
	v := toolkitsView_(t, dir, "")
	if p := byDescription(t, v, "written here"); p.File != "" || len(p.Tools) != 1 || p.Tools[0].Name != "r" {
		t.Errorf("a toolkit written in place is %+v", p)
	}
	if p := byDescription(t, v, "from a file"); p.File != "toolkits/w.yaml" || p.Tools[0].Name != "q" {
		t.Errorf("a toolkit in a file is %+v", p)
	}
	for _, p := range v.Toolkits {
		if p.Description == "same" && len(p.Offered) != 1 {
			t.Errorf("two files with one description: are one toolkit: %+v", p)
		}
	}
	if len(v.Toolkits) != 4 {
		t.Errorf("the toolkits are %+v", v.Toolkits)
	}

	head := "        machine:\n          agent: same\n          model: { name: claude-sonnet-5-5, secret: KEY }\n          prompt: hello\n          toolkits:\n"
	write(t, dir, map[string]string{
		".corbieignore": "toolkits/\n",
		"same.yaml": "flow: same\nstages:\n  a:\n    job:\n" + head + "            p: { description: in place, tools: { r: { description: r, job: { script: ./r.js } } } }\n" +
			"  b:\n    job:\n" + head + "            p: { include: ./toolkits/z.yaml }\n",
		"toolkits/z.yaml": "description: in a file\ntools:\n  r: { description: r, job: { script: r.js } }\n",
	})
	v = toolkitsView_(t, dir, "")
	if p := byDescription(t, v, "in place"); p.File != "" {
		t.Errorf("a toolkit written in place has the file %q", p.File)
	}
	if p := byDescription(t, v, "in a file"); p.File != "toolkits/z.yaml" {
		t.Errorf("a toolkit in a file has the file %q", p.File)
	}
}

func Test_machines_toolkits_add_what_each_agent_writes(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		".corbieignore":   "toolkits/\n",
		"a.yaml":          "agent: a\n" + agentHead + "toolkits:\n  p: { include: ./toolkits/x.yaml }\n",
		"b.yaml":          "agent: b\n" + agentHead + "toolkits:\n  p: { include: ./toolkits/x.yaml, approve: true }\n",
		"c.yaml":          "agent: c\n" + agentHead + "toolkits:\n  p: { include: ./toolkits/x.yaml, ask: { machine: ./say.yaml, input: { toolkit: other } } }\n",
		"say.yaml":        "flow: say\nstages:\n  a: { job: { script: ./r.js } }\n",
		"r.js":            "return 1",
		"toolkits/x.yaml": "description: x\nask: { machine: say.yaml, input: { toolkit: x } }\ntools:\n  r: { description: r, job: { script: r.js, approve: true } }\n",
	})
	v := toolkitsView_(t, dir, "")
	if len(v.Toolkits) != 1 {
		t.Fatalf("the toolkits are %+v", v.Toolkits)
	}
	if p := v.Toolkits[0]; !p.Opening || len(p.Offered) != 3 {
		t.Errorf("the toolkit is %+v", p)
	}
}

func Test_machines_toolkits_agent_named_as_yaml_reads_a_value(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		".corbieignore":   "toolkits/\n",
		"a.yaml":          "agent: true\n" + agentHead + "toolkits:\n  p: { include: ./toolkits/x.yaml }\n",
		"toolkits/x.yaml": "description: x\ntools:\n  r: { description: r, job: { script: r.js } }\n",
		"r.js":            "return 1",
	})
	v := toolkitsView_(t, dir, "")
	if len(v.Toolkits) != 1 || v.Toolkits[0].File != "toolkits/x.yaml" {
		t.Errorf("the toolkits are %+v", v.Toolkits)
	}
}

func Test_machines_toolkits_page_follows_the_directory(t *testing.T) {
	dir := project(t)
	h, err := New(Config{Dir: dir, Interval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/toolkits/stream?toolkit=messages", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	for range 2 {
		if _, _, err := conn.Read(ctx); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dir, "toolkits/toolkit.yaml"), []byte("description: changed on disk\ntools: {}\n"), 0o644)
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

func toolNamed(t *testing.T, p *toolkit, name string) toolkitTool {
	t.Helper()
	for _, tool := range p.Tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("the toolkit %s has no tool %s: %+v", p.Name, name, p.Tools)
	return toolkitTool{}
}

// callsProject writes a project whose agent is called by its location, and
// whose toolkits call machines by theirs.
func callsProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, map[string]string{
		".corbieignore":    "toolkits/\n",
		"people/find.yaml": "flow: find\ndescription: Find a person by email.\ninput: { table: string, email: string }\nstages:\n  a: { job: { script: ./find.js } }\n",
		"people/find.js":   "return 1",
		"agents/support.yaml": "agent: support\n" + agentHead + "toolkits:\n" +
			"  orders: { include: toolkits/orders.yaml, when: \"${input.asker.scopes.includes('orders:read')}\" }\n" +
			"tools:\n  who: { call: people/find, input: { table: staff } }\n",
		"chat/post.yaml": "flow: post\ndescription: Post a message.\nstages:\n  a: { job: { script: ./post.js } }\n",
		"chat/post.js":   "return 1",
		"toolkits/orders.yaml": "description: The orders app.\ninstructions: { include: ./orders.md }\ntools:\n" +
			"  find: { call: people/find, input: { table: customers }, approve: true, when: \"${input.asker.name != ''}\" }\n" +
			"  tell: { description: Tell the room., job: { call: { at: chat/post }, input: { room: orders }, approve: true } }\n" +
			"  ping: { description: Ping the room., job: { send: { to: { at: chat/post }, event: ping } } }\n",
		"toolkits/orders.md": "Use these for orders.\nAsk first.\n",
		"toolkits/infra.yaml": "description: The machines.\ninstructions: |\n  Restart only when asked.\n  Say so.\ntools:\n" +
			"  restart: { call: people/find, input: { table: vms }, when: \"${input.x}\" }\n" +
			"  tell: { description: Tell the room., job: { call: { at: chat/post }, input: { room: infra }, approve: true } }\n" +
			"  ping: { description: Ping the room., job: { send: { to: { at: chat/post, key: infra }, event: ping } } }\n",
		"conversation.yaml": "statechart: conversation\ninitial: talking\nstates:\n  talking:\n    job: { call: { at: agents/support }, input: { text: hi } }\n    on: { done: over, failed: over }\n  over: { end: done }\n",
		"caller.yaml":       "flow: caller\nstages:\n  a: { job: { call: { at: /" + filepath.Base(dir) + "/agents/support } } }\n",
		"other.yaml":        "flow: other\nstages:\n  a: { job: { call: { at: /elsewhere/agents/support } } }\n",
	})
	return dir
}

func Test_machines_toolkits_call_machines_by_their_location(t *testing.T) {
	dir := callsProject(t)
	v := toolkitsView_(t, dir, "orders")
	if len(v.Problems) != 0 {
		t.Fatalf("the project has problems: %+v", v.Problems)
	}
	orders := v.Open
	if orders == nil || orders.File != "toolkits/orders.yaml" || orders.InstructionsFile != "toolkits/orders.md" || orders.Instructions != "" {
		t.Fatalf("the toolkit is %+v", orders)
	}
	if len(orders.Offered) != 1 || orders.Offered[0].When != "input.asker.scopes.includes('orders:read')" {
		t.Errorf("the agent offers the toolkit as %+v", orders.Offered)
	}
	var starts []string
	for _, s := range orders.Offered[0].Starts {
		starts = append(starts, s.File+" "+s.Type)
	}
	if got := strings.Join(starts, ", "); got != "caller.yaml flow, conversation.yaml statechart" {
		t.Errorf("the agent called by its location is started by %q", got)
	}
	find := toolNamed(t, orders, "find")
	if find.Job != "call" || find.Call != "people/find" || find.File != "people/find.yaml" || !slices.Equal(find.Fixed, []string{"table"}) ||
		!find.Approve || find.When != "input.asker.name != ''" || find.Description != "Find a person by email." {
		t.Errorf("the call tool is %+v", find)
	}
	// A tool whose job is a call is a call, and one whose job is a send is
	// a send, as the agent's drawing says.
	if tell := toolNamed(t, orders, "tell"); tell.Job != "call" || tell.Call != "chat/post" || tell.File != "chat/post.yaml" ||
		!slices.Equal(tell.Fixed, []string{"room"}) || !tell.Approve || tell.Description != "Tell the room." {
		t.Errorf("a tool whose job is a call is %+v", tell)
	}
	if ping := toolNamed(t, orders, "ping"); ping.Job != "send" || ping.Event != "ping" || ping.To != "chat/post" || ping.File != "chat/post.yaml" || ping.Call != "" {
		t.Errorf("a tool whose job is a send is %+v", ping)
	}

	infra := byDescription(t, v, "The machines.")
	if infra.Name != "infra" || len(infra.Offered) != 0 || infra.Instructions != "Restart only when asked." {
		t.Errorf("the toolkit nothing offers is %+v", infra)
	}
	if r := toolNamed(t, &infra, "restart"); r.Job != "call" || r.File != "people/find.yaml" || r.Description != "Find a person by email." || !slices.Equal(r.Fixed, []string{"table"}) || r.When != "input.x" {
		t.Errorf("a call tool in a file nothing offers is %+v", r)
	}
	if tell := toolNamed(t, &infra, "tell"); tell.Job != "call" || tell.Call != "chat/post" || tell.File != "chat/post.yaml" ||
		!slices.Equal(tell.Fixed, []string{"room"}) || !tell.Approve || tell.Description != "Tell the room." {
		t.Errorf("a tool whose job is a call, in a file nothing offers, is %+v", tell)
	}
	if ping := toolNamed(t, &infra, "ping"); ping.Job != "send" || ping.Event != "ping" || ping.To != "chat/post#infra" || ping.File != "chat/post.yaml" {
		t.Errorf("a tool whose job is a send, in a file nothing offers, is %+v", ping)
	}

	h, err := New(Config{Dir: dir, Interval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	body := do(h, "GET", "/toolkits?toolkit=orders", nil).Body.String()
	for _, want := range []string{
		`<p class="instructions">instructions <a href="/machines?file=toolkits%2forders.md"`,
		`<code class="when">when: input.asker.scopes.includes(&#39;orders:read&#39;)</code>`,
		`<section class="tool when" title="when: input.asker.name != &#39;&#39;">`,
		`<p class="job">call <a href="/machines?file=people%2ffind.yaml" title="people/find.yaml"><code>people/find</code></a></p><p class="job">input: table</p>`,
		`<p class="job">call <a href="/machines?file=chat%2fpost.yaml" title="chat/post.yaml"><code>chat/post</code></a></p><p class="job">input: room</p>`,
		`<p class="job">send <code>ping</code> to <a href="/machines?file=chat%2fpost.yaml" title="chat/post.yaml"><code>chat/post</code></a></p>`,
		"needs approval", ">conversation</a>", ">caller</a>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not show %q:\n%s", want, body)
		}
	}
	if body := do(h, "GET", "/toolkits?toolkit=infra", nil).Body.String(); !strings.Contains(body, `<p class="instructions" title="instructions">Restart only when asked.</p>`) {
		t.Errorf("the instructions written in a toolkit's file are not shown:\n%s", body)
	}

	// The agent's drawing links a call tool's card to the machine it calls,
	// and the conversation's to the agent.
	agent := do(h, "GET", "/machines?file=agents/support.yaml", nil).Body.String()
	for _, want := range []string{`<a href="/machines?file=people%2Ffind.yaml"><g class="state tool when">`, `<g class="state holder toolkit when">`, ">call: people/find<"} {
		if !strings.Contains(agent, want) {
			t.Errorf("the agent's drawing does not hold %q:\n%s", want, agent)
		}
	}
	if body := do(h, "GET", "/machines?file=conversation.yaml", nil).Body.String(); !strings.Contains(body, `<a href="/machines?file=agents%2Fsupport.yaml">`) {
		t.Errorf("a call's card does not link to the machine it calls:\n%s", body)
	}
}

func Test_machines_toolkits_in_one_directory_are_named_by_their_files(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		".corbieignore":        "toolkits/\n",
		"r.js":                 "return 1",
		"toolkits/orders.yaml": "description: orders\ntools:\n  r: { description: r, job: { script: r.js } }\n",
		"toolkits/infra.yaml":  "description: infra\ntools:\n  r: { description: r, job: { script: r.js } }\n",
		"toolkits/typed.yaml":  "description: typed\ntools: {}\nflow: typed\n",
	})
	v := toolkitsView_(t, dir, "")
	var ids []string
	for _, p := range v.Toolkits {
		ids = append(ids, p.ID+" "+p.File)
	}
	if got := strings.Join(ids, ", "); got != "infra toolkits/infra.yaml, orders toolkits/orders.yaml" {
		t.Errorf("the toolkits are %q", got)
	}
}

// A placed agent takes its type from a file .corbieignore lists, and writes
// a toolkit in place: the toolkit is in the agent's own file, so it has no
// file it is included from, on the Toolkits page, in the agent's drawing,
// or in the drawing of a machine that runs it, unfolded.
func Test_machines_toolkits_written_in_a_placed_agent_are_in_its_file(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		".corbieignore":       "shared/\n",
		"shared/agent.yaml":   "agent: support\n" + agentHead,
		"agents/support.yaml": "include: shared/agent.yaml\ntoolkits:\n  notes: { description: written here, tools: { r: { description: r, job: { script: ./r.js } } } }\n",
		"agents/r.js":         "return 1",
		"parent.yaml":         "flow: parent\nstages:\n  a: { job: { machine: agents/support.yaml } }\n",
		"dotted.yaml":         "flow: dotted\nstages:\n  a: { job: { machine: ./agents/support.yaml } }\n",
	})
	v := toolkitsView_(t, dir, "")
	if len(v.Problems) != 0 {
		t.Fatalf("the project has problems: %+v", v.Problems)
	}
	if p := byDescription(t, v, "written here"); p.File != "" {
		t.Errorf("a toolkit written in a placed agent is included from %q", p.File)
	}
	h, err := New(Config{Dir: dir, Interval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	holder := regexp.MustCompile(`<g class="state holder toolkit">.*?</g>`)
	for _, address := range []string{"/machines?file=agents/support.yaml", "/machines?file=parent.yaml&open=a", "/machines?file=dotted.yaml&open=a"} {
		body := do(h, "GET", address, nil).Body.String()
		found := holder.FindAllString(body, -1)
		if len(found) != 1 {
			t.Errorf("%s holds %d toolkits:\n%s", address, len(found), body)
		}
		for _, g := range found {
			if strings.Contains(g, "<title>") {
				t.Errorf("%s names a file the toolkit is included from: %s", address, g)
			}
		}
	}
}
