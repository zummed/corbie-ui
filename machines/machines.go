// Package machines is the Machines page: the definitions in one directory,
// listed by what names what, each drawn, shown as text and checked as it is
// written, and for a client edited, tested in the UI's process and started
// on the store. The Parts page shows the same directory from the tools up.
package machines

import (
	"bytes"
	"context"
	"embed"
	"encoding/json/v2"
	"errors"
	"html/template"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/zummed/corbie-ui/drawing"
	"github.com/zummed/corbie-ui/page"
	"github.com/zummed/corbie/identity"
	"github.com/zummed/corbie/machine"
	"github.com/zummed/corbie/secret"
	"github.com/zummed/corbie/store"
)

// Config is what the Machines page is given: the directory of machines,
// whether the UI acts as a client, which edits, tests and starts, the
// controller when the UI acts as the controller, the store, which is read
// for the controller's secrets only then, how a command runs, how often the
// directory is read again, and the shell every page shares.
type Config struct {
	Dir        string
	Edits      bool
	Controller *identity.Identity
	Store      store.Store
	Command    page.Command
	Interval   time.Duration
	Shell      page.Shell
}

type handler struct {
	Config
	dir *dir
}

//go:embed machines.html parts.html machines.js
var files embed.FS

var pages = template.Must(template.New("").Funcs(template.FuncMap{
	"last": func(i, n int) bool { return i == n-1 },
	"base": path.Base,
}).ParseFS(files, "machines.html", "parts.html"))

// New gives the Machines page.
func New(c Config) (http.Handler, error) {
	d, err := newDir(c.Dir)
	if err != nil {
		return nil, err
	}
	h := &handler{Config: c, dir: d}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /machines", h.show)
	mux.HandleFunc("GET /machines/stream", h.stream)
	mux.HandleFunc("GET /machines/raw", h.raw)
	mux.HandleFunc("GET /parts", h.showParts)
	mux.HandleFunc("GET /parts/stream", h.streamParts)
	mux.HandleFunc("POST /machines/check", h.checkText)
	if c.Edits {
		mux.HandleFunc("POST /machines/save", h.save)
		mux.HandleFunc("POST /machines/new", h.create)
		mux.HandleFunc("GET /machines/test", h.test)
		mux.HandleFunc("POST /machines/start", h.start)
	}
	if c.Controller != nil {
		mux.HandleFunc("POST /machines/secret-set", h.setSecret)
	}
	mux.HandleFunc("GET /machines/script.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		data, _ := files.ReadFile("machines.js")
		w.Write(data)
	})
	return mux, nil
}

// view is the Machines page as it is drawn.
type view struct {
	Dir      string
	Entries  []entry
	Open     string
	First    string
	Text     string
	Base     string
	Kind     string
	Check    checkView
	Types    []string
	Edits    bool
	ShowText bool
	Drawing  template.HTML
	Unread   string
}

// checkView is what is known of a definition as it is written: its
// problems, its drawing, the secrets it names, and the fields that can be
// written at the cursor. Controller says whether the UI acts as the
// controller, whose secrets give each secret's state.
type checkView struct {
	draft      *machine.Draft
	Problems   []problem     `json:"problems"`
	Drawing    template.HTML `json:"drawing"`
	Secrets    []secretView  `json:"secrets"`
	Controller bool          `json:"controller"`
	What       string        `json:"what"`
	Fields     []fieldView   `json:"fields"`
}

// A secretView is a secret the definition names, and its state on the
// controller: set, not set, or nothing when the UI does not act as the
// controller.
type secretView struct {
	Name, State string
}

func (h *handler) checkOf(ctx context.Context, rel string, src []byte, line int) (checkView, error) {
	kind, _ := firstLine(src)
	if kind == "" {
		return checkView{}, nil
	}
	c := h.dir.check(rel, src)
	v := checkView{draft: c.draft, Problems: c.problems, Controller: h.Controller != nil}
	if c.draft != nil {
		v.Drawing, _ = drawing.Draw(c.draft.Definition, nil)
	}
	v.What, v.Fields = fieldsAt(src, kind, line)
	for _, name := range secretsNamed(src) {
		v.Secrets = append(v.Secrets, secretView{Name: name})
	}
	if h.Controller != nil && len(v.Secrets) > 0 {
		set, err := secret.List(ctx, h.Store, h.Controller.Name)
		if err != nil {
			return v, err
		}
		for i := range v.Secrets {
			v.Secrets[i].State = "not set"
			if slices.Contains(set, v.Secrets[i].Name) {
				v.Secrets[i].State = "set"
			}
		}
	}
	return v, nil
}

