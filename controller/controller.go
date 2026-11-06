// Package controller is the Controller page: the runners and clients the
// controller trusts and the names of its secrets, read through Corbie's
// packages and changed with its corbie controller commands.
package controller

import (
	"bytes"
	"context"
	"embed"
	"encoding/json/v2"
	"html/template"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/zummed/corbie-ui/page"
	"github.com/zummed/corbie/identity"
	"github.com/zummed/corbie/secret"
	"github.com/zummed/corbie/store"
)

// Config is what the Controller page is given: the controller, its store,
// how a command runs, how often the store is read, and the shell every
// page shares.
type Config struct {
	Controller *identity.Identity
	Store      store.Store
	Command    page.Command
	Interval   time.Duration
	Shell      page.Shell
}

type handler struct{ Config }

//go:embed controller.html
var files embed.FS

var pages = template.Must(template.New("").Funcs(template.FuncMap{
	"join": strings.Join,
}).ParseFS(files, "controller.html"))

// A party is one runner or client the controller trusts, as `corbie
// controller parties` lists it.
type party struct {
	Name, Kind, Fingerprint string
	Tags, Ceiling, Deny     []string
	Removed                 bool
}

type view struct {
	Parties []party
	Secrets []string
}

// New gives the Controller page.
func New(c Config) http.Handler {
	h := &handler{c}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /controller", h.show)
	mux.HandleFunc("GET /controller/stream", h.stream)
	mux.HandleFunc("POST /controller/{act}", h.act)
	return mux
}

func (h *handler) read(ctx context.Context) (view, error) {
	var v view
	trusted, err := identity.ListTrust(ctx, h.Store, h.Controller.Card)
	if err != nil {
		return v, err
	}
	for _, t := range trusted {
		v.Parties = append(v.Parties, party{Name: t.Card.Name, Kind: string(t.Card.Kind), Fingerprint: t.Card.Fingerprint(),
			Tags: t.Tags, Ceiling: t.Ceiling.Allow, Deny: t.Ceiling.Deny, Removed: t.Removed})
	}
	slices.SortFunc(v.Parties, func(a, b party) int { return strings.Compare(a.Kind+a.Name, b.Kind+b.Name) })
	entries, err := secret.List(ctx, h.Store, h.Controller.Name)
	if err != nil {
		return v, err
	}
	for _, e := range entries {
		v.Secrets = append(v.Secrets, e.Name)
	}
	slices.Sort(v.Secrets)
	v.Secrets = slices.Compact(v.Secrets)
	return v, nil
}

func (h *handler) parts(ctx context.Context) ([]page.Part, error) {
	v, err := h.read(ctx)
	if err != nil {
		return nil, err
	}
	var parts []page.Part
	for _, id := range []string{"parties", "secrets"} {
		html, err := render(id, v)
		if err != nil {
			return nil, err
		}
		parts = append(parts, page.Part{ID: id, HTML: html})
	}
	return parts, nil
}

func (h *handler) show(w http.ResponseWriter, r *http.Request) {
	v, err := h.read(r.Context())
	if err != nil {
		page.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	main, err := render("controller", v)
	if err != nil {
		page.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	s := h.Shell
	s.Title, s.Current, s.Stream, s.Main = "Controller", "Controller", "/controller/stream", main
	page.Write(w, s)
}

func (h *handler) stream(w http.ResponseWriter, r *http.Request) {
	never := make(chan struct{})
	page.Stream(w, r, h.Interval, func() <-chan struct{} { return never }, h.parts)
}

// act runs the corbie controller command of an action, and answers with
// what it printed back, or its error.
func (h *handler) act(w http.ResponseWriter, r *http.Request) {
	list := func(name string) string { return strings.Join(strings.Fields(strings.ReplaceAll(r.FormValue(name), ",", " ")), ",") }
	var call page.Call
	switch r.PathValue("act") {
	case "enrol":
		call = page.Call{Words: []string{"controller", "enrol"}, Args: []string{strings.TrimSpace(r.FormValue("card"))}}
		if tags := list("tags"); tags != "" {
			call.Flags = append(call.Flags, "--tags="+tags)
		}
		if ceiling := list("ceiling"); ceiling != "" {
			call.Flags = append(call.Flags, "--ceiling="+ceiling)
		}
		if r.FormValue("replace") != "" {
			call.Flags = append(call.Flags, "--replace")
		}
	case "tags", "ceiling":
		name := r.FormValue("name")
		v, err := h.read(r.Context())
		if err != nil {
			page.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		at := slices.IndexFunc(v.Parties, func(p party) bool { return p.Name == name })
		if at < 0 || v.Parties[at].Removed {
			page.Error(w, http.StatusConflict, "the party is not trusted: enrol it again with its card")
			return
		}
		call = page.Call{Words: []string{"controller", "enrol"}, Args: []string{name}, Flags: []string{"--" + r.PathValue("act") + "=" + list(r.PathValue("act"))}}
	case "remove":
		call = page.Call{Words: []string{"controller", "remove"}, Args: []string{r.FormValue("name")}}
	case "secret-set":
		// The value goes on standard input, which "-" names to the command.
		call = page.Call{Words: []string{"controller", "secret", "set"}, Args: []string{r.FormValue("name")}, Flags: []string{"-"}, Stdin: r.FormValue("value")}
	case "secret-remove":
		call = page.Call{Words: []string{"controller", "secret", "remove"}, Args: []string{r.FormValue("name")}}
	default:
		page.Error(w, http.StatusNotFound, "there is no such action")
		return
	}
	answer := h.Command(r.Context(), call)
	if answer.Status != 0 {
		page.Error(w, http.StatusConflict, answer.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, said(answer))
}

// said is what an action's command printed back: for an enrol, the
// controller's card and the store's address, which the other side trusts.
func said(a page.Answer) string {
	var printed struct {
		Card  string `json:"card"`
		Store string `json:"store"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(a.Out)), &printed) == nil && printed.Card != "" {
		return strings.TrimSpace(a.Said) + "\n" + printed.Card + "\n" + printed.Store
	}
	if text := strings.TrimSpace(a.Said); text != "" {
		return text
	}
	return "done"
}

func render(name string, data any) (template.HTML, error) {
	var b bytes.Buffer
	err := pages.ExecuteTemplate(&b, name, data)
	return template.HTML(b.String()), err
}
