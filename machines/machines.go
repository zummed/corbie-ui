// Package machines is the Machines page: the machines in one directory, each
// drawn and shown as text, with the problems Corbie finds in them. The Parts
// page shows the same directory from the tools up. Both only read.
package machines

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/zummed/corbie-ui/controller"
	"github.com/zummed/corbie-ui/drawing"
	"github.com/zummed/corbie-ui/page"
	"github.com/zummed/corbie/client"
	"github.com/zummed/corbie/identity"
	"github.com/zummed/corbie/machine"
	"github.com/zummed/corbie/secret"
	"github.com/zummed/corbie/store"
)

// Config is what the Machines page is given: the directory of machines, the
// controller and its store when the UI acts as the controller, which give
// where each secret and value is set, how often the page looks for a
// change, and the shell every page shares.
type Config struct {
	Dir        string
	Controller *identity.Identity
	Store      store.Store
	Interval   time.Duration
	Shell      page.Shell
}

type handler struct {
	Config
	dir   *dir
	index *indexed
}

//go:embed machines.html parts.html machines.js
var files embed.FS

var pages = template.Must(template.New("").Funcs(template.FuncMap{
	"base":   path.Base,
	"plural": plural,
	"short":  short,
	"join":   func(s []string) string { return strings.Join(s, ", ") },
}).ParseFS(files, "machines.html", "parts.html"))

// New gives the Machines and Parts pages.
func New(c Config) (http.Handler, error) {
	d, err := newDir(c.Dir)
	if err != nil {
		return nil, err
	}
	h := &handler{Config: c, dir: d, index: &indexed{dir: d}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /machines", h.show)
	mux.HandleFunc("GET /machines/stream", h.stream)
	mux.HandleFunc("GET /parts", h.showParts)
	mux.HandleFunc("GET /parts/stream", h.streamParts)
	mux.HandleFunc("GET /machines/script.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		data, _ := files.ReadFile("machines.js")
		w.Write(data)
	})
	return mux, nil
}

// view is the Machines page as it is drawn. View is the tab chosen:
// drawing, text or written.
type view struct {
	Dir, DirFull string
	*index
	Open       string
	Kind       string
	First      string
	Base       string
	View       string
	Drawable   bool
	Text       []codeLine
	Found      []problem
	Place      *place
	Uses       []use
	Piece      bool
	Included   []string
	Controller bool
	Drawing    template.HTML
}

// A place is where a machine lives and how its runs start, as its file
// says: its location, its lifetime, what a changed file does to its runs,
// how many may run at once, who may send to it and call it, and the
// locations it may send to and call.
type place struct {
	Location, Lifetime, Started, Update, Updated string
	Runs                                         int
	Receives, Sends                              []string
}

// A use is a secret or a value a machine uses, and, acting as the
// controller, where it is set for the machine's location.
type use struct {
	Name, Kind, State string
}

// tree gives the index of the directory, read at the prefix of the tree
// the controller recorded from it, where the UI acts as the controller and
// it recorded one; at the prefix a submit gives by default otherwise.
func (h *handler) tree(ctx context.Context) (*index, error) {
	if h.Controller == nil {
		return h.index.current(""), nil
	}
	trees, err := controller.Trees(ctx, h.Store, h.Controller)
	if err != nil {
		return nil, err
	}
	at := ""
	for _, t := range trees {
		if t.Directory == h.dir.real {
			at = t.Prefix
			break
		}
	}
	return h.index.current(at), nil
}

