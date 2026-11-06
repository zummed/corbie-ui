package machines

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/zummed/corbie-ui/drawing"
	"github.com/zummed/corbie-ui/page"
	"github.com/zummed/corbie/agent"
	"github.com/zummed/corbie/machine"
	"go.yaml.in/yaml/v3"
)

// A part is a group of tools under an agent's `open:`, as the Parts page shows
// it. File is the file it is included from, where one is written, Opening
// says opening it needs approval, and Broken that its file has problems.
type part struct {
	ID, Name, Does, File string
	Opening, Broken      bool
	Tools                []partTool
	Opened               []opener
	key                  string
}

// A partTool is a tool of a part: its job's kind, the file that job names, and
// whether the job needs approval.
type partTool struct {
	Name, Does, Job, File string
	Approve               bool
}

// An opener is an agent that opens a part, the machines that start it, and
// for an agent in a file of its own, how its location's runs start.
type opener struct {
	Name, File, Lifetime, Started string
	Starts                        []start
}

// A start is a machine that starts an agent.
type start struct {
	Name, File, Type string
}

// partsView is the Parts page as it is drawn.
// It lists the project's problems as the Machines page does, and shows the
// part chosen with the problems found in its file, or its file's text.
type partsView struct {
	Dir      string
	Parts    []part
	Open     *part
	Problems []problem
	Found    []problem
	ShowText bool
	Text     []codeLine
	Unread   string
}

// A node is one file of a machine as the parts are read: the type and name of
// the machine in it.
type node struct {
	name, kind string
}

// partIndex is what the machines in a directory say of parts.
type partIndex struct {
	byFile map[string]*node
	above  map[string][]string
	parts  map[string]*part
}

// partsOf reads every part in the directory: those that its machines open,
// and through them the machines they name, and those that no agent opens. A
// machine with problems opens none, since Corbie does not write it out.
func (d *dir) partsOf(ix *index) []part {
	px := &partIndex{byFile: map[string]*node{}, above: map[string][]string{}, parts: map[string]*part{}}
	for _, file := range sortedKeys(ix.machines) {
		if m := ix.machines[file]; m.Draft != nil {
			px.add(file, m.Draft)
		}
	}
	var parts []part
	for _, p := range px.parts {
		for i := range p.Opened {
			o := &p.Opened[i]
			o.Starts = px.startsOf(o.File)
			if m := ix.machines[o.File]; m != nil && m.Draft != nil && px.byFile[o.File].kind == "agent" {
				o.Lifetime, o.Started = m.Draft.Definition.Lifetime, startedBy(m.Draft.Definition.Lifetime, m.Location)
			}
		}
		slices.SortFunc(p.Opened, func(a, b opener) int { return strings.Compare(a.File+a.Name, b.File+b.Name) })
		parts = append(parts, *p)
	}
	parts = append(parts, d.unopened(ix, parts)...)
	for i := range parts {
		parts[i].Broken = parts[i].File != "" && slices.ContainsFunc(ix.Problems, func(p problem) bool { return p.File == parts[i].File })
	}
	slices.SortFunc(parts, func(a, b part) int { return strings.Compare(a.Name+a.key, b.Name+b.key) })
	uses := map[string]int{}
	for i := range parts {
		p := &parts[i]
		uses[p.Name]++
		p.ID = p.Name
		if uses[p.Name] > 1 {
			p.ID = fmt.Sprintf("%s-%d", p.Name, uses[p.Name])
		}
	}
	return parts
}

// partsView gives the Parts page with a part chosen, by default the first.
func (ix *index) partsView(open string) partsView {
	v := partsView{Parts: ix.parts, Problems: ix.Problems, Unread: ix.Unread}
	for i := range v.Parts {
		if v.Parts[i].ID == open || (open == "" && i == 0) {
			v.Open = &v.Parts[i]
		}
	}
	if v.Open != nil && v.Open.File != "" {
		v.Found = ix.at(v.Open.File)
	}
	return v
}

