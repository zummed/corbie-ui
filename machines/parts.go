package machines

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/zummed/corbie-ui/page"
	"github.com/zummed/corbie/agent"
	"github.com/zummed/corbie/machine"
	"go.yaml.in/yaml/v3"
)

// A part is a group of tools under an agent's `open:`, as the Parts page shows
// it. File is the file it is included from, where one is written, and Opening
// says opening it needs approval.
type part struct {
	ID, Name, Does, File string
	Opening              bool
	Tools                []partTool
	Opened               []opener
	key                  string
}

// A partTool is a tool of a part: its job's kind, the file that job names, and
// whether the job needs approval.
type partTool struct {
	Name, Does, Job, File string
	Held                  bool
}

// An opener is an agent that opens a part, and the machines that start it.
type opener struct {
	Name, File string
	Starts     []start
}

// A start is a machine that starts an agent.
type start struct {
	Name, File, Type string
}

// partsView is the Parts page as it is drawn.
type partsView struct {
	Dir      string
	Parts    []part
	Open     *part
	Problems []problem
	Broken   int
	Unread   string
}

// A node is one file of a machine as the parts are read: the type and name of
// the machine in it.
type node struct {
	name, kind string
}

// partIndex is what the machines in a directory say of parts.
type partIndex struct {
	dir    *dir
	byFile map[string]*node
	above  map[string][]string
	parts  map[string]*part
}

// partsOf reads every part in the directory: those that the project's
// machines open, and through them the machines they name, and those that no
// agent opens.
func (d *dir) partsOf(ix *index) []part {
	px := &partIndex{dir: d, byFile: map[string]*node{}, above: map[string][]string{}, parts: map[string]*part{}}
	for _, file := range sortedKeys(ix.machines) {
		if m := ix.machines[file]; m.Draft != nil && !dotted(file) {
			px.add(file, m.Draft)
		}
	}
	var parts []part
	for _, p := range px.parts {
		for i := range p.Opened {
			p.Opened[i].Starts = px.startsOf(p.Opened[i].File)
		}
		slices.SortFunc(p.Opened, func(a, b opener) int { return strings.Compare(a.File+a.Name, b.File+b.Name) })
		parts = append(parts, *p)
	}
	parts = append(parts, d.unopened(parts)...)
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
	v := partsView{Parts: ix.parts, Broken: ix.Broken, Unread: ix.Unread}
	for i := range v.Parts {
		if v.Parts[i].ID == open || (open == "" && i == 0) {
			v.Open = &v.Parts[i]
		}
	}
	if v.Open != nil && v.Open.File != "" {
		v.Problems = ix.at(v.Open.File)
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
		included := px.dir.partFile(file, d.Definition.Name, name, a.Parts[name].Does)
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
	out.Held = t.Job.Approve
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

// startsOf gives the machines that start the agent in a file: the nearest
// machine above it on each path that is outside directories whose names begin
// with a dot, or the machine the agent is written in.
func (px *partIndex) startsOf(file string) []start {
	var starts []start
	seen := map[string]bool{}
	var up func(file string)
	up = func(file string) {
		for _, above := range px.above[file] {
			if seen[above] {
				continue
			}
			seen[above] = true
			if dotted(above) {
				up(above)
				continue
			}
			h := px.byFile[above]
			starts = append(starts, start{Name: h.name, File: above, Type: h.kind})
		}
	}
	if h := px.byFile[file]; h.kind != "agent" && !dotted(file) {
		return []start{{Name: h.name, File: file, Type: h.kind}}
	}
	up(file)
	slices.SortFunc(starts, func(a, b start) int { return strings.Compare(a.File, b.File) })
	return starts
}

// unopened gives each file in the directory that is written as a part and that
// no agent opens, outside directories whose names begin with a dot.
func (d *dir) unopened(known []part) []part {
	var found []part
	filepath.WalkDir(d.root, func(full string, e fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case e.IsDir() && full != d.root && strings.HasPrefix(e.Name(), "."):
			return filepath.SkipDir
		case e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml"):
			return nil
		}
		rel, err := filepath.Rel(d.root, full)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if p, ok := d.writtenPart(rel); ok && !slices.ContainsFunc(known, func(k part) bool {
			return k.File == rel
		}) {
			found = append(found, p)
		}
		return nil
	})
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
	t.Held, _ = job["approve"].(bool)
	script, _ := job["script"].(string)
	t.File = writtenFrom(file, script)
	if ran, ok := job["machine"]; ok {
		named, _ := ran.(string)
		t.Job, t.File = "machine", writtenFrom(file, named)
	}
	return t
}

// showParts is the Parts page.
func (h *handler) showParts(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("part")
	v := h.index.current().partsView(id)
	v.Dir = shortly(h.Dir)
	if id != "" && v.Open == nil {
		page.Error(w, http.StatusNotFound, "there is no part "+id)
		return
	}
	if v.Open != nil {
		id = v.Open.ID
	}
	main, err := render("parts", v)
	if err != nil {
		page.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	s := h.Shell
	s.Title, s.Current, s.Stream, s.Main = "Parts", "Parts", "/parts/stream?part="+url.QueryEscape(id), main
	page.Write(w, s)
}

// streamParts sends the page the parts as the directory now holds them.
func (h *handler) streamParts(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("part")
	never := make(chan struct{})
	page.Stream(w, r, h.Interval, func() <-chan struct{} { return never }, func(context.Context) ([]page.Part, error) {
		v := h.index.current().partsView(id)
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
