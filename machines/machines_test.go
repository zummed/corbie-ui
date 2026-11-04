package machines

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

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
// the config says: acting as its controller, or not.
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

// listed gives the list as directory, file, first line and problem count.
func listed(ix *index) []string {
	var out []string
	for _, g := range ix.Groups {
		for _, e := range g.Entries {
			out = append(out, fmt.Sprintf("%s %s %s %d", g.Dir, e.Path, e.First, e.Problems))
		}
	}
	return out
}

func Test_machines_list_shows_what_each_item_is(t *testing.T) {
	dir := project(t)
	write(t, dir, map[string]string{
		"bridges/b.yaml": "flow: b\nstages:\n  a: { job: { machine: .kit/say.yaml } }\n",
		"bad.yaml":       "flow: bad\nstages: {}\ncolour: blue\n",
	})
	d, _ := newDir(dir)
	got := listed(d.index())
	want := []string{". bad.yaml flow: bad 2", ". helper.yaml agent: helper 0", ". later.yaml flow: later 0", ". telegram.yaml flow: bridge 0", "bridges bridges/b.yaml flow: b 0"}
	if !slices.Equal(got, want) {
		t.Errorf("the list is\n%s\nand not\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	h, _ := New(Config{Dir: dir, Interval: time.Second})
	body := do(h, "GET", "/machines?file=bridges/b.yaml", nil).Body.String()
	_, list, _ := strings.Cut(body, `id="files"`)
	list, _, _ = strings.Cut(list, "</nav>")
	for _, want := range []string{`<h2 class="group" title="bridges">bridges/</h2>`, `<use href="#g-agent"/>`, "<small>agent: helper</small>", `<span class="bad" title="2 problems">2</span>`, `href="/machines?file=bridges%2fb.yaml" aria-current="page"`} {
		if !strings.Contains(list, want) {
			t.Errorf("the list does not show %s:\n%s", want, list)
		}
	}
	for _, gone := range []string{".kit/", "people.yaml", "part.yaml"} {
		if strings.Contains(list, gone) {
			t.Errorf("the list holds %s:\n%s", gone, list)
		}
	}
	if body := do(h, "GET", "/machines?file=./bridges/b.yaml", nil).Body.String(); !strings.Contains(body, `aria-current="page"`) {
		t.Errorf("a path written with ./ is not the file chosen")
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

func Test_machines_problems_are_listed_once_at_their_file(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"a.yaml":            "agent: a\n" + agentHead + "open:\n  messages: { include: ./parts/m/part.yaml }\n",
		"b.yaml":            "agent: b\n" + agentHead + "open:\n  msgs: { include: ./parts/m/part.yaml }\n",
		".kit/c.yaml":       "agent: c\n" + agentHead + "open:\n  messages: { include: parts/m/part.yaml }\n",
		"parts/m/part.yaml": "does: messages\ntools:\n  tell:\n    description: tells\n    job: { script: ./tell.js }\n",
		"parts/m/tell.js":   "return 1",
		"z.yaml":            "flow: z\nstages:\n  a: { job: ./z.js }\ncolour: blue\n",
		"z.js":              "return 1",
	})
	d, _ := newDir(dir)
	ix := d.index()
	var atPart int
	for _, p := range ix.Problems {
		switch p.File {
		case "parts/m/part.yaml":
			atPart++
			if !slices.Equal(p.Breaks, []string{"a.yaml", "b.yaml"}) || !strings.HasPrefix(p.Place, "tools.tell") || p.Line == 0 {
				t.Errorf("a problem in the part is %+v", p)
			}
		case "z.yaml":
			if !slices.Equal(p.Breaks, []string{"z.yaml"}) || p.Place != "colour" || p.Line != 4 {
				t.Errorf("a problem in a machine's own file is %+v", p)
			}
		default:
			t.Errorf("a problem is at %+v", p)
		}
	}
	if atPart == 0 || len(ix.Problems) != atPart+1 {
		t.Errorf("the problems are %+v", ix.Problems)
	}
	if ix.Broken != 3 {
		t.Errorf("%d machines of the project have problems", ix.Broken)
	}
	h, _ := New(Config{Dir: dir, Interval: time.Second})
	body := do(h, "GET", "/machines?file=z.yaml", nil).Body.String()
	if n := strings.Count(body, "breaks 2 machines"); n != atPart {
		t.Errorf("the page shows the part's problems %d times, and not %d:\n%s", n, atPart, body)
	}
	if strings.Contains(body, ".kit/c.yaml") {
		t.Errorf("a machine in a dotted directory is counted:\n%s", body)
	}

	// A file that three machines include, which cannot be read, is one
	// problem at that file, however each machine writes its path.
	for name, base := range map[string]string{"not YAML": "stages: [ { a: \n", "missing": ""} {
		dir := t.TempDir()
		files := map[string]string{
			"w.js":           "return 1",
			"one.yaml":       "flow: one\ninclude: ./base.yaml\n",
			"two.yaml":       "flow: two\ninclude: ./base.yaml\n",
			"sub/three.yaml": "flow: three\ninclude: base.yaml\n",
		}
		if base != "" {
			files["base.yaml"] = base
		}
		write(t, dir, files)
		d, _ := newDir(dir)
		ix := d.index()
		if len(ix.Problems) != 1 || ix.Problems[0].File != "base.yaml" || len(ix.Problems[0].Breaks) != 3 {
			t.Errorf("a %s file three machines include gives the problems %+v", name, ix.Problems)
		}
		if got := listed(ix); slices.ContainsFunc(got, func(l string) bool { return strings.Contains(l, " base.yaml ") }) || ix.Broken != 3 {
			t.Errorf("with a %s file included, the list is %v and %d machines have problems", name, got, ix.Broken)
		}
	}
}

func Test_machines_directory_below_the_top_is_one_problem(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"proj/a.yaml":     "flow: a\nstages:\n  x: { job: { machine: b.yaml } }\n",
		"proj/b.yaml":     "flow: b\nstages:\n  x: { job: ./r.js }\n",
		"proj/r.js":       "return 1",
		"proj/sub/c.yaml": "flow: c\nstages:\n  x: { job: { machine: b.yaml } }\n",
		"z.yaml":          "flow: z\nstages: {}\ncolour: blue\n",
	})
	d, _ := newDir(dir)
	ix := d.index()
	if len(ix.Problems) == 0 {
		t.Fatalf("no problems")
	}
	top := ix.Problems[0]
	if !top.Top || top.File != "proj" || !slices.Equal(top.Breaks, []string{"proj/a.yaml", "proj/sub/c.yaml"}) {
		t.Errorf("the first problem is %+v", top)
	}
	for _, p := range ix.Problems[1:] {
		if p.File != "z.yaml" {
			t.Errorf("a machine read from the wrong directory gave %+v", p)
		}
	}
	if got := listed(ix); !slices.Contains(got, "proj proj/a.yaml flow: a 1") {
		t.Errorf("a machine read from the wrong directory counts other than the one problem shown at it: %v", got)
	}
	h, _ := New(Config{Dir: dir, Interval: time.Second})
	if body := do(h, "GET", "/machines", nil).Body.String(); !strings.Contains(body, "<code>proj/</code> is the top of a project") || !strings.Contains(body, "breaks 2 machines") {
		t.Errorf("the page does not say which directory is the top:\n%s", body)
	}
	if body := do(h, "GET", "/machines?file=proj/a.yaml", nil).Body.String(); strings.Count(body, "is the top of a project") != 2 || strings.Contains(body, "cannot be read") {
		t.Errorf("a machine read from the wrong directory does not say so:\n%s", body)
	}
}

