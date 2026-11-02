package machines

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zummed/corbie-ui/page"
	"github.com/zummed/corbie/client/cli"
	"github.com/zummed/corbie/identity"
	"github.com/zummed/corbie/store"

	_ "github.com/zummed/corbie/agent"
	_ "github.com/zummed/corbie/flow"
	_ "github.com/zummed/corbie/statechart"
)

const review = `statechart: review
input: { document: string }
initial: drafting
states:
  drafting:
    job: { machine: ./writer.yaml, input: { document: "${input.document}" } }
    on: { done: waiting, failed: gave_up }
  waiting:
    ticket: verdict
    on:
      verdict:
        - { go: approved, when: "${event.data.ok}" }
        - { go: drafting }
  approved: { end: done }
  gave_up: { end: failed }
`

const writer = `flow: writer
input: { document: string }
stages:
  draft:
    job: { script: ./write.js, input: { document: "${input.document}" }, files: [./lib.js] }
output: ${stages.draft.output}
`

// machines writes a directory of machines, and gives the page on it, as
// the config says: running its commands, and acting as its controller.
func machines(t *testing.T, c Config) (http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	for name, text := range map[string]string{
		"review.yaml": review, "writer.yaml": writer, "write.js": "return 1", "lib.js": "module.exports = 1",
		"notes.yaml": "a: list of things\n", ".hidden/secret.yaml": "flow: hidden\n",
	} {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
		os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644)
	}
	c.Dir, c.Interval, c.Shell = dir, time.Second, page.Shell{}
	h, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	return h, dir
}

func corbie(t *testing.T, stdin string, args ...string) {
	t.Helper()
	var out, said bytes.Buffer
	if status := cli.Run(context.Background(), args, strings.NewReader(stdin), &out, &said); status != 0 {
		t.Fatalf("corbie %s: %s%s", strings.Join(args, " "), said.String(), out.String())
	}
}

// lab sets one computer up, as corbie init does, with the secret A set,
// and gives its controller and its store.
func lab(t *testing.T) (*identity.Identity, store.Store) {
	t.Helper()
	dir := t.TempDir()
	config, at := filepath.Join(dir, "config"), filepath.Join(dir, "store")
	corbie(t, "", "init", "--store", at, "--name", "lab", "--ceiling", "all", "--config", config)
	corbie(t, "swordfish", "controller", "secret", "set", "A", "-", "--config", config)
	ctl, err := identity.Config{Dir: config}.Load(identity.Controller)
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(at)
	if err != nil {
		t.Fatal(err)
	}
	return ctl, s
}

func do(h http.Handler, method, target string, form url.Values) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	if method == "POST" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	h.ServeHTTP(w, r)
	return w
}

func Test_machines_list_shows_what_each_item_is(t *testing.T) {
	h, dir := machines(t, Config{Edits: true})
	d, _ := newDir(dir)
	list, _ := d.list("")
	if len(list) != 1 || list[0].Path != "review.yaml" {
		t.Fatalf("the list holds %+v", list)
	}
	r := list[0]
	if r.First != "statechart: review" || r.Type != "statechart" || r.Size != "4 states" || r.Problems != 0 {
		t.Errorf("review is listed as %+v", r)
	}
	if len(r.Under) != 1 || r.Under[0].Path != "writer.yaml" || len(r.Files) != 0 {
		t.Fatalf("under review are %+v and %+v", r.Under, r.Files)
	}
	w := r.Under[0]
	if w.Size != "1 stage" || len(w.Files) != 2 || w.Files[0] != (named{Path: "write.js", Field: "script:", Icon: "script"}) || w.Files[1] != (named{Path: "lib.js", Field: "files:", Icon: "file"}) {
		t.Errorf("writer is listed as %+v", w)
	}
	body := do(h, "GET", "/machines?file=writer.yaml", nil).Body.String()
	for _, want := range []string{`<use href="#g-statechart"/>`, "statechart: review · 4 states", "flow: writer · 1 stage", "<small>files:</small>"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not show %s", want)
		}
	}
	if body := do(h, "GET", "/machines?file=review.yaml", nil).Body.String(); strings.Contains(body, "<small>files:</small>") {
		t.Errorf("what is under a machine below the one chosen is shown")
	}
}

