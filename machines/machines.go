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

	"github.com/zummed/corbie-ui/drawing"
	"github.com/zummed/corbie-ui/page"
	"github.com/zummed/corbie/identity"
	"github.com/zummed/corbie/secret"
	"github.com/zummed/corbie/store"
)

// Config is what the Machines page is given: the directory of machines, the
// controller and its store when the UI acts as the controller, which give
// each secret's state, how often the page looks for a change, and the shell
// every page shares.
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

// view is the Machines page as it is drawn.
type view struct {
	Dir, DirFull string
	*index
	Open       string
	Kind       string
	First      string
	Base       string
	ShowText   bool
	Drawable   bool
	Text       []codeLine
	Found      []problem
	Secrets    []secretView
	Controller bool
	Drawing    template.HTML
}

// A secretView is a secret the definition names, and its state on the
// controller: set, not set, or nothing when the UI does not act as the
// controller.
type secretView struct {
	Name, State string
}

func (h *handler) show(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ix := h.index.current()
	v := view{Dir: short(shortly(h.Dir)), DirFull: shortly(h.Dir), index: ix, Open: q.Get("file"), ShowText: q.Get("view") == "text", Controller: h.Controller != nil}
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
		if v.Kind != "" {
			if v.Secrets, err = h.secretsOf(r.Context(), src); err != nil {
				page.Error(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		m := ix.machines[v.Open]
		v.Drawable = m != nil && m.Draft != nil
		if !v.ShowText && v.Drawable {
			open := map[string]bool{}
			for _, at := range q["open"] {
				open[at] = true
			}
			u := drawing.Unfold{Open: open, Href: hrefs(v.Open, open), Part: h.dir.partFile}
			if v.Drawing, err = drawing.Unfolded(m.Draft, v.Open, u); err != nil {
				page.Error(w, http.StatusInternalServerError, err.Error())
				return
			}
		} else {
			v.ShowText, v.Text = true, colour(src, v.Open, v.Found)
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
			base += m.Draft.Hash()
		}
		var name string
		v.Kind, name = firstLine(src)
		if m.Draft != nil {
			v.Kind, name = m.Draft.Definition.Type, m.Draft.Definition.Name
		}
		v.First = v.Kind + ": " + name
	}
	v.Base = hash(fmt.Appendf([]byte(base), "%v", v.Found))
	return src, nil
}

// secretsOf gives the secrets a definition names, and, acting as the
// controller, which are set.
func (h *handler) secretsOf(ctx context.Context, src []byte) ([]secretView, error) {
	var out []secretView
	for _, name := range secretsNamed(src) {
		out = append(out, secretView{Name: name})
	}
	if h.Controller == nil || len(out) == 0 {
		return out, nil
	}
	set, err := secret.List(ctx, h.Store, h.Controller.Name)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].State = "not set"
		if slices.ContainsFunc(set, func(e secret.Entry) bool { return e.Name == out[i].Name }) {
			out[i].State = "set"
		}
	}
	return out, nil
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
	page.Stream(w, r, h.Interval, func() <-chan struct{} { return never }, func(context.Context) ([]page.Part, error) {
		v := view{index: h.index.current(), Open: open}
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

// A codeLine is one line of a file as the text view shows it, in spans, and
// whether a problem is found at it.
type codeLine struct {
	Spans []span
	Bad   bool
}

// A span is a piece of a line: a key, an expression, a comment, or plain.
type span struct {
	Class, Text string
}

var (
	keyAt   = regexp.MustCompile(`^(\s*(?:-\s+)?)([A-Za-z_][\w.-]*)(:)(\s|$)`)
	exprIn  = regexp.MustCompile(`\$\{[^}]*\}`)
	comment = regexp.MustCompile(`(^|\s)#.*$`)
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
		l := codeLine{Bad: bad[i+1]}
		rest, note := line, ""
		if at := comment.FindStringIndex(line); at != nil {
			if line[at[0]] != '#' {
				at[0]++
			}
			rest, note = line[:at[0]], line[at[0]:]
		}
		if m := keyAt.FindStringSubmatchIndex(rest); m != nil {
			l.Spans = append(l.Spans, span{"", rest[:m[3]]}, span{"k", rest[m[4]:m[5]]})
			rest = rest[m[5]:]
		}
		for {
			at := exprIn.FindStringIndex(rest)
			if at == nil {
				break
			}
			l.Spans = append(l.Spans, span{"", rest[:at[0]]}, span{"x", rest[at[0]:at[1]]})
			rest = rest[at[1]:]
		}
		l.Spans = append(l.Spans, span{"", rest})
		if note != "" {
			l.Spans = append(l.Spans, span{"c", note})
		}
		out = append(out, l)
	}
	return out
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