func (h *handler) show(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ix, err := h.tree(r.Context())
	if err != nil {
		page.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	v := view{Dir: short(shortly(h.Dir)), DirFull: shortly(h.Dir), index: ix, Open: q.Get("file"), View: q.Get("view"), Controller: h.Controller != nil}
	if v.Open == "" && len(ix.Groups) > 0 {
		v.Open = ix.Groups[0].Entries[0].Path
	}
	if v.Open != "" {
		v.Open = path.Clean(v.Open)
		src, err := h.opened(&v)
		if err != nil {
			page.Error(w, http.StatusNotFound, err.Error())
			return
		}
		m := ix.machines[v.Open]
		v.Drawable = m != nil && m.Draft != nil
		if v.Drawable {
			v.Place = placeOf(m)
			if v.Uses, err = h.usesOf(r.Context(), m); err != nil {
				page.Error(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		v.Piece = ix.piece(v.Open)
		v.Included = ix.included[v.Open]
		switch {
		case v.Drawable && v.View == "written":
			v.Text = writtenOut(m.Draft.WrittenOut())
		case v.Drawable && v.View != "text":
			v.View = "drawing"
			open := map[string]bool{}
			for _, at := range q["open"] {
				open[at] = true
			}
			u := drawing.Unfold{Open: open, Href: hrefs(v.Open, open)}
			if v.Drawing, err = drawing.Unfolded(m.Draft, u); err != nil {
				page.Error(w, http.StatusInternalServerError, err.Error())
				return
			}
		default:
			v.View, v.Text = "text", colour(src, v.Open, v.Found)
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

// opened reads the file chosen, its type and first line, the problems found in
// it, and the mark by which the page knows any of them changed.
func (h *handler) opened(v *view) ([]byte, error) {
	src, base, err := h.dir.read(v.Open)
	if err != nil {
		return nil, err
	}
	v.Found = v.index.at(v.Open)
	if m := v.index.machines[v.Open]; m != nil {
		if m.Draft != nil {
			base += m.Draft.Hash() + fmt.Sprint(m.Draft.Definition.Lines)
		}
		var name string
		v.Kind, name = firstLine(src)
		if m.Draft != nil {
			v.Kind, name = m.Draft.Definition.Type, m.Draft.Definition.Name
		}
		v.First = v.Kind + ": " + name
	}
	v.Base = hash(fmt.Appendf([]byte(base), "%v%v", v.Found, v.index.included[v.Open]))
	return src, nil
}

// placeOf gives where a machine lives and how its runs start.
func placeOf(m *client.Machine) *place {
	def := m.Draft.Definition
	p := &place{Location: m.Location, Lifetime: def.Lifetime, Update: def.Update, Runs: def.Runs}
	p.Started = startedBy(def.Lifetime, m.Location)
	p.Updated = "a changed file leaves its runs to finish on the definition they started with"
	if def.Update == machine.UpdateRestart {
		p.Updated = "a changed file cancels its runs"
	}
	for _, event := range sortedKeys(def.Receive) {
		from := strings.Join(def.Receive[event], ", ")
		if event == "" {
			p.Receives = append(p.Receives, "every event from "+from)
		} else {
			p.Receives = append(p.Receives, event+" from "+from)
		}
	}
	for _, entry := range def.Allow {
		if to, ok := strings.CutPrefix(entry, "send:"); ok {
			p.Sends = append(p.Sends, to)
		}
	}
	return p
}

// usesOf gives the secrets and the values a machine uses, as Corbie reads
// them from it written out, and, acting as the controller, where each is
// set: at the machine's location or the nearest location above it.
func (h *handler) usesOf(ctx context.Context, m *client.Machine) ([]use, error) {
	var out []use
	for _, name := range secretsOf(m.Draft) {
		out = append(out, use{Name: name, Kind: "secret"})
	}
	for _, name := range m.Draft.Values() {
		out = append(out, use{Name: name, Kind: "value"})
	}
	if h.Controller == nil || len(out) == 0 {
		return out, nil
	}
	secrets, err := secret.List(ctx, h.Store, h.Controller.Name)
	if err != nil {
		return nil, err
	}
	values, err := secret.ListValues(ctx, h.Store, h.Controller)
	if err != nil {
		return nil, err
	}
	for i, u := range out {
		set := secrets
		if u.Kind == "value" {
			set = values
		}
		out[i].State = "not set at " + m.Location + " or above"
		if at := nearest(set, u.Name, m.Location); at != "" {
			out[i].State = "set at " + at
		}
	}
	return out, nil
}

// secretsOf gives the names of the secrets a draft's jobs and its child
// definitions name, in order.
func secretsOf(d *machine.Draft) []string {
	all := map[string]bool{}
	var add func(d *machine.Draft)
	add = func(d *machine.Draft) {
		allow := slices.Clone(d.Definition.Allow)
		for _, job := range machine.Jobs(d.Definition) {
			allow = append(allow, job.Job.Allow...)
		}
		if d.Definition.Ask != nil {
			allow = append(allow, d.Definition.Ask.Allow...)
		}
		for _, name := range secret.Named(allow) {
			all[name] = true
		}
		for _, child := range d.Children {
			add(child)
		}
	}
	add(d)
	return sortedKeys(all)
}

// nearest gives the location nearest to a location, at it or above it,
// where a name is set, or nothing.
func nearest(set []secret.Entry, name, location string) string {
	best := ""
	for _, e := range set {
		above := e.At == "/" || e.At == location || strings.HasPrefix(location, e.At+"/")
		if e.Name == name && above && len(e.At) > len(best) {
			best = e.At
		}
	}
	return best
}

// hrefs gives the address of a card's link: the page with the card unfolded,
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

// stream sends the page the list as the directory now holds it, and the mark
// of the file chosen, by which the page shows it again when it changed.
func (h *handler) stream(w http.ResponseWriter, r *http.Request) {
	open := r.URL.Query().Get("file")
	never := make(chan struct{})
	page.Stream(w, r, h.Interval, func() <-chan struct{} { return never }, func(ctx context.Context) ([]page.Part, error) {
		ix, err := h.tree(ctx)
		if err != nil {
			return nil, err
		}
		v := view{index: ix, Open: open}
		if open != "" {
			h.opened(&v)
		}
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

// A codeLine is one line of a file as the text view shows it, in spans,
// its anchor, and whether a problem is found at it.
type codeLine struct {
	Spans []span
	ID    string
	Bad   bool
}

// A span is a piece of a line: a key, an expression, a comment, or plain,
// and where it links to.
type span struct {
	Class, Text, Href string
}

var (
	keyAt   = regexp.MustCompile(`^(\s*(?:-\s+)?)([A-Za-z_][\w.-]*)(:)(\s|$)`)
	exprIn  = regexp.MustCompile(`\$\{[^}]*\}`)
	comment = regexp.MustCompile(`(^|\s)#.*$`)
	lineAt  = regexp.MustCompile(`^(.*\S)(\s+)# (\S+):(\d+)$`)
)

// colour gives a file's lines, its keys, expressions and comments set apart,
// and the lines of the problems found in it marked.
func colour(src []byte, file string, found []problem) []codeLine {
	bad := map[int]bool{}
	for _, p := range found {
		if p.File == file {
			bad[p.Line] = true
		}
	}
	var out []codeLine
	for i, line := range strings.Split(strings.TrimSuffix(string(src), "\n"), "\n") {
		l := colourLine(line)
		l.ID, l.Bad = fmt.Sprintf("L%d", i+1), bad[i+1]
		out = append(out, l)
	}
	return out
}

// writtenOut gives a machine written out, each value's file and line a
// link to that line of that file.
func writtenOut(text []byte) []codeLine {
	var out []codeLine
	for _, line := range strings.Split(strings.TrimSuffix(string(text), "\n"), "\n") {
		m := lineAt.FindStringSubmatch(line)
		if m == nil {
			out = append(out, colourLine(line))
			continue
		}
		l := colourLine(m[1])
		href := "/machines?" + url.Values{"file": {m[3]}, "view": {"text"}}.Encode() + "#L" + m[4]
		l.Spans = append(l.Spans, span{Text: m[2]}, span{Class: "c", Text: "# " + m[3] + ":" + m[4], Href: href})
		out = append(out, l)
	}
	return out
}

// colourLine gives a line's keys, expressions and comment set apart.
func colourLine(line string) codeLine {
	var l codeLine
	rest, note := line, ""
	if at := comment.FindStringIndex(line); at != nil {
		if line[at[0]] != '#' {
			at[0]++
		}
		rest, note = line[:at[0]], line[at[0]:]
	}
	if m := keyAt.FindStringSubmatchIndex(rest); m != nil {
		l.Spans = append(l.Spans, span{Text: rest[:m[3]]}, span{Class: "k", Text: rest[m[4]:m[5]]})
		rest = rest[m[5]:]
	}
	for {
		at := exprIn.FindStringIndex(rest)
		if at == nil {
			break
		}
		l.Spans = append(l.Spans, span{Text: rest[:at[0]]}, span{Class: "x", Text: rest[at[0]:at[1]]})
		rest = rest[at[1]:]
	}
	l.Spans = append(l.Spans, span{Text: rest})
	if note != "" {
		l.Spans = append(l.Spans, span{Class: "c", Text: note})
	}
	return l
}

// shortly writes a directory as a person types it: under the home
// directory, from ~.
func shortly(dir string) string {
	if home, err := os.UserHomeDir(); err == nil && (dir == home || strings.HasPrefix(dir, home+string(filepath.Separator))) {
		return "~" + strings.TrimPrefix(dir, home)
	}
	return dir
}

// short cuts a long path in the middle, keeping its start and its end.
func short(p string) string {
	if r := []rune(p); len(r) > 34 {
		return string(r[:10]) + "…" + string(r[len(r)-23:])
	}
	return p
}

// plural writes a count and a noun.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func render(name string, data any) (template.HTML, error) {
	var b bytes.Buffer
	err := pages.ExecuteTemplate(&b, name, data)
	return template.HTML(b.String()), err
}
