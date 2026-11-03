// Package runs is the Runs page, which lists the runs in progress by the
// root each is under, and a run's page, which shows one run and acts on it.
package runs

import (
	"bytes"
	"context"
	"embed"
	"encoding/json/v2"
	"errors"
	"html/template"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/zummed/corbie-ui/page"
	"github.com/zummed/corbie/client"
	"github.com/zummed/corbie/store"
)

// A List is every run whose status the UI may read, as last read, and a
// signal of each change.
type List interface {
	Runs() ([]*client.Run, error)
	Changed() <-chan struct{}
	Poke()
}

// Config is what the Runs pages are given: the list, the parties that read
// runs, whether the UI acts as the controller, how a command runs, how
// often the store is read, and the shell every page shares.
type Config struct {
	List       List
	Lister     *client.Client
	Client     *client.Client
	Controller bool
	Command    page.Command
	Interval   time.Duration
	Shell      page.Shell
}

type handler struct {
	Config
	steps *steps
}

//go:embed list.html run.html runs.js
var files embed.FS

var pages = template.Must(template.New("").Funcs(template.FuncMap{
	"cmd":  func(c *page.Call) string { return c.String() },
	"word": pluralWord,
}).ParseFS(files, "list.html", "run.html"))

// New gives the Runs pages.
func New(c Config) http.Handler {
	h := &handler{Config: c, steps: &steps{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /runs", h.listPage)
	mux.HandleFunc("GET /runs/stream", h.listStream)
	mux.HandleFunc("POST /runs/cancel", h.cancel)
	mux.HandleFunc("GET /runs/script.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		data, _ := files.ReadFile("runs.js")
		w.Write(data)
	})
	mux.HandleFunc("GET /run/{who}/{stamp}", h.runPage)
	mux.HandleFunc("GET /run/{who}/{stamp}/stream", h.runStream)
	mux.HandleFunc("GET /run/{who}/{stamp}/file", h.file)
	mux.HandleFunc("POST /run/{who}/{stamp}/{act}", h.act)
	return mux
}

// listOf reads the Runs page: the runs, and the latest step of each
// running run the UI's client started.
func (h *handler) listOf(ctx context.Context, n narrowing) (listView, error) {
	runs, err := h.List.Runs()
	if runs == nil && err != nil {
		return listView{}, err
	}
	steps, stepErr := h.steps.of(ctx, h.Client, runs)
	mine := ""
	if h.Client != nil && h.Client.ID != nil {
		mine = h.Client.ID.Name
	}
	return listOf(runs, steps, n, time.Now(), mine), errors.Join(err, stepErr)
}

func (h *handler) listParts(ctx context.Context, n narrowing) ([]page.Part, error) {
	v, err := h.listOf(ctx, n)
	if err != nil {
		return nil, err
	}
	var parts []page.Part
	// The ended runs are sent before the running ones, so that a run that
	// ended is found running on the page as its new line arrives.
	for _, id := range []string{"ended", "busy", "idle", "services"} {
		html, err := render(id, v)
		if err != nil {
			return nil, err
		}
		parts = append(parts, page.Part{ID: id, HTML: html})
	}
	return parts, nil
}

func (h *handler) listPage(w http.ResponseWriter, r *http.Request) {
	n := narrowingOf(r.URL.Query())
	v, _ := h.listOf(r.Context(), n)
	main, err := render("list", v)
	if err != nil {
		page.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	s := h.Shell
	s.Title, s.Current, s.Stream, s.Script, s.Main = "Runs", "Runs", "/runs/stream"+n.query(), "/runs/script.js", main
	page.Write(w, s)
}

func (h *handler) listStream(w http.ResponseWriter, r *http.Request) {
	n := narrowingOf(r.URL.Query())
	page.Stream(w, r, h.Interval, h.List.Changed, func(ctx context.Context) ([]page.Part, error) { return h.listParts(ctx, n) })
}

func idOf(r *http.Request) (string, error) {
	id := r.PathValue("who") + "/" + r.PathValue("stamp")
	return id, store.CheckInstance(id)
}

func (h *handler) runPage(w http.ResponseWriter, r *http.Request) {
	id, err := idOf(r)
	if err != nil {
		page.Error(w, http.StatusNotFound, err.Error())
		return
	}
	var entries []client.Entry
	if h.mine(id) {
		h.Client.Record(r.Context(), id, func(e client.Entry) { entries = append(entries, e) })
	}
	v, err := h.runOf(r.Context(), id, entries, &reading{})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, client.ErrNoRun) {
			status = http.StatusNotFound
		}
		page.Error(w, status, err.Error())
		return
	}
	main, err := render("run", v)
	if err != nil {
		page.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	s := h.Shell
	s.Title, s.Current, s.Stream, s.Script, s.Main = v.Run.Machine, "Runs", "/run/"+id+"/stream", "/runs/script.js", main
	page.Write(w, s)
}

// runStream follows a run: its status from the list, and, for a run the
// UI's client started, its record as it is written.
func (h *handler) runStream(w http.ResponseWriter, r *http.Request) {
	id, err := idOf(r)
	if err != nil {
		page.Error(w, http.StatusNotFound, err.Error())
		return
	}
	ctx := r.Context()
	rec := newRecord()
	if h.mine(id) {
		go h.Client.Follow(ctx, id, h.Interval, rec.add)
	}
	changed := func() <-chan struct{} { return either(ctx, h.List.Changed(), rec.Changed()) }
	read := &reading{}
	page.Stream(w, r, h.Interval, changed, func(ctx context.Context) ([]page.Part, error) {
		v, err := h.runOf(ctx, id, rec.all(), read)
		if err != nil {
			return nil, err
		}
		var parts []page.Part
		for _, id := range runParts {
			html, err := render(id, v)
			if err != nil {
				return nil, err
			}
			parts = append(parts, page.Part{ID: id, HTML: html})
		}
		return parts, nil
	})
}

// either gives a channel closed when either of two is, or the context ends.
func either(ctx context.Context, a, b <-chan struct{}) <-chan struct{} {
	out := make(chan struct{})
	go func() {
		select {
		case <-a:
		case <-b:
		case <-ctx.Done():
		}
		close(out)
	}()
	return out
}

// file sends one file of a run's result, as `corbie run show --save`
// fetches it.
func (h *handler) file(w http.ResponseWriter, r *http.Request) {
	id, err := idOf(r)
	if err != nil || !h.mine(id) {
		page.Error(w, http.StatusNotFound, "only the client that started a run can read its files")
		return
	}
	path := r.URL.Query().Get("path")
	reader, _, err := h.Client.Fetch(r.Context(), id, path)
	if err != nil {
		page.Error(w, http.StatusNotFound, err.Error())
		return
	}
	download(w, path, reader)
}

// download sends a file of a run's result to be saved and never shown: a
// result can hold HTML or SVG a script fetched from anywhere, which shown
// in a page would run with the UI's cookie.
func download(w http.ResponseWriter, path string, r io.Reader) {
	name := strings.Trim(strings.Map(func(r rune) rune {
		if r == '.' || r == '_' || r == '-' || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9') {
			return r
		}
		return '-'
	}, path), "-.")
	if name == "" {
		name = "file"
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	io.Copy(w, r)
}

// act runs the command of an action on a run, and answers with what it
// printed: the receipt, or the error.
func (h *handler) act(w http.ResponseWriter, r *http.Request) {
	id, err := idOf(r)
	if err != nil {
		page.Error(w, http.StatusNotFound, err.Error())
		return
	}
	mine := h.mine(id)
	act := r.PathValue("act")
	var call page.Call
	switch act {
	case "reply":
		value, err := jsonOf(r.FormValue("value"))
		if err != nil {
			page.Error(w, http.StatusBadRequest, "the value is not JSON: "+err.Error())
			return
		}
		call = page.Call{Words: []string{"run", "reply"}, Args: []string{id, r.FormValue("ticket")}}
		if value != "" {
			call.Args = append(call.Args, value)
		}
	case "send":
		data, err := jsonOf(r.FormValue("data"))
		if err != nil {
			page.Error(w, http.StatusBadRequest, "the data is not JSON: "+err.Error())
			return
		}
		call = page.Call{Words: []string{"run", "send"}, Args: []string{id, r.FormValue("event")}}
		if data != "" {
			call.Args = append(call.Args, data)
		}
	case "cancel":
		call = cancelOf(id)
		if !mine {
			call.Words = []string{"controller", "cancel"}
		}
	default:
		page.Error(w, http.StatusNotFound, "there is no such action")
		return
	}
	switch {
	case h.Client == nil && call.Words[0] != "controller":
		page.Error(w, http.StatusForbidden, "only a client acts on a run: start the UI as the client")
		return
	case call.Words[0] == "controller" && !h.Controller:
		page.Error(w, http.StatusForbidden, "only the client that started a run cancels it, or the controller")
		return
	case act == "send" && !mine:
		page.Error(w, http.StatusForbidden, "only the client that started a run sends it events")
		return
	}
	answer := h.Command(r.Context(), call)
	h.List.Poke()
	if answer.Status != 0 {
		page.Error(w, http.StatusConflict, answer.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, receipt(answer))
}

// receipt is an action's answer in words: taken, refused with the reason,
// and for an event whether the machine acted on it.
func receipt(a page.Answer) string {
	var got struct {
		Taken   bool   `json:"taken"`
		Refused string `json:"refused"`
		Acted   *bool  `json:"acted"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(a.Out)), &got) != nil {
		return strings.TrimSpace(a.Said + "\n" + a.Out)
	}
	switch {
	case got.Refused != "":
		return "refused: " + got.Refused
	case got.Acted != nil && !*got.Acted:
		return "taken; the machine has no rule for the event"
	case got.Taken:
		return "taken"
	}
	return strings.TrimSpace(a.Said)
}

// jsonOf reads a value typed as JSON and gives it written out again, so a
// command reads it as the value it is. Nothing typed is no value.
func jsonOf(text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", nil
	}
	var v any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		return "", err
	}
	out, err := json.Marshal(v, json.Deterministic(true))
	return string(out), err
}

func render(name string, data any) (template.HTML, error) {
	var b bytes.Buffer
	err := pages.ExecuteTemplate(&b, name, data)
	return template.HTML(b.String()), err
}