// project writes a directory laid out as a project of ready-made machines
// is: a machine to keep running, machines in a directory whose name begins
// with a dot, and an agent with a part from a file of its own.
func project(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range map[string]string{
		"telegram.yaml": "# starts the bridge\nflow: bridge\nstages:\n  listen: { job: { machine: ./.kit/listen.yaml } }\n",
		"later.yaml":    "flow: later\nstages:\n  say: { job: { machine: ./.kit/say.yaml } }\n",
		".kit/listen.yaml": `statechart: listen
initial: hearing
states:
  hearing:
    job: ./hear.js
    on:
      message: { send: { to: { machine: ./chat.yaml, key: "a" }, event: said } }
      done: hearing
`,
		".kit/chat.yaml": `# one conversation
statechart: chat
initial: answering
states:
  answering:
    job:
      machine:
        flow: message
        stages:
          answer: { job: { machine: helper.yaml } }
          say: { job: { machine: ./say.yaml } }
    on: { done: over }
  over: { end: done }
`,
		".kit/unused.yaml": "flow: unused\nstages:\n  a: { job: ./hear.js }\n",
		".kit/say.yaml":    "flow: say\nstages:\n  a: { job: ./hear.js }\n",
		".kit/hear.js":     "return 1",
		"helper.yaml":      "agent: helper\nmodel: { name: claude-sonnet-5-5, secret: KEY }\nprompt: hello\nopen:\n  messages: { include: ./parts/part.yaml }\n",
		"parts/part.yaml":  "does: messages\nask: { machine: .kit/say.yaml }\ntools:\n  tell: { does: tells, job: { machine: .kit/say.yaml, approve: true } }\n",
		"people.yaml":      "people: []\n",
	} {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
		os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644)
	}
	return dir
}

func must(data []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return data
}

func paths(list []entry) string {
	var names []string
	for _, e := range list {
		name := e.Path
		if e.Again {
			name += "*"
		}
		if len(e.Under) > 0 {
			name += "(" + paths(e.Under) + ")"
		}
		names = append(names, name)
	}
	return strings.Join(names, " ")
}

func Test_machines_list_follows_what_names_what(t *testing.T) {
	dir := project(t)
	d, _ := newDir(dir)
	list, _ := d.list(".kit/chat.yaml")
	want := "later.yaml(.kit/say.yaml) telegram.yaml(.kit/listen.yaml(.kit/chat.yaml(.kit/say.yaml* helper.yaml(.kit/say.yaml*))))"
	if got := paths(list); got != want {
		t.Errorf("the list is\n%s\nand not\n%s", got, want)
	}
	if p := d.check("helper.yaml", must(os.ReadFile(filepath.Join(dir, "helper.yaml")))).problems; len(p) > 0 {
		t.Fatalf("the agent has problems: %+v", p)
	}
	listen := list[1].Under[0]
	if listen.Problems != 0 {
		t.Errorf("a path out of a machine's directory, inside the directory of machines, is a problem: %+v", listen)
	}
	chat := listen.Under[0]
	if !chat.Here || listen.Here {
		t.Errorf("the open file is not marked: %+v", chat)
	}
	helper := chat.Under[1]
	if helper.Size != "1 tool" || len(helper.Files) != 1 || helper.Files[0].Path != "parts/part.yaml" || helper.Files[0].Field != "include:" {
		t.Errorf("the agent is listed as %+v", helper)
	}
}

func Test_machines_list_holds_what_a_machine_with_problems_names(t *testing.T) {
	dir := project(t)
	broken := "flow: bridge\nstagez: 1\nstages:\n  listen: { job: { machine: ./.kit/listen.yaml } }\n"
	os.WriteFile(filepath.Join(dir, "telegram.yaml"), []byte(broken), 0o644)
	d, _ := newDir(dir)
	list, _ := d.list("")
	if got := paths(list); !strings.Contains(got, ".kit/listen.yaml(.kit/chat.yaml(") || !strings.Contains(got, "helper.yaml(") {
		t.Errorf("a problem in a machine hides what it names: %s", got)
	}
}

func Test_machines_list_holds_machines_that_name_each_other(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.yaml"), []byte("flow: a\nstages:\n  x: { job: { machine: ./b.yaml } }\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.yaml"), []byte("flow: b\nstages:\n  x: { job: { machine: ./a.yaml } }\n"), 0o644)
	d, _ := newDir(dir)
	list, _ := d.list("")
	if got := paths(list); got != "a.yaml(b.yaml(a.yaml*))" {
		t.Errorf("the list is %q", got)
	}
}