func (h *handler) show(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	v := view{Dir: shortly(h.Dir), Open: q.Get("file"), Types: machine.Types(), Edits: h.Edits, ShowText: q.Get("view") == "text"}
	if v.Open != "" {
		v.Open = path.Clean(v.Open)
	}
	v.listed(h.dir)
	if v.Open == "" && len(v.Entries) > 0 {
		v.Open = v.Entries[0].Path
		v.listed(h.dir)
	}
	if v.Open != "" {
		data, base, err := h.dir.read(v.Open)
		if err != nil {
			page.Error(w, http.StatusNotFound, err.Error())
			return
		}
		v.Text, v.Base = string(data), base
		var name string
		v.Kind, name = firstLine(data)
		v.First = v.Kind + ": " + name
		v.ShowText = v.ShowText || v.Kind == ""
		if v.Check, err = h.checkOf(r.Context(), v.Open, data, 1); err != nil {
			page.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !v.ShowText && v.Check.draft != nil {
			open := map[string]bool{}
			for _, at := range q["open"] {
				open[at] = true
			}
			u := drawing.Unfold{Open: open, Href: hrefs(v.Open, open), Part: h.dir.partFile}
			if v.Drawing, err = drawing.Unfolded(v.Check.draft, v.Open, u); err != nil {
				page.Error(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}
	main, err := render("machines", v)
	if err != nil {
		page.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	s := h.Shell
	s.Title, s.Current, s.Stream, s.Script, s.Main = "Machines", "Machines", "/machines/stream?file="+url.QueryEscape(v.Open), "/machines/script.js", main
	page.Write(w, s)
}

// listed reads the list, and notes what could not be read.
func (v *view) listed(d *dir) {
	var err error
	v.Entries, err = d.list(v.Open)
	v.Unread = ""
	if err != nil {
		v.Unread = err.Error()
	}
}

// hrefs gives the address of a box's link: the page with the box unfolded,
// or, when it is, with it and what is inside it folded.
func hrefs(file string, open map[string]bool) func(string) string {
	return func(at string) string {
		q := url.Values{"file": {file}}
		for _, p := range slices.Sorted(maps.Keys(open)) {
			if !open[at] || (p != at && !strings.HasPrefix(p, at+"/")) {
				q.Add("open", p)
			}
		}
		if !open[at] {
			q.Add("open", at)
		}
		return "/machines?" + q.Encode()
	}
}

// stream sends the page the list as the directory now holds it, and the
// hash of the open file, by which the page reads a file that changed on
// disk.
func (h *handler) stream(w http.ResponseWriter, r *http.Request) {
	open := r.URL.Query().Get("file")
	never := make(chan struct{})
	page.Stream(w, r, h.Interval, func() <-chan struct{} { return never }, func(context.Context) ([]page.Part, error) {
		v := view{Open: open}
		v.listed(h.dir)
		_, v.Base, _ = h.dir.read(open)
		var parts []page.Part
		for _, id := range []string{"files", "disk"} {
			html, err := render(id, v)
			if err != nil {
				return nil, err
			}
			parts = append(parts, page.Part{ID: id, HTML: html})
		}
		return parts, nil
	})
}

// shortly writes a directory as a person types it: under the home
// directory, from ~.
func shortly(dir string) string {
	if home, err := os.UserHomeDir(); err == nil && (dir == home || strings.HasPrefix(dir, home+string(filepath.Separator))) {
		return "~" + strings.TrimPrefix(dir, home)
	}
	return dir
}

func answer(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.MarshalWrite(w, v)
}

func (h *handler) raw(w http.ResponseWriter, r *http.Request) {
	data, base, err := h.dir.read(r.URL.Query().Get("file"))
	if err != nil {
		page.Error(w, http.StatusNotFound, err.Error())
		return
	}
	answer(w, http.StatusOK, map[string]string{"text": string(data), "base": base})
}

func (h *handler) checkText(w http.ResponseWriter, r *http.Request) {
	rel := r.FormValue("file")
	if _, err := h.dir.inside(rel); err != nil {
		page.Error(w, http.StatusForbidden, err.Error())
		return
	}
	line := 1
	if n, err := jsonInt(r.FormValue("line")); err == nil {
		line = n
	}
	v, err := h.checkOf(r.Context(), rel, []byte(r.FormValue("text")), line)
	if err != nil {
		page.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	drawn := map[string]any{"drawing": v.Drawing, "count": len(v.Problems)}
	for _, id := range []string{"problems", "secrets", "fields"} {
		if drawn[id], err = render(id, v); err != nil {
			page.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	lines := []int{}
	for _, p := range v.Problems {
		if p.Line > 0 {
			lines = append(lines, p.Line)
		}
	}
	drawn["lines"] = lines
	answer(w, http.StatusOK, drawn)
}

// setSecret runs the Controller page's Set: corbie controller secret set
// NAME -, with the value on its standard input, which "-" names to the
// command.
func (h *handler) setSecret(w http.ResponseWriter, r *http.Request) {
	call := page.Call{Words: []string{"controller", "secret", "set"}, Args: []string{r.FormValue("name")}, Flags: []string{"-"}, Stdin: r.FormValue("value")}
	a := h.Command(r.Context(), call)
	if a.Status != 0 {
		page.Error(w, http.StatusConflict, a.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, "set")
}

func jsonInt(s string) (int, error) {
	var n int
	err := json.Unmarshal([]byte(s), &n)
	return n, err
}

// save writes the text only if the file is as it was read; when it is not,
// it answers with the file as it is now.
func (h *handler) save(w http.ResponseWriter, r *http.Request) {
	base, now, err := h.dir.save(r.FormValue("file"), []byte(r.FormValue("text")), r.FormValue("base"))
	switch {
	case errors.Is(err, errChanged):
		answer(w, http.StatusConflict, map[string]string{"error": err.Error(), "text": string(now), "base": hash(now)})
	case err != nil:
		answer(w, http.StatusForbidden, map[string]string{"error": err.Error()})
	default:
		answer(w, http.StatusOK, map[string]string{"base": base})
	}
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	rel, err := h.dir.create(r.FormValue("type"), strings.TrimSpace(r.FormValue("name")))
	if err != nil {
		page.Error(w, http.StatusConflict, err.Error())
		return
	}
	http.Redirect(w, r, "/machines?file="+rel, http.StatusSeeOther)
}

// startFlags are the flags Test and Start take from the page: the input
// as JSON the UI has read, and run secrets by name, each read from the
// UI's environment. Start also takes labels, the permissions that narrow
// the run, and a timeout.
func startFlags(form func(string) string, start bool) ([]string, error) {
	var flags []string
	if text := strings.TrimSpace(form("input")); text != "" {
		var v any
		if err := json.Unmarshal([]byte(text), &v); err != nil {
			return nil, errors.New("the input is not JSON: " + err.Error())
		}
		input, _ := json.Marshal(v, json.Deterministic(true))
		flags = append(flags, "--input="+string(input))
	}
	for _, name := range strings.Fields(strings.ReplaceAll(form("secrets"), ",", " ")) {
		flags = append(flags, "--secret-env="+name)
	}
	if !start {
		return flags, nil
	}
	for _, label := range strings.Fields(form("labels")) {
		flags = append(flags, "--label="+label)
	}
	for _, p := range strings.Fields(strings.ReplaceAll(form("allow"), ",", " ")) {
		flags = append(flags, "--allow="+p)
	}
	for _, p := range strings.Fields(strings.ReplaceAll(form("deny"), ",", " ")) {
		flags = append(flags, "--deny="+p)
	}
	if t := strings.TrimSpace(form("timeout")); t != "" {
		flags = append(flags, "--timeout="+t)
	}
	return flags, nil
}

// test runs `corbie test <file> --watch` in the UI's process, and sends
// the page each line it prints, as it prints it. The test stops when its
// page closes.
func (h *handler) test(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	full, err := h.dir.inside(q.Get("file"))
	if err != nil {
		page.Error(w, http.StatusForbidden, err.Error())
		return
	}
	flags, err := startFlags(q.Get, false)
	if err != nil {
		page.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	ctx := conn.CloseRead(r.Context())
	lines := &lineWriter{send: func(line string) { conn.Write(ctx, websocket.MessageText, []byte(line)) }}
	a := h.Command(ctx, page.Call{Words: []string{"test"}, Args: []string{full}, Flags: append(flags, "--watch"), Lines: lines})
	lines.Flush()
	if said := strings.TrimSpace(a.Said); said != "" {
		conn.Write(ctx, websocket.MessageText, []byte(said))
	}
	conn.Close(websocket.StatusNormalClosure, "")
}

// lineWriter sends each whole line written to it.
type lineWriter struct {
	buf  bytes.Buffer
	send func(string)
}

func (l *lineWriter) Write(p []byte) (int, error) {
	l.buf.Write(p)
	for {
		line, err := l.buf.ReadString('\n')
		if err != nil {
			l.buf.WriteString(line)
			return len(p), nil
		}
		l.send(strings.TrimSuffix(line, "\n"))
	}
}

func (l *lineWriter) Flush() {
	if rest := l.buf.String(); rest != "" {
		l.send(rest)
		l.buf.Reset()
	}
}

// start runs `corbie run submit <file>`, and answers with the address of
// the run's page.
func (h *handler) start(w http.ResponseWriter, r *http.Request) {
	full, err := h.dir.inside(r.FormValue("file"))
	if err != nil {
		page.Error(w, http.StatusForbidden, err.Error())
		return
	}
	flags, err := startFlags(r.FormValue, true)
	if err != nil {
		page.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	a := h.Command(r.Context(), page.Call{Words: []string{"run", "submit"}, Args: []string{full}, Flags: flags})
	if a.Status != 0 {
		page.Error(w, http.StatusConflict, a.Error())
		return
	}
	var started struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(a.Out)), &started); err != nil || started.ID == "" {
		page.Error(w, http.StatusInternalServerError, "corbie run submit printed no run: "+a.Out)
		return
	}
	answer(w, http.StatusOK, map[string]string{"href": "/run/" + started.ID})
}

func render(name string, data any) (template.HTML, error) {
	var b bytes.Buffer
	err := pages.ExecuteTemplate(&b, name, data)
	return template.HTML(b.String()), err
}
