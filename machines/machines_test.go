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
	"github.com/zummed/corbie/secret"
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
      message: { send: { to: { at: .kit/chat, key: "a" }, event: said } }
      done: talking
  talking:
    job: { machine: ./chat.yaml }
    on: { done: hearing }
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
    job: { script: ./write.js, input: { hub: "${values.HUB}" }, allow: [net:y, secret:B@net:y, net:z, "secret:*@net:z"] }
`

// later writes its own allow: after its stages.
const later = `flow: later
stages:
  ship:
    job: { script: ./write.js, allow: [secret:B@net:y] }
allow: [secret:A@net:x]
`

// listed gives the list as directory, file, first line, lifetime and
// problem count.
func listed(ix *index) []string {
	var out []string
	for _, g := range ix.Groups {
		for _, e := range g.Entries {
			out = append(out, strings.Join(slices.DeleteFunc([]string{g.Dir, e.Path, e.First, e.Lifetime, fmt.Sprint(e.Problems)}, func(f string) bool { return f == "" }), " "))
		}
	}
	return out
}

func Test_machines_list_shows_what_each_item_is(t *testing.T) {
	dir := project(t)
	write(t, dir, map[string]string{
		"bridges/b.yaml":     "flow: b\nlifetime: singleton\nstages:\n  a: { job: { machine: .kit/say.yaml } }\n",
		"bad.yaml":           "flow: bad\nstages: {}\ncolour: blue\n",
		"keyed.yaml":         "statechart: keyed\nlifetime: keyed\ninclude: ./shared/states.yaml\n",
		"shared/states.yaml": "initial: a\nstates:\n  a: { on: { go: b } }\n  b: { end: done }\n",
		"prompt.md":          "Be brief.\n",
		"talker.yaml":        "agent: talker\nmodel: { name: claude-sonnet-5-5, secret: KEY }\nprompt: hello\nsystem: { include: ./prompt.md }\n",
	})
	d, _ := newDir(dir)
	ix := d.index()
	got := listed(ix)
	want := []string{
		". bad.yaml flow: bad 2", ". helper.yaml agent: helper transient 0", ". keyed.yaml statechart: keyed keyed 0", ". later.yaml flow: later transient 0",
		". talker.yaml agent: talker transient 0", ". telegram.yaml flow: bridge transient 0",
		".kit .kit/chat.yaml statechart: chat transient 0", ".kit .kit/listen.yaml statechart: listen transient 0", ".kit .kit/say.yaml flow: say transient 0", ".kit .kit/unused.yaml flow: unused transient 0",
		"bridges bridges/b.yaml flow: b singleton 0",
	}
	if !slices.Equal(got, want) {
		t.Errorf("the list is\n%s\nand not\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	var pieces []string
	for _, p := range ix.Pieces {
		pieces = append(pieces, fmt.Sprintf("%s %d", p.Path, p.Included))
	}
	if want := []string{"parts/part.yaml 1", "people.yaml 0", "prompt.md 1", "shared/states.yaml 1"}; !slices.Equal(pieces, want) {
		t.Errorf("the pieces are %v, and not %v", pieces, want)
	}
	if ix.Prefix != "/"+filepath.Base(dir) || ix.Groups[len(ix.Groups)-1].Entries[0].Location != ix.Prefix+"/bridges/b" {
		t.Errorf("the prefix is %q and the locations %+v", ix.Prefix, ix.Groups)
	}
	h, _ := New(Config{Dir: dir, Interval: time.Second})
	body := do(h, "GET", "/machines?file=bridges/b.yaml", nil).Body.String()
	_, list, _ := strings.Cut(body, `id="files"`)
	list, _, _ = strings.Cut(list, "</nav>")
	list, piecesList, _ := strings.Cut(list, `<h2 class="kind">Pieces</h2>`)
	for _, want := range []string{`<h2 class="group" title="bridges">bridges/</h2>`, `<h2 class="group" title=".kit">.kit/</h2>`, `<use href="#g-agent"/>`, "<small>agent: helper · transient</small>", "<small>flow: b · singleton</small>", `title="` + ix.Prefix + `/bridges/b">b.yaml<`, `<span class="bad" title="2 problems">2</span>`, `href="/machines?file=bridges%2fb.yaml" aria-current="page"`} {
		if !strings.Contains(list, want) {
			t.Errorf("the list does not show %s:\n%s", want, list)
		}
	}
	for _, gone := range []string{"people.yaml", "part.yaml"} {
		if strings.Contains(list, gone) {
			t.Errorf("the machines hold %s:\n%s", gone, list)
		}
	}
	for _, want := range []string{">parts/part.yaml</span><small>included by 1 machine</small>", ">people.yaml</span><small>included by none</small>"} {
		if !strings.Contains(piecesList, want) {
			t.Errorf("the pieces do not show %s:\n%s", want, piecesList)
		}
	}
	if !strings.Contains(body, ">at "+ix.Prefix+"<") {
		t.Errorf("the page does not give the prefix:\n%s", body)
	}
	piece := do(h, "GET", "/machines?file=shared/states.yaml", nil).Body.String()
	if !strings.Contains(piece, `<section class="included"><h2>Included by</h2><a href="/machines?file=keyed.yaml">keyed.yaml</a></section>`) || !strings.Contains(piece, ">a piece<") || strings.Contains(piece, ">Drawing<") {
		t.Errorf("a piece does not show the machines that include it:\n%s", piece)
	}
	script := do(h, "GET", "/machines?file=.kit/hear.js", nil).Body.String()
	if !strings.Contains(script, ">a file<") || strings.Contains(script, ">a piece<") || strings.Contains(script, "Included by") {
		t.Errorf("a script no machine takes a value from is shown as a piece:\n%s", script)
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

func Test_machines_list_is_empty_when_a_location_has_one_below_it(t *testing.T) {
	dir := project(t)
	write(t, dir, map[string]string{"helper/x.yaml": "flow: x\nstages:\n  a: { job: ./x.js }\n"})
	d, _ := newDir(dir)
	ix := d.index()
	if !strings.Contains(ix.Unread, "helper.yaml is beside a directory of the same name") || len(ix.Groups) != 0 || len(ix.Pieces) != 0 || len(ix.parts) != 0 {
		t.Errorf("a tree whose files cannot all have locations lists %+v, %+v, %d parts, and says %q", ix.Groups, ix.Pieces, len(ix.parts), ix.Unread)
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
			if !slices.Equal(p.Breaks, []string{".kit/c.yaml", "a.yaml", "b.yaml"}) || !strings.HasPrefix(p.Place, "tools.tell") || p.Line == 0 {
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
	h, _ := New(Config{Dir: dir, Interval: time.Second})
	body := do(h, "GET", "/machines?file=z.yaml", nil).Body.String()
	if n := strings.Count(body, "breaks 3 machines"); n != atPart {
		t.Errorf("the page shows the part's problems %d times, and not %d:\n%s", n, atPart, body)
	}
	// The Parts page lists the same problems, drawn the same way.
	parts := do(h, "GET", "/parts", nil).Body.String()
	box := func(page string) string {
		start := strings.Index(page, `<div class="problems">`)
		if start < 0 {
			return ""
		}
		return page[start : start+strings.Index(page[start:], "</div>")]
	}
	if box(parts) == "" || box(parts) != box(do(h, "GET", "/machines", nil).Body.String()) {
		t.Errorf("the Parts page lists the problems %q, and not as the Machines page does", box(parts))
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
		if got := listed(ix); slices.ContainsFunc(got, func(l string) bool { return strings.Contains(l, " base.yaml ") }) {
			t.Errorf("with a %s file included, the list is %v", name, got)
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
	if body := do(h, "GET", "/machines", nil).Body.String(); !strings.Contains(body, "<code>proj/</code> is the top of a tree") || !strings.Contains(body, "breaks 2 machines") {
		t.Errorf("the page does not say which directory is the top:\n%s", body)
	}
	if body := do(h, "GET", "/machines?file=proj/a.yaml", nil).Body.String(); strings.Count(body, "is the top of a tree") != 2 || strings.Contains(body, "cannot be read") {
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
	if strings.Contains(body, `id="drawing"`) || !strings.Contains(body, "missing.js") || !strings.Contains(body, `<span class="l err" id="L4"><span class="k">  a</span>`) && !strings.Contains(body, `<span class="l err" id="L4">  <span class="k">a</span>`) {
		t.Errorf("a machine with problems is not shown as text with its line marked:\n%s", body)
	}
	if !strings.Contains(body, `<span class="l" id="L1"><span class="c"># one stage</span></span>`) {
		t.Errorf("a comment is not set apart:\n%s", body)
	}
	part := do(h, "GET", "/machines?file=parts/m.yaml&view=text", nil).Body.String()
	if !strings.Contains(part, "tools.r.colour") || !strings.Contains(part, "breaks 1 machine") || strings.Count(part, `class="l err"`) != 1 {
		t.Errorf("a part's text does not show the problem found in it:\n%s", part)
	}
	if a := do(h, "GET", "/machines?file=a.yaml", nil).Body.String(); !strings.Contains(a, "tools.r.colour") || !strings.Contains(a, "breaks 1 machine") || strings.Contains(a, `class="l err"`) {
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
	if !strings.Contains(body, ">hearing<") || !strings.Contains(body, ">.kit/chat#a<") {
		t.Fatalf("the machine is not drawn:\n%s", body)
	}
	if regexp.MustCompile(`<a href="(/machines\?[^"]*)"><g class="[^"]*sends`).MatchString(body) {
		t.Errorf("a send, which starts no child, unfolds:\n%s", body)
	}
	link := regexp.MustCompile(`<a href="(/machines\?[^"]*)"><g class="[^"]*"><rect[^>]*/><text[^>]*>talking<`).FindStringSubmatch(body)
	if link == nil {
		t.Fatalf("a job that names a machine has no link:\n%s", body)
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
	folded := regexp.MustCompile(`<a href="(/machines\?[^"]*)"><g class="[^"]*unfolded[^"]*"><rect[^>]*/><text[^>]*>talking<`).FindStringSubmatch(deeper)
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

func Test_machines_secrets_and_values_used_are_listed(t *testing.T) {
	section := regexp.MustCompile(`(?s)<section class="uses">.*?</section>`)
	ctl, s := lab(t)
	h, dir := machines(t, Config{Controller: ctl, Store: s})
	prefix := "/" + filepath.Base(dir)
	write(t, dir, map[string]string{
		"shipper.yaml": shipper,
		"later.yaml":   later,
		"shared.yaml":  "allow: [net:z, secret:C@net:z]\n",
		"inc.yaml":     "flow: inc\nallow: [{ include: ./shared.yaml#allow }]\nstages:\n  a: { job: { script: ./write.js, allow: [secret:C@net:z] } }\n",
	})
	ctx := context.Background()
	if err := secret.SetValue(ctx, s, ctl, prefix, "HUB", "hub.lan"); err != nil {
		t.Fatal(err)
	}
	if err := secret.Set(ctx, s, ctl, "/elsewhere", "B", "kept"); err != nil {
		t.Fatal(err)
	}
	got := section.FindString(do(h, "GET", "/machines?view=text&file=shipper.yaml", nil).Body.String())
	for _, want := range []string{
		`<span>A</span><span class="dim">secret · set at /</span>`,
		`<span>B</span><span class="dim">secret · not set at ` + prefix + `/shipper or above</span>`,
		`<span>HUB</span><span class="dim">value · set at ` + prefix + `</span>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("as the controller, the secrets and values do not show %s:\n%s", want, got)
		}
	}
	if strings.Count(got, "<span>") != 3 || strings.Contains(got, "corbie controller") {
		t.Errorf("as the controller, the secrets and values are shown as\n%s", got)
	}
	if strings.Contains(got, "<form") || strings.Contains(got, "<button") || strings.Contains(got, "<input") || strings.Contains(got, "hub.lan") {
		t.Errorf("the secrets can be set, or a value read, from the Machines page:\n%s", got)
	}
	if got := section.FindString(do(h, "GET", "/machines?file=shipper.yaml", nil).Body.String()); !strings.Contains(got, ">A<") {
		t.Errorf("the drawing does not list the secrets:\n%s", got)
	}
	if got := section.FindString(do(h, "GET", "/machines?file=inc.yaml", nil).Body.String()); !strings.Contains(got, `<span>C</span>`) {
		t.Errorf("a secret an included file names is not listed:\n%s", got)
	}

	h, _ = machines(t, Config{})
	got = section.FindString(do(h, "GET", "/machines?view=text&file=review.yaml", nil).Body.String())
	if got != "" {
		t.Errorf("a machine that uses no secret or value shows %s", got)
	}
	h, dir = machines(t, Config{})
	os.WriteFile(filepath.Join(dir, "shipper.yaml"), []byte(shipper), 0o644)
	got = section.FindString(do(h, "GET", "/machines?file=shipper.yaml", nil).Body.String())
	if !strings.Contains(got, `<span>A</span><span class="dim">secret</span>`) || !strings.Contains(got, `<span>HUB</span><span class="dim">value</span>`) || !strings.Contains(got, "corbie controller secret set NAME --at /"+filepath.Base(dir)+"/shipper") {
		t.Errorf("as a client alone, the secrets and values are shown as\n%s", got)
	}
}

func Test_machines_a_machine_shows_its_location(t *testing.T) {
	h, dir := machines(t, Config{})
	prefix := "/" + filepath.Base(dir)
	write(t, dir, map[string]string{
		"devices/boiler.yaml":   "statechart: boiler\nlifetime: singleton\nreceive: { set: [schedules/*, client:panel], stop: [client:panel] }\ninitial: idle\nstates:\n  idle: { on: { set: idle } }\n",
		"rooms/thermostat.yaml": "statechart: thermostat\nlifetime: keyed\nlimits: { runs: 3 }\nupdate: restart\nallow: [send:devices/boiler, send:/work/log]\ninitial: on\nstates:\n  on: { on: { set: on } }\n",
		"services/weather.yaml": "flow: weather\nreceive: [rooms/*]\nstages:\n  a: { job: write.js }\n",
	})
	place := regexp.MustCompile(`(?s)<section class="place">.*?</section>`)
	rows := func(file string) string {
		got := place.FindString(do(h, "GET", "/machines?file="+file, nil).Body.String())
		return regexp.MustCompile(`\s*\n\s*`).ReplaceAllString(got, "")
	}
	for file, want := range map[string][]string{
		"devices/boiler.yaml": {
			"<span>location</span><span>" + prefix + "/devices/boiler</span>",
			"<span>lifetime</span><span>singleton: the submit starts its one run</span>",
			"<span>update</span><span>restart: a changed file cancels its runs</span>",
			"<span>receives</span><span>set from schedules/*, client:panel<br>stop from client:panel<br>and anything from its tree's client</span>",
			"<span>sends to</span><span>no location: its <code>allow:</code> has no <code>send:</code> entry</span>",
		},
		"rooms/thermostat.yaml": {
			"<span>lifetime</span><span>keyed: a send to a key starts the run for that key, at most 3 running at once</span>",
			"<span>receives</span><span>from its own tree and its tree's client</span>",
			"<span>sends to</span><span>devices/boiler, /work/log</span>",
		},
		"services/weather.yaml": {
			"<span>lifetime</span><span>transient: a call, or corbie run submit " + prefix + "/services/weather, starts each run</span>",
			"<span>update</span><span>drain: a changed file leaves its runs to finish on the definition they started with</span>",
			"<span>receives</span><span>every event from rooms/*<br>and anything from its tree's client</span>",
		},
	} {
		got := rows(file)
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("%s does not show %s:\n%s", file, w, got)
			}
		}
	}
	if got := place.FindString(do(h, "GET", "/machines?file=notes.yaml", nil).Body.String()); got != "" {
		t.Errorf("a piece has a location:\n%s", got)
	}
}

func Test_machines_written_out_shows_where_each_value_is_from(t *testing.T) {
	h, dir := machines(t, Config{})
	write(t, dir, map[string]string{
		"family.yaml": "agent: family\ninclude: ./base.yaml\nmodel: { effort: high }\nallow:\n  - { include: ./base.yaml#allow }\n  - send:devices/*\n",
		"base.yaml":   "model: { name: claude-sonnet-5-5, secret: ANTHROPIC_KEY }\nprompt: ${input.said}\nallow: [\"fs.read:${input.dir}/**\"]\n",
	})
	body := do(h, "GET", "/machines?file=family.yaml&view=written", nil).Body.String()
	if !strings.Contains(body, `href="/machines?file=family.yaml&amp;view=written" aria-current="page">Written out<`) {
		t.Errorf("the Written out tab is not chosen:\n%s", body)
	}
	_, code, _ := strings.Cut(body, `<pre class="code written"`)
	for _, want := range []string{
		`<span class="k">name</span>: claude-sonnet-5-5`,
		`<a class="c" href="/machines?file=base.yaml&amp;view=text#L1"># base.yaml:1</a>`,
		`<span class="k">effort</span>: high`,
		`<a class="c" href="/machines?file=family.yaml&amp;view=text#L3"># family.yaml:3</a>`,
		`<a class="c" href="/machines?file=base.yaml&amp;view=text#L3"># base.yaml:3</a>`,
		`<a class="c" href="/machines?file=family.yaml&amp;view=text#L6"># family.yaml:6</a>`,
	} {
		if !strings.Contains(code, want) {
			t.Errorf("the definition written out does not hold %s:\n%s", want, code)
		}
	}
	if strings.Contains(code, "include") {
		t.Errorf("the definition written out holds an include:\n%s", code)
	}
	if text := do(h, "GET", "/machines?file=base.yaml&view=text", nil).Body.String(); !strings.Contains(text, `<span class="l" id="L3">`) {
		t.Errorf("a line of a file has no anchor to link to:\n%s", text)
	}
	if body := do(h, "GET", "/machines?file=notes.yaml&view=written", nil).Body.String(); strings.Contains(body, "Written out") || !strings.Contains(body, `<span class="k">a</span>`) {
		t.Errorf("a piece is written out:\n%s", body)
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
	if next == first || !slices.Contains(listed(next), ". new.yaml flow: new transient 0") {
		t.Errorf("a file written is not read: %v", listed(next))
	}
	write(t, dir, map[string]string{"parts/part.yaml": "does: changed\ntools: {}\n"})
	if c.current() == next {
		t.Errorf("a change to a file that is not a machine is not read")
	}
}

func Test_machines_dotted_files_are_read_as_any_other(t *testing.T) {
	dir := project(t)
	write(t, dir, map[string]string{
		".kit/alone.yaml":   "agent: alone\n" + agentHead + "open:\n  hidden: { does: hidden, tools: { r: { does: r, job: { script: ./hear.js } } } }\n",
		".kit/broken.yaml":  "flow: broken\nstages: {}\ncolour: blue\n",
		".kit/reached.yaml": "agent: reached\n" + agentHead + "open:\n  seen: { does: seen, tools: { r: { does: r, job: { script: ./hear.js } } } }\n",
		"uses.yaml":         "flow: uses\nstages:\n  a: { job: { machine: .kit/reached.yaml } }\n",
	})
	d, _ := newDir(dir)
	ix := d.index()
	prefix := "/" + filepath.Base(dir)
	for _, want := range []string{".kit .kit/alone.yaml agent: alone transient 0", ".kit .kit/broken.yaml flow: broken 2"} {
		if !slices.Contains(listed(ix), want) {
			t.Errorf("the list does not hold %s: %v", want, listed(ix))
		}
	}
	if m := ix.machines[".kit/alone.yaml"]; m == nil || m.Location != prefix+"/.kit/alone" {
		t.Errorf("a machine in a dotted directory is %+v", m)
	}
	if !slices.ContainsFunc(ix.Problems, func(p problem) bool { return p.File == ".kit/broken.yaml" }) {
		t.Errorf("a machine in a dotted directory gives no problems: %+v", ix.Problems)
	}
	v := ix.partsView("")
	hidden := byDoes(t, v, "hidden")
	if len(hidden.Opened) != 1 || hidden.Opened[0].File != ".kit/alone.yaml" || len(hidden.Opened[0].Starts) != 0 || hidden.Opened[0].Lifetime != "transient" {
		t.Errorf("an agent in a dotted directory that nothing names is %+v", hidden.Opened)
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