func Test_machines_list_keeps_what_is_not_read_as_a_machine(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.yaml"), []byte("---\nflow: a\nstages:\n  x: { job: { machine: ./b.yml } }\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.yml"), []byte("flow: b\nstages:\n  x: { job: ./c.js }\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "c.js"), []byte("return 1"), 0o644)
	d, _ := newDir(dir)
	list, _ := d.list("a.yaml")
	if len(list) != 1 || len(list[0].Files) != 1 || list[0].Files[0].Path != "b.yml" {
		t.Errorf("the list holds %+v", list)
	}
}

func Test_machines_list_marks_the_file_open_once(t *testing.T) {
	dir := project(t)
	d, _ := newDir(dir)
	marked := func(list []entry) int {
		n := 0
		var walk func(list []entry)
		walk = func(list []entry) {
			for _, e := range list {
				if e.Here {
					n++
				}
				walk(e.Under)
			}
		}
		walk(list)
		return n
	}
	list, _ := d.list(".kit/say.yaml")
	if n := marked(list); n != 1 {
		t.Errorf("the machine open is marked %d times", n)
	}
	list, _ = d.list(".kit/hear.js")
	listen := list[1].Under[0]
	if !list[0].Shown || !listen.Shown || len(listen.Files) != 1 || !listen.Files[0].Here {
		t.Errorf("a script open is not shown in the list: %+v", listen)
	}
	h, _ := New(Config{Dir: dir, Interval: time.Second})
	if body := do(h, "GET", "/machines?file=./later.yaml", nil).Body.String(); !strings.Contains(body, `aria-current="page"`) {
		t.Errorf("a path written with ./ is not the file open")
	}
}

func Test_machines_list_says_what_it_could_not_read(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads every directory")
	}
	dir := project(t)
	locked := filepath.Join(dir, "a-locked")
	os.Mkdir(locked, 0o000)
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	h, _ := New(Config{Dir: dir, Interval: time.Second})
	if body := do(h, "GET", "/machines", nil).Body.String(); !strings.Contains(body, `class="unread"`) || !strings.Contains(body, "a-locked") {
		t.Errorf("a directory that cannot be read is not reported:\n%s", body)
	}
}