// add reads a machine in a file once, and what it names.
func (px *partIndex) add(file string, d *machine.Draft) {
	if px.byFile[file] != nil {
		return
	}
	px.byFile[file] = &node{name: d.Definition.Name, kind: d.Definition.Type}
	px.read(file, d)
}

// read reads a machine, in its own file or written in place in that file: the
// parts of an agent, and the machines it names.
func (px *partIndex) read(file string, d *machine.Draft) {
	if d.Definition.Type == "agent" {
		var a agent.Agent
		if json.Unmarshal(d.Definition.Body, &a) == nil {
			px.opens(file, &a, d)
		}
	}
	for _, name := range sortedKeys(d.Children) {
		if strings.HasPrefix(name, "in-place-") {
			px.read(file, d.Children[name])
			continue
		}
		child := path.Clean(name)
		if !slices.Contains(px.above[child], file) {
			px.above[child] = append(px.above[child], file)
		}
		px.add(child, d.Children[name])
	}
}

// opens notes the parts of an agent. Two agents that include one file open one
// part, whichever path and name each gives it. Parts in no file are one where
// their names, their `does:` and their tools are the same.
func (px *partIndex) opens(file string, a *agent.Agent, d *machine.Draft) {
	for _, name := range sortedKeys(a.Parts) {
		var tools []string
		for _, tool := range sortedKeys(a.Tools) {
			if a.Tools[tool].Part == name {
				tools = append(tools, tool)
			}
		}
		included := drawing.PartFile(d.Definition, name)
		key := included
		if key == "" {
			key = name + "\x00" + a.Parts[name].Does + "\x00" + strings.Join(tools, " ")
		}
		p := px.parts[key]
		if p == nil {
			p = &part{Name: name, Does: a.Parts[name].Does, File: included, key: key}
			for _, tool := range tools {
				p.Tools = append(p.Tools, toolOf(tool, a.Tools[tool], d))
			}
			px.parts[key] = p
		}
		p.Opening = p.Opening || a.Parts[name].Approve
		if !slices.ContainsFunc(p.Opened, func(o opener) bool { return o.File == file && o.Name == d.Definition.Name }) {
			p.Opened = append(p.Opened, opener{Name: d.Definition.Name, File: file})
		}
	}
}

// toolOf gives a tool as the page shows it, its paths written from the
// directory.
func toolOf(name string, t *agent.Tool, d *machine.Draft) partTool {
	out := partTool{Name: name, Does: t.Does}
	if t.Job == nil {
		return out
	}
	out.Approve = t.Job.Approve
	if t.Job.IsMachine() {
		out.Job = "machine"
		if d.Children[t.Job.Machine] != nil && !strings.HasPrefix(t.Job.Machine, "in-place-") {
			out.File = path.Clean(t.Job.Machine)
		}
		return out
	}
	out.Job, out.File = "script", writtenFrom(".", t.Job.Script)
	return out
}

// writtenFrom gives a path a file writes from the directory, or nothing for one
// of Corbie's own scripts or one an expression gives.
func writtenFrom(file, name string) string {
	if name == "" || strings.HasPrefix(name, "corbie:") || machine.HasExpr(name) {
		return ""
	}
	return fromFile(file, name)
}

// startsOf gives the machines that start the agent in a file: for an agent
// in a file of its own, each machine that runs that file with `machine:`;
// for one written in place, the machine it is written in.
func (px *partIndex) startsOf(file string) []start {
	if h := px.byFile[file]; h.kind != "agent" {
		return []start{{Name: h.name, File: file, Type: h.kind}}
	}
	var starts []start
	for _, above := range px.above[file] {
		h := px.byFile[above]
		starts = append(starts, start{Name: h.name, File: above, Type: h.kind})
	}
	slices.SortFunc(starts, func(a, b start) int { return strings.Compare(a.File, b.File) })
	return starts
}

// startedBy says how the runs at a location with a lifetime start.
func startedBy(lifetime, location string) string {
	switch lifetime {
	case machine.LifetimeSingleton:
		return "the submit starts its one run"
	case machine.LifetimeKeyed:
		return "a send to a key starts the run for that key"
	}
	return "a call, or corbie run submit " + location + ", starts each run"
}