func Test_machines_text_shows_the_problems_found_in_it(t *testing.T) {
	dir := project(t)
	write(t, dir, map[string]string{
		"broken.yaml":  "# one stage\nflow: broken\nstages:\n  a: { job: ./missing.js }\n",
		"a.yaml":       "agent: a\n" + agentHead + "open:\n  m: { include: ./parts/m.yaml }\n",
		"parts/m.yaml": "does: m\ntools:\n  r:\n    does: r\n    job: { script: ./r.js }\n    colour: blue\n",
		"parts/r.js":   "return 1",
	})
	h, _ := New(Config{Dir: dir, Interval: time.Second})
	body := do(h, "GET", "/machines?file=broken.yaml", nil).Body.String()
	if strings.Contains(body, `id="drawing"`) || !strings.Contains(body, "missing.js") || !strings.Contains(body, `<span class="l err"><span class="k">  a</span>`) && !strings.Contains(body, `<span class="l err">  <span class="k">a</span>`) {
		t.Errorf("a machine with problems is not shown as text with its line marked:\n%s", body)
	}
	if !strings.Contains(body, `<span class="l"><span class="c"># one stage</span></span>`) {
		t.Errorf("a comment is not set apart:\n%s", body)
	}
	part := do(h, "GET", "/machines?file=parts/m.yaml&view=text", nil).Body.String()
	if !strings.Contains(part, "tools.r.colour") || !strings.Contains(part, "breaks 1 machine") || strings.Count(part, `class="l err"`) != 1 {
		t.Errorf("a part's text does not show the problem found in it:\n%s", part)
	}
	if a := do(h, "GET", "/machines?file=a.yaml", nil).Body.String(); !strings.Contains(a, "tools.r.colour") || strings.Contains(a, `class="l err"`) {
		t.Errorf("a machine marks its lines for a problem in a file it includes:\n%s", a)
	}
	for _, file := range []string{"people.yaml", "helper.yaml"} {
		text := do(h, "GET", "/machines?view=text&file="+file, nil).Body.String()
		if strings.Contains(text, `class="found"`) || strings.Contains(text, "No problems") || strings.Contains(text, "<textarea") || !strings.Contains(text, `<pre class="code"`) {
			t.Errorf("%s is not shown as read-only text with no problems:\n%s", file, text)
		}
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
	text := do(h, "GET", "/machines?file=helper.yaml&view=text", nil).Body.String()
	if !strings.Contains(text, `<span class="k">agent</span>: helper`) || !strings.Contains(text, `href="/machines?file=helper.yaml">Drawing</a>`) {
		t.Errorf("the text is not shown:\n%s", text)
	}
	if text := do(h, "GET", "/machines?file=people.yaml", nil).Body.String(); !strings.Contains(text, `<span class="k">people</span>: []`) || strings.Contains(text, ">Drawing<") {
		t.Errorf("a file that is not a machine does not show its text alone:\n%s", text)
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
	if !strings.Contains(drawn, ">messages</text><title>parts/part.yaml</title>") {
		t.Errorf("the part's file is not in its tooltip:\n%s", drawn)
	}
}

func Test_machines_path_outside_the_directory_is_refused(t *testing.T) {
	h, dir := machines(t, Config{})
	outside := filepath.Join(t.TempDir(), "elsewhere.yaml")
	os.WriteFile(outside, []byte("flow: kept-elsewhere\nstages: {}\n"), 0o644)
	os.Symlink(outside, filepath.Join(dir, "link.yaml"))
	os.WriteFile(filepath.Join(dir, "out.yaml"), []byte("flow: out\nstages:\n  a: { job: ../../../outside.js }\n"), 0o644)
	for _, path := range []string{"../elsewhere.yaml", outside, "/etc/passwd", "link.yaml", "sub/../../x.yaml"} {
		if w := do(h, "GET", "/machines?view=text&file="+url.QueryEscape(path), nil); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "leaves the directory") {
			t.Errorf("reading %s answered %d %s", path, w.Code, w.Body.String())
		}
	}
	body := do(h, "GET", "/machines?file=out.yaml", nil).Body.String()
	if strings.Contains(body, "kept-elsewhere") {
		t.Errorf("a file a link outside the directory names is shown:\n%s", body)
	}
	if !strings.Contains(body, "has no ../") {
		t.Errorf("a script outside the directory of machines was read:\n%s", body)
	}
}

func Test_machines_secrets_named_are_listed(t *testing.T) {
	section := regexp.MustCompile(`(?s)<section class="secrets">.*?</section>`)
	ctl, s := lab(t)
	h, dir := machines(t, Config{Controller: ctl, Store: s})
	os.WriteFile(filepath.Join(dir, "shipper.yaml"), []byte(shipper), 0o644)
	os.WriteFile(filepath.Join(dir, "later.yaml"), []byte(later), 0o644)
	got := section.FindString(do(h, "GET", "/machines?view=text&file=shipper.yaml", nil).Body.String())
	a, b := `<span>A</span><span class="dim">set</span>`, `<span>B</span><span class="dim">not set</span>`
	if strings.Count(got, "<span>") != 2 || !strings.Contains(got, a) || strings.Index(got, b) < strings.Index(got, a) || strings.Contains(got, "controller's computer") {
		t.Errorf("as the controller, the secrets are shown as\n%s", got)
	}
	if strings.Contains(got, "<form") || strings.Contains(got, "<button") || strings.Contains(got, "<input") {
		t.Errorf("the secrets can be set from the Machines page:\n%s", got)
	}
	if got := section.FindString(do(h, "GET", "/machines?file=shipper.yaml", nil).Body.String()); !strings.Contains(got, a) {
		t.Errorf("the drawing does not list the secrets:\n%s", got)
	}
	if got := section.FindString(do(h, "GET", "/machines?file=later.yaml", nil).Body.String()); strings.Index(got, ">A<") < strings.Index(got, ">B<") {
		t.Errorf("the secrets are not in the order written:\n%s", got)
	}

	h, _ = machines(t, Config{})
	got = section.FindString(do(h, "GET", "/machines?view=text&file=review.yaml", nil).Body.String())
	if got != "" {
		t.Errorf("a machine that names no secret shows %s", got)
	}
	h, dir = machines(t, Config{})
	os.WriteFile(filepath.Join(dir, "shipper.yaml"), []byte(shipper), 0o644)
	got = section.FindString(do(h, "GET", "/machines?file=shipper.yaml", nil).Body.String())
	if !strings.Contains(got, `<span>A</span><span class="dim"></span>`) || !strings.Contains(got, "corbie controller secret set NAME") {
		t.Errorf("as a client alone, the secrets are shown as\n%s", got)
	}
}

func Test_machines_directory_is_read_once_per_change(t *testing.T) {
	dir := project(t)
	d, _ := newDir(dir)
	c := &indexed{dir: d}
	first := c.current()
	if c.current() != first {
		t.Errorf("the directory was read again with nothing changed")
	}
	write(t, dir, map[string]string{"new.yaml": "flow: new\nstages:\n  a: { job: .kit/hear.js }\n"})
	next := c.current()
	if next == first || !slices.Contains(listed(next), ". new.yaml flow: new 0") {
		t.Errorf("a file written is not read: %v", listed(next))
	}
	write(t, dir, map[string]string{"parts/part.yaml": "does: changed\ntools: {}\n"})
	if c.current() == next {
		t.Errorf("a change to a file that is not a machine is not read")
	}
}

func Test_machines_dotted_files_are_reached_through_what_names_them(t *testing.T) {
	dir := project(t)
	write(t, dir, map[string]string{
		".kit/alone.yaml":   "agent: alone\n" + agentHead + "open:\n  hidden: { does: hidden, tools: { r: { does: r, job: { script: ./hear.js } } } }\n",
		".kit/broken.yaml":  "flow: broken\nstages: {}\ncolour: blue\n",
		".kit/reached.yaml": "agent: reached\n" + agentHead + "open:\n  seen: { does: seen, tools: { r: { does: r, job: { script: ./hear.js } } } }\n",
		"uses.yaml":         "flow: uses\nstages:\n  a: { job: { machine: .kit/reached.yaml } }\n",
	})
	d, _ := newDir(dir)
	ix := d.index()
	for _, line := range listed(ix) {
		if strings.Contains(line, ".kit/") {
			t.Errorf("the list holds %s", line)
		}
	}
	if len(ix.Problems) != 0 || ix.Broken != 0 {
		t.Errorf("a machine in a dotted directory that nothing names gives problems: %+v", ix.Problems)
	}
	v := ix.partsView("")
	var does []string
	for _, p := range v.Parts {
		does = append(does, p.Does)
	}
	if slices.Contains(does, "hidden") || slices.Contains(does, "kept in a dotted directory") {
		t.Errorf("a part only a dotted directory holds is listed: %v", does)
	}
	seen := byDoes(t, v, "seen")
	if len(seen.Opened) != 1 || seen.Opened[0].File != ".kit/reached.yaml" || len(seen.Opened[0].Starts) != 1 || seen.Opened[0].Starts[0].File != "uses.yaml" {
		t.Errorf("an agent in a dotted directory that a machine names is %+v", seen.Opened)
	}
}

func Test_machines_pages_only_read(t *testing.T) {
	ctl, s := lab(t)
	h, dir := machines(t, Config{Controller: ctl, Store: s})
	d, _ := newDir(dir)
	before := d.stamp()
	for _, path := range []string{"/machines", "/machines/save", "/machines/new", "/machines/start", "/machines/check", "/machines/secret-set", "/machines/raw", "/parts"} {
		form := url.Values{"file": {"review.yaml"}, "text": {"flow: x\n"}, "type": {"flow"}, "name": {"x"}, "value": {"v"}}
		if w := do(h, "POST", path, form); w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s answered %d", path, w.Code)
		}
	}
	for _, path := range []string{"/machines/test?file=review.yaml", "/machines/raw?file=review.yaml"} {
		if w := do(h, "GET", path, nil); w.Code != http.StatusNotFound {
			t.Errorf("GET %s answered %d", path, w.Code)
		}
	}
	for _, path := range []string{"/machines?file=review.yaml", "/machines?file=review.yaml&view=text", "/parts"} {
		body := do(h, "GET", path, nil).Body.String()
		for _, control := range []string{"<form", "<textarea", "<input", "<button"} {
			if strings.Contains(body, control) {
				t.Errorf("%s shows %s:\n%s", path, control, body)
			}
		}
	}
	if d.stamp() != before {
		t.Errorf("the directory changed")
	}
}