func Test_machines_drawing_unfolds_as_the_address_says(t *testing.T) {
	h, err := New(Config{Dir: project(t), Interval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	body := do(h, "GET", "/machines?file=.kit/listen.yaml", nil).Body.String()
	if !strings.Contains(body, ">hearing<") || !strings.Contains(body, ">chat.yaml<") {
		t.Fatalf("the machine is not drawn:\n%s", body)
	}
	link := regexp.MustCompile(`<a href="(/machines\?[^"]*)"><g class="[^"]*sends`).FindStringSubmatch(body)
	if link == nil {
		t.Fatalf("a send that names a machine has no link:\n%s", body)
	}
	address := strings.ReplaceAll(strings.ReplaceAll(link[1], "&amp;", "&"), "&#43;", "+")
	unfolded := do(h, "GET", address, nil).Body.String()
	if !strings.Contains(unfolded, ">answering<") {
		t.Fatalf("%s does not unfold the machine:\n%s", address, unfolded)
	}
	second := regexp.MustCompile(`<a href="(/machines\?[^"]*)"><g class="[^"]*"><rect[^>]*/><text[^>]*>answering<`).FindStringSubmatch(unfolded)
	if second == nil {
		t.Fatalf("the unfolded machine has no links:\n%s", unfolded)
	}
	deeper := do(h, "GET", strings.ReplaceAll(second[1], "&amp;", "&"), nil).Body.String()
	if !strings.Contains(deeper, ">flow: message<") || !strings.Contains(deeper, ">answer<") {
		t.Errorf("a machine written in place does not unfold:\n%s", deeper)
	}
	folded := regexp.MustCompile(`<a href="(/machines\?[^"]*)"><g class="[^"]*sends[^"]*unfolded`).FindStringSubmatch(deeper)
	if folded == nil {
		t.Fatalf("the unfolded box has no link:\n%s", deeper)
	}
	if u, _ := url.Parse(strings.ReplaceAll(folded[1], "&amp;", "&")); len(u.Query()["open"]) != 0 {
		t.Errorf("folding a box leaves what is inside it unfolded: %s", u)
	}

	part := do(h, "GET", "/machines?file=helper.yaml", nil).Body.String()
	for _, want := range []string{">tell<", "parts/part.yaml", ">approve: true<", ">ask<"} {
		if !strings.Contains(part, want) {
			t.Errorf("the agent's drawing does not hold %s", want)
		}
	}
	if !strings.Contains(part, `href="/machines?file=helper.yaml&amp;view=text"`) {
		t.Errorf("the drawing does not offer the text")
	}
	if text := do(h, "GET", "/machines?file=helper.yaml&view=text", nil).Body.String(); !strings.Contains(text, "agent: helper") || !strings.Contains(text, " readonly>") {
		t.Errorf("the text is not shown read-only:\n%s", text)
	}
	if text := do(h, "GET", "/machines?file=people.yaml", nil).Body.String(); !strings.Contains(text, "people: []") {
		t.Errorf("a file that is not a machine does not show its text:\n%s", text)
	}
	bad, _ := newDir(project(t))
	os.WriteFile(filepath.Join(bad.root, "broken.yaml"), []byte("flow: broken\nstages:\n  a: { job: ./missing.js }\n"), 0o644)
	h, _ = New(Config{Dir: bad.root, Interval: time.Second})
	w := do(h, "GET", "/machines?file=broken.yaml", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "missing.js") {
		t.Errorf("a machine with problems answered %d:\n%s", w.Code, w.Body.String())
	}
}

func Test_machines_drawing_labels_a_part_of_an_agent_written_in_place(t *testing.T) {
	dir := project(t)
	os.WriteFile(filepath.Join(dir, "here.yaml"), []byte("flow: here\nstages:\n  look:\n    job:\n      machine:\n        agent: in\n        model: { name: claude-sonnet-5-5, secret: KEY }\n        prompt: hello\n        open:\n          messages: { include: ./parts/part.yaml }\n"), 0o644)
	h, err := New(Config{Dir: dir, Interval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	body := do(h, "GET", "/machines?file=here.yaml&open=look", nil).Body.String()
	_, drawn, ok := strings.Cut(body, `id="drawing"`)
	if !ok {
		t.Fatalf("the page has no drawing:\n%s", body)
	}
	if !strings.Contains(drawn, "messages · parts/part.yaml") {
		t.Errorf("the part's file is not in its label:\n%s", drawn)
	}
}

func Test_machines_read_only_writes_nothing(t *testing.T) {
	h, dir := machines(t, Config{})
	body := do(h, "GET", "/machines?view=text", nil).Body.String()
	for _, word := range []string{`id="save"`, `id="test"`, `id="start"`, `action="/machines/new"`, `id="run"`} {
		if strings.Contains(body, word) {
			t.Errorf("a UI that does not act as a client shows %s", word)
		}
	}
	before, _ := os.ReadFile(filepath.Join(dir, "review.yaml"))
	for _, path := range []string{"/machines/save", "/machines/new", "/machines/start"} {
		w := do(h, "POST", path, url.Values{"file": {"review.yaml"}, "text": {"x"}, "base": {hash(before)}, "type": {"flow"}, "name": {"n"}})
		if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s answered %d", path, w.Code)
		}
	}
	if w := do(h, "GET", "/machines/test?file=review.yaml", nil); w.Code != http.StatusNotFound {
		t.Errorf("Test answered %d", w.Code)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "review.yaml"))
	if string(after) != string(before) {
		t.Errorf("the file was written")
	}
	if _, err := os.Stat(filepath.Join(dir, "n.yaml")); err == nil {
		t.Errorf("a new file was written")
	}
}

func Test_machines_check_gives_problems_drawing_and_fields(t *testing.T) {
	h, _ := machines(t, Config{Edits: true})
	check := func(text string, line int) map[string]any {
		w := do(h, "POST", "/machines/check", url.Values{"file": {"review.yaml"}, "text": {text}, "line": {jsonOf(line)}})
		var got map[string]any
		json.Unmarshal(w.Body.Bytes(), &got)
		return got
	}
	broken := strings.Replace(review, "gave_up: { end: failed }", "gave_up: { end: failed, colour: red }", 1)
	got := check(broken, 9)
	if got["count"] != 1.0 || !strings.Contains(got["problems"].(string), "states.gave_up.colour") || got["drawing"] != "" {
		t.Errorf("a definition with a problem gave %v", got)
	}
	if lines, _ := got["lines"].([]any); len(lines) != 1 || lines[0] != 15.0 {
		t.Errorf("the problem's line is given as %v", got["lines"])
	}
	if f := got["fields"].(string); !strings.Contains(f, "Fields of a state") || !strings.Contains(f, `>ticket</span>`) || strings.Count(f, `class="here"`) != 2 {
		t.Errorf("at line 9 the fields are %v", got["fields"])
	}
	got = check(review, 1)
	if got["count"] != 0.0 || !strings.Contains(got["drawing"].(string), "<svg") || !strings.Contains(got["fields"].(string), "Fields of a definition") {
		t.Errorf("a definition with no problems gave %v", got)
	}
}

func jsonOf(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

// recorder runs no command, and keeps every call; its test prints lines.
type recorder struct{ calls []page.Call }

func (r *recorder) run(_ context.Context, c page.Call) page.Answer {
	r.calls = append(r.calls, c)
	if c.Lines != nil {
		c.Lines.Write([]byte("12:00:00 0123456789  step 1  <- start {}\n12:00:00 0123456789  step 1  done\n"))
		return page.Answer{}
	}
	return page.Answer{Out: `{"id":"lab-client/20261015T120000.000Z-0123456789"}`}
}

func Test_machines_test_and_start_run_their_commands(t *testing.T) {
	r := &recorder{}
	h, dir := machines(t, Config{Edits: true, Command: r.run})
	w := do(h, "POST", "/machines/start", url.Values{"file": {"writer.yaml"}, "input": {`{ "document": "q4" }`}, "secrets": {"API_KEY"},
		"labels": {"team=data doc=q4"}, "allow": {"net:example.com"}, "deny": {"exec"}, "timeout": {"1h"}})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"/run/lab-client/20261015T120000.000Z-0123456789"`) {
		t.Errorf("Start answered %d %s", w.Code, w.Body.String())
	}
	server := httptest.NewServer(h)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/machines/test?file=writer.yaml&input=%7B%7D&secrets=API_KEY", nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		got = append(got, string(data))
	}
	if len(got) != 2 || got[1] != "12:00:00 0123456789  step 1  done" {
		t.Errorf("Test sent the page %q", got)
	}
	file := filepath.Join(dir, "writer.yaml")
	want := []string{
		"corbie run submit " + file + ` --input={"document":"q4"} --secret-env=API_KEY --label=team=data --label=doc=q4 --allow=net:example.com --deny=exec --timeout=1h`,
		"corbie test " + file + " --input={} --secret-env=API_KEY --watch",
	}
	if len(r.calls) != 2 || r.calls[0].String() != want[0] || r.calls[1].String() != want[1] {
		t.Errorf("Start and Test ran %v", r.calls)
	}
	for _, c := range r.calls {
		if found, _ := cli.Find(c.Words); found == nil || found.Name != strings.Join(c.Words, " ") {
			t.Errorf("%q is not one of Corbie's commands", strings.Join(c.Words, " "))
		}
	}
}

func Test_machines_path_outside_the_directory_is_refused(t *testing.T) {
	h, dir := machines(t, Config{Edits: true})
	outside := filepath.Join(t.TempDir(), "elsewhere.yaml")
	os.WriteFile(outside, []byte(review), 0o644)
	os.Symlink(outside, filepath.Join(dir, "link.yaml"))
	for _, path := range []string{"../elsewhere.yaml", outside, "/etc/passwd", "link.yaml", "sub/../../x.yaml"} {
		if w := do(h, "GET", "/machines/raw?file="+url.QueryEscape(path), nil); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "leaves the directory") {
			t.Errorf("reading %s answered %d %s", path, w.Code, w.Body.String())
		}
		if w := do(h, "POST", "/machines/save", url.Values{"file": {path}, "text": {"x"}, "base": {""}}); w.Code != http.StatusForbidden {
			t.Errorf("saving %s answered %d", path, w.Code)
		}
	}
	d, _ := newDir(dir)
	list, _ := d.list("")
	if strings.Contains(paths(list), "link.yaml") {
		t.Errorf("a link that leaves the directory is listed: %s", paths(list))
	}
	if h, _ := machines(t, Config{Edits: true}); h != nil {
		text := "flow: out\nstages:\n  a: { job: ../../../outside.js }\n"
		w := do(h, "POST", "/machines/check", url.Values{"file": {"review.yaml"}, "text": {text}})
		if !strings.Contains(w.Body.String(), "has no ../") {
			t.Errorf("a script outside the directory of machines was read: %s", w.Body.String())
		}
	}
	if w := do(h, "POST", "/machines/start", url.Values{"file": {"../elsewhere.yaml"}}); w.Code != http.StatusForbidden {
		t.Errorf("starting a file outside the directory answered %d", w.Code)
	}
}

func Test_machines_save_refuses_a_file_changed_on_disk(t *testing.T) {
	h, dir := machines(t, Config{Edits: true})
	var read map[string]string
	json.Unmarshal(do(h, "GET", "/machines/raw?file=writer.yaml", nil).Body.Bytes(), &read)
	os.WriteFile(filepath.Join(dir, "writer.yaml"), []byte(writer+"# a model wrote this\n"), 0o644)
	w := do(h, "POST", "/machines/save", url.Values{"file": {"writer.yaml"}, "text": {"flow: mine\n"}, "base": {read["base"]}})
	var refused map[string]string
	json.Unmarshal(w.Body.Bytes(), &refused)
	if w.Code != http.StatusConflict || !strings.Contains(refused["text"], "# a model wrote this") {
		t.Errorf("a save over a changed file answered %d %v", w.Code, refused)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "writer.yaml")); !strings.Contains(string(data), "# a model wrote this") {
		t.Errorf("the change on disk was lost")
	}
	if w := do(h, "POST", "/machines/save", url.Values{"file": {"writer.yaml"}, "text": {"flow: mine\n"}, "base": {refused["base"]}}); w.Code != http.StatusOK {
		t.Errorf("a save from the file as it is now answered %d", w.Code)
	}
}

func Test_machines_new_refuses_a_name_in_use(t *testing.T) {
	h, dir := machines(t, Config{Edits: true})
	if w := do(h, "POST", "/machines/new", url.Values{"type": {"flow"}, "name": {"digest"}}); w.Code != http.StatusSeeOther {
		t.Fatalf("New answered %d %s", w.Code, w.Body.String())
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "digest.yaml")); string(data) != "flow: digest\n" {
		t.Errorf("New wrote %q", data)
	}
	if w := do(h, "POST", "/machines/new", url.Values{"type": {"statechart"}, "name": {"review"}}); w.Code != http.StatusConflict {
		t.Errorf("New over review.yaml answered %d", w.Code)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "review.yaml")); string(data) != review {
		t.Errorf("New changed a file that was there")
	}
	if w := do(h, "POST", "/machines/new", url.Values{"type": {"flow"}, "name": {"../up"}}); w.Code != http.StatusConflict {
		t.Errorf("New with a path for a name answered %d", w.Code)
	}
}

const shipper = `flow: shipper
allow: [net:x, secret:A@net:x]
stages:
  ship:
    job: { script: ./write.js, allow: [net:y, secret:B@net:y, net:z, "secret:*@net:z"] }
`

// later writes its own allow: after its stages.
const later = `flow: later
stages:
  ship:
    job: { script: ./write.js, allow: [secret:B@net:y] }
allow: [secret:A@net:x]
`

func Test_machines_secrets_named_are_listed_and_set(t *testing.T) {
	section := regexp.MustCompile(`(?s)<section id="secrets".*?</section>`)
	shown := func(h http.Handler, file string) (string, string) {
		t.Helper()
		w := do(h, "GET", "/machines?view=text&file="+file, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("the page answered %d %s", w.Code, w.Body.String())
		}
		return w.Body.String(), section.FindString(w.Body.String())
	}
	r := &recorder{}
	ctl, s := lab(t)
	h, dir := machines(t, Config{Edits: true, Command: r.run, Controller: ctl, Store: s})
	os.WriteFile(filepath.Join(dir, "shipper.yaml"), []byte(shipper), 0o644)
	body, got := shown(h, "shipper.yaml")
	a, b := `<span class="name">A</span><div class="state">set</div>`, `<span class="name">B</span><div class="state">not set<form method="post" action="/machines/secret-set">`
	if !strings.Contains(body, "<h2>No problems</h2>") || strings.Count(got, `class="name"`) != 2 || !strings.Contains(got, a) || strings.Index(got, b) < strings.Index(got, a) || strings.Contains(got, "controller's computer") {
		t.Errorf("as the controller, the secrets are shown as\n%s", got)
	}
	if !strings.Contains(got, `type="password"`) || !strings.Contains(got, `data-cmd="corbie controller secret set B -"`) {
		t.Errorf("Set is not the Controller page's Set:\n%s", got)
	}
	if _, got := shown(h, "review.yaml"); got != `<section id="secrets" hidden></section>` {
		t.Errorf("a definition that names no secret shows\n%s", got)
	}
	var checked map[string]any
	json.Unmarshal(do(h, "POST", "/machines/check", url.Values{"file": {"shipper.yaml"}, "text": {strings.Replace(shipper, "net:z,", "net:z, secret:C@net:z,", 1)}, "line": {"1"}}).Body.Bytes(), &checked)
	if got, _ := checked["secrets"].(string); !strings.Contains(got, `<span class="name">C</span><div class="state">not set<form`) {
		t.Errorf("the check of text naming another secret gave\n%s", got)
	}
	json.Unmarshal(do(h, "POST", "/machines/check", url.Values{"file": {"shipper.yaml"}, "text": {later}, "line": {"1"}}).Body.Bytes(), &checked)
	if got, _ := checked["secrets"].(string); strings.Count(got, `class="name"`) != 2 || strings.Index(got, `<span class="name">B</span>`) > strings.Index(got, `<span class="name">A</span>`) {
		t.Errorf("a definition whose own allow: follows its stages lists its secrets as\n%s", got)
	}

	w := do(h, "POST", "/machines/secret-set", url.Values{"name": {"B"}, "value": {"hunter2"}})
	if w.Code != http.StatusOK || len(r.calls) != 1 {
		t.Fatalf("Set answered %d %s and ran %v", w.Code, w.Body.String(), r.calls)
	}
	if c := r.calls[0]; c.String() != "corbie controller secret set B -" || c.Stdin != "hunter2" {
		t.Errorf("Set ran %q with %q on standard input", c.String(), c.Stdin)
	}
	body, _ = shown(h, "shipper.yaml")
	if strings.Contains(w.Body.String(), "hunter2") || strings.Contains(body, "hunter2") || strings.Contains(body, "swordfish") {
		t.Errorf("a secret's value was sent to the page")
	}

	r = &recorder{}
	h, dir = machines(t, Config{Edits: true, Command: r.run})
	os.WriteFile(filepath.Join(dir, "shipper.yaml"), []byte(shipper), 0o644)
	_, got = shown(h, "shipper.yaml")
	if strings.Count(got, `class="name"`) != 2 || !strings.Contains(got, `<span class="name">A</span><div class="state"></div>`) || strings.Contains(got, "secret-set") ||
		!strings.Contains(got, "set on the controller's computer: <code>corbie controller secret set NAME</code>") {
		t.Errorf("as a client alone, the secrets are shown as\n%s", got)
	}
	if w := do(h, "POST", "/machines/secret-set", url.Values{"name": {"B"}, "value": {"hunter2"}}); w.Code != http.StatusNotFound || len(r.calls) != 0 {
		t.Errorf("Set without the controller answered %d and ran %v", w.Code, r.calls)
	}
}

func Test_machines_input_is_json_and_never_a_file(t *testing.T) {
	r := &recorder{}
	h, _ := machines(t, Config{Edits: true, Command: r.run})
	for _, input := range []string{"@/etc/passwd", "@writer.yaml", "not json"} {
		if w := do(h, "POST", "/machines/start", url.Values{"file": {"writer.yaml"}, "input": {input}}); w.Code != http.StatusBadRequest {
			t.Errorf("Start with the input %q answered %d", input, w.Code)
		}
	}
	if len(r.calls) != 0 {
		t.Errorf("a command ran with input that is not JSON: %v", r.calls)
	}
}