// unopened gives each piece in the directory that is written as a part and
// that no agent opens.
func (d *dir) unopened(ix *index, known []part) []part {
	var found []part
	for _, piece := range ix.Pieces {
		if p, ok := d.writtenPart(piece.Path); ok && !slices.ContainsFunc(known, func(k part) bool { return k.File == piece.Path }) {
			found = append(found, p)
		}
	}
	return found
}

// writtenPart reads a file with `does:` and `tools:` at its top level as a
// part: a machine's file is not one. A part has no name until an agent opens
// it, so it goes by its directory's name.
func (d *dir) writtenPart(rel string) (part, bool) {
	src, _, err := d.read(rel)
	if err != nil {
		return part{}, false
	}
	if kind, _ := firstLine(src); kind != "" {
		return part{}, false
	}
	var written struct {
		Does  string
		Tools map[string]struct {
			Does string
			Job  any
		}
	}
	if yaml.Unmarshal(src, &written) != nil || written.Does == "" || written.Tools == nil {
		return part{}, false
	}
	name := path.Base(path.Dir(rel))
	if name == "." {
		name = strings.TrimSuffix(path.Base(rel), ".yaml")
	}
	p := part{Name: name, Does: written.Does, File: rel, key: rel}
	for _, tool := range sortedKeys(written.Tools) {
		p.Tools = append(p.Tools, writtenTool(tool, written.Tools[tool].Does, written.Tools[tool].Job, rel))
	}
	return p, true
}

// writtenTool gives a tool as a part's file writes it. Its job is a script's
// path, or a mapping.
func writtenTool(name, does string, written any, file string) partTool {
	t := partTool{Name: name, Does: does, Job: "script"}
	job, _ := written.(map[string]any)
	if job == nil {
		script, _ := written.(string)
		t.File = writtenFrom(file, script)
		return t
	}
	t.Approve, _ = job["approve"].(bool)
	script, _ := job["script"].(string)
	t.File = writtenFrom(file, script)
	if ran, ok := job["machine"]; ok {
		named, _ := ran.(string)
		t.Job, t.File = "machine", writtenFrom(file, named)
	}
	return t
}

// partsPage reads the Parts page as an address asks for it: the part
// chosen, and its file's text where the address says view=text.
func (h *handler) partsPage(q url.Values) (partsView, error) {
	v := h.index.current().partsView(q.Get("part"))
	v.Dir = shortly(h.Dir)
	if v.Open != nil && v.Open.File != "" && q.Get("view") == "text" {
		src, _, err := h.dir.read(v.Open.File)
		if err != nil {
			return v, err
		}
		v.ShowText, v.Text = true, colour(src, v.Open.File, v.Found)
	}
	return v, nil
}

// showParts is the Parts page.
func (h *handler) showParts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	v, err := h.partsPage(q)
	if err != nil {
		page.Error(w, http.StatusNotFound, err.Error())
		return
	}
	id := q.Get("part")
	if id != "" && v.Open == nil {
		page.Error(w, http.StatusNotFound, "there is no part "+id)
		return
	}
	stream := url.Values{}
	if v.Open != nil {
		stream.Set("part", v.Open.ID)
	}
	if v.ShowText {
		stream.Set("view", "text")
	}
	main, err := render("parts", v)
	if err != nil {
		page.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	s := h.Shell
	s.Title, s.Current, s.Stream, s.Main = "Parts", "Parts", "/parts/stream?"+stream.Encode(), main
	page.Write(w, s)
}

// streamParts sends the page the parts as the directory now holds them.
func (h *handler) streamParts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	never := make(chan struct{})
	page.Stream(w, r, h.Interval, func() <-chan struct{} { return never }, func(context.Context) ([]page.Part, error) {
		v, err := h.partsPage(q)
		if err != nil {
			return nil, err
		}
		var parts []page.Part
		for _, name := range []string{"partlist", "partview"} {
			html, err := render(name, v)
			if err != nil {
				return nil, err
			}
			parts = append(parts, page.Part{ID: name, HTML: html})
		}
		return parts, nil
	})
}
