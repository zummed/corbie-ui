package machines

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/zummed/corbie-ui/page"
	"github.com/zummed/corbie/agent"
	"github.com/zummed/corbie/client"
	"github.com/zummed/corbie/machine"
	"github.com/zummed/corbie/statechart"
	"go.yaml.in/yaml/v3"
)

// A part is a group of tools under an agent's `open:`, as the Parts page shows
// it. File is the file it is included from, where one is written. Ask is the
// file of its ask's machine, and Opening says opening it needs approval.
// Unnamed says it needs approval and rules.yaml names no one for it. approve
// holds the names rules.yaml lists its approvers under, which an agent's own
// `ask:` can change.
type part struct {
	ID, Name, Does, File, Ask string
	Opening, Unnamed          bool
	Tools                     []partTool
	Opened                    []opener
	Approvers                 []string
	key                       string
	approve                   []string
}

// A partTool is a tool of a part: its job's kind and the file that job names,
// whether the job needs approval, and the `allow:` entries of the job and of
// the machines it runs.
type partTool struct {
	Name, Does, Job, File string
	Held                  bool
	Allow                 []string
}

// An opener is an agent that opens a part, with the machines that start it
// and, in a Rook project, who rules.yaml gives it to. Unclear says only
// machines with problems are above it, and none of them starts it.
type opener struct {
	Name, File string
	Starts     []start
	For        []string
	Unclear    bool
	above      []string
}

// A start is a machine above an agent that no other machine names.
type start struct {
	Name, File, Type string
	Timer, Timers    string
}

// partsView is the Parts page as it is drawn.
type partsView struct {
	Dir     string
	Parts   []part
	Open    *part
	Rook    bool
	Unread  []string
	Problem []string
}

// A node is one file of a machine as the page reads it: its name, its type,
// whether it is kept running or has problems, its timers, and the names of the
// machines written in it.
type node struct {
	name, kind string
	broken     bool
	timers     []string
	names      []string
}

// index is what the machines in a directory say of parts.
type index struct {
	dir    *dir
	byFile map[string]*node
	above  map[string][]string
	parts  map[string]*part
}

// partsOf reads every part in the directory.
func (d *dir) partsOf(open string) partsView {
	v := partsView{}
	found, err := client.ReadMachines(d.root)
	if err != nil {
		v.Unread = append(v.Unread, err.Error())
	}
	ix := &index{dir: d, byFile: map[string]*node{}, above: map[string][]string{}, parts: map[string]*part{}}
	for _, m := range found {
		if m.Draft == nil {
			ix.broken(m.Path, &v)
			continue
		}
		if err := ix.add(m.Path, m.Draft); err != nil {
			v.Unread = append(v.Unread, m.Path+": "+err.Error())
		}
	}
	rules, err := d.rulesOf()
	if err != nil {
		v.Unread = append(v.Unread, "rules.yaml: "+err.Error())
	}
	v.Rook = rules != nil
	for _, p := range ix.parts {
		for i := range p.Opened {
			ix.startsOf(&p.Opened[i])
			if rules != nil {
				p.Opened[i].For = rules.forAny(p.Opened[i].above)
			}
		}
		slices.SortFunc(p.Opened, func(a, b opener) int { return strings.Compare(a.File+a.Name, b.File+b.Name) })
		if rules != nil {
			p.Approvers = rules.approvers(p.approve)
			p.Unnamed = len(p.Approvers) == 0 && p.held()
		}
		v.Parts = append(v.Parts, *p)
	}
	unopened, err := d.unopened(v.Parts)
	if err != nil {
		v.Unread = append(v.Unread, err.Error())
	}
	v.Parts = append(v.Parts, unopened...)
	slices.SortFunc(v.Parts, func(a, b part) int { return strings.Compare(a.Name+a.key, b.Name+b.key) })
	uses := map[string]int{}
	for i := range v.Parts {
		p := &v.Parts[i]
		uses[p.Name]++
		p.ID = p.Name
		if uses[p.Name] > 1 {
			p.ID = fmt.Sprintf("%s-%d", p.Name, uses[p.Name])
		}
	}
	for i := range v.Parts {
		if v.Parts[i].ID == open || (open == "" && i == 0) {
			v.Open = &v.Parts[i]
		}
	}
	return v
}

// held says a tool of the part, or opening it, needs approval.
func (p *part) held() bool {
	return p.Opening || slices.ContainsFunc(p.Tools, func(t partTool) bool { return t.Held })
}

// broken notes a machine with problems, and what its text names, so the
// machines below it keep the machines above it. A file that is not written as
// a machine is left out.
func (ix *index) broken(file string, v *partsView) {
	en, ok := ix.dir.entryOf(file)
	if !ok {
		return
	}
	v.Problem = append(v.Problem, file)
	name := strings.TrimPrefix(en.First, en.Type+": ")
	h := &node{name: name, kind: en.Type, names: []string{name}, broken: true}
	ix.byFile[file] = h
	if src, _, err := ix.dir.read(file); err == nil {
		h.fromText(src)
	}
	for _, child := range writtenIn(en) {
		ix.above[child] = append(ix.above[child], file)
	}
}

// fromText reads what a machine with problems writes of its
// timers. A field that does not fit is left out, and takes no other with it.
func (h *node) fromText(src []byte) {
	var written map[string]any
	if yaml.Unmarshal(src, &written) != nil {
		return
	}
	states, _ := written["states"].(map[string]any)
	for _, name := range sortedKeys(states) {
		state, _ := states[name].(map[string]any)
		timers, _ := state["timers"].(map[string]any)
		given := map[string]string{}
		for timer, at := range timers {
			if text, ok := at.(string); ok {
				given[timer] = text
			}
		}
		h.addTimers(given)
	}
}

// add reads a machine in a file once, and what it names.
func (ix *index) add(file string, d *machine.Draft) error {
	if ix.byFile[file] != nil {
		return nil
	}
	h := &node{name: d.Definition.Name, kind: d.Definition.Type}
	ix.byFile[file] = h
	if d.Definition.Type == "statechart" {
		var c statechart.Chart
		if err := json.Unmarshal(d.Definition.Body, &c); err != nil {
			return err
		}
		for _, name := range sortedKeys(c.States) {
			h.addTimers(c.States[name].Timers)
		}
	}
	return ix.read(file, d)
}

// addTimers adds a state's timers, kept by the day of the week.
func (h *node) addTimers(timers map[string]string) {
	for _, name := range sortedKeys(timers) {
		h.timers = append(h.timers, timers[name])
	}
	slices.SortStableFunc(h.timers, func(a, b string) int { return dayOf(a) - dayOf(b) })
}

// read reads a machine, in its own file or written in place in that file: the
// parts of an agent, and the machines it names.
func (ix *index) read(file string, d *machine.Draft) error {
	ix.byFile[file].names = append(ix.byFile[file].names, d.Definition.Name)
	if d.Definition.Type == "agent" {
		var a agent.Agent
		if err := json.Unmarshal(d.Definition.Body, &a); err != nil {
			return err
		}
		ix.opens(file, &a, d)
	}
	for _, name := range sortedKeys(d.Children) {
		var err error
		if strings.HasPrefix(name, "in-place-") {
			err = ix.read(file, d.Children[name])
		} else {
			child := path.Clean(name)
			ix.above[child] = append(ix.above[child], file)
			err = ix.add(child, d.Children[name])
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// opens notes the parts of an agent. Two agents that include one file open one
// part, whichever path and name each gives it. Parts in no file are one where
// their names, their `does:` and their tools are the same.
func (ix *index) opens(file string, a *agent.Agent, d *machine.Draft) {
	for _, name := range sortedKeys(a.Parts) {
		var tools []string
		for _, tool := range sortedKeys(a.Tools) {
			if a.Tools[tool].Part == name {
				tools = append(tools, tool)
			}
		}
		included := ix.dir.partFile(file, d.Definition.Name, name, a.Parts[name].Does)
		key := included
		if key == "" {
			key = name + "\x00" + a.Parts[name].Does + "\x00" + strings.Join(tools, " ")
		}
		p := ix.parts[key]
		if p == nil {
			p = &part{Name: name, Does: a.Parts[name].Does, File: included, key: key}
			if ask := a.Parts[name].Ask; ask != nil {
				p.Ask = fileNamed(file, d, ask.Machine)
			}
			for _, tool := range tools {
				p.Tools = append(p.Tools, toolOf(tool, a.Tools[tool], file, d))
			}
			ix.parts[key] = p
		}
		p.Opening = p.Opening || a.Parts[name].Approve
		if listed := approveName(name, a.Parts[name].Ask); !slices.Contains(p.approve, listed) {
			p.approve = append(p.approve, listed)
		}
		if !slices.ContainsFunc(p.Opened, func(o opener) bool { return o.File == file && o.Name == d.Definition.Name }) {
			p.Opened = append(p.Opened, opener{Name: d.Definition.Name, File: file})
		}
	}
}

// toolOf gives a tool as the page shows it, its paths written from the
// directory.
func toolOf(name string, t *agent.Tool, file string, d *machine.Draft) partTool {
	out := partTool{Name: name, Does: t.Does}
	if t.Job == nil {
		return out
	}
	out.Held, out.Allow = t.Job.Approve, slices.Clone(t.Job.Allow)
	if t.Job.IsMachine() {
		out.Job, out.File = "machine", fileNamed(file, d, t.Job.Machine)
		if child := d.Children[t.Job.Machine]; child != nil {
			out.Allow = allowOf(child, out.Allow)
		}
		return out
	}
	out.Job, out.File = "script", fromDir(t.Job.Script)
	return out
}

// fromDir gives a path a definition keeps from the directory, or nothing for
// one of Corbie's own scripts or one an expression gives.
func fromDir(name string) string {
	if name == "" || strings.HasPrefix(name, "corbie:") || machine.HasExpr(name) {
		return ""
	}
	return path.Clean(name)
}

// writtenFrom gives a path a file writes from the directory, or nothing for one
// of Corbie's own scripts or one an expression gives.
func writtenFrom(file, name string) string {
	if name == "" || strings.HasPrefix(name, "corbie:") || machine.HasExpr(name) {
		return ""
	}
	return fromFile(file, name)
}

// fileNamed gives the file of a machine a draft names, or nothing for one
// written in place or given by an expression.
func fileNamed(file string, d *machine.Draft, name string) string {
	if d.Children[name] == nil || strings.HasPrefix(name, "in-place-") {
		return ""
	}
	return path.Clean(name)
}

// allowOf adds the `allow:` entries of a machine and of what it runs.
func allowOf(d *machine.Draft, allow []string) []string {
	allow = append(allow, d.Definition.Allow...)
	var body any
	if json.Unmarshal(d.Definition.Body, &body) == nil {
		allow = allowIn(body, allow)
	}
	for _, name := range sortedKeys(d.Children) {
		allow = allowOf(d.Children[name], allow)
	}
	slices.Sort(allow)
	return slices.Compact(allow)
}

// allowIn adds the `allow:` of each job in a machine's full form. A mapping
// with no `script:` or `machine:` is no job: an `allow` under `input:` is data.
func allowIn(v any, allow []string) []string {
	switch v := v.(type) {
	case map[string]any:
		_, script := v["script"]
		_, runs := v["machine"]
		if list, ok := v["allow"].([]any); ok && (script || runs) {
			for _, entry := range list {
				if s, ok := entry.(string); ok {
					allow = append(allow, s)
				}
			}
		}
		for _, key := range sortedKeys(v) {
			allow = allowIn(v[key], allow)
		}
	case []any:
		for _, each := range v {
			allow = allowIn(each, allow)
		}
	}
	return allow
}

// startsOf finds the machines above an agent that no other machine names, and
// the name of every machine on the way. A machine in a dotted directory starts
// nothing here, since it runs only by what names it. An agent in its own file
// that nothing names has no machine above it.
func (ix *index) startsOf(o *opener) {
	seen := map[string]bool{}
	var walk func(file string)
	walk = func(file string) {
		if seen[file] {
			return
		}
		seen[file] = true
		above := ix.above[file]
		h := ix.byFile[file]
		alone := file == o.File && h.kind == "agent"
		if len(above) == 0 && !dotted(file) && !alone {
			o.Starts = append(o.Starts, ix.startOf(file))
		}
		for _, up := range above {
			walk(up)
		}
	}
	walk(o.File)
	slices.SortFunc(o.Starts, func(a, b start) int { return strings.Compare(a.File, b.File) })
	for file := range seen {
		o.above = append(o.above, ix.byFile[file].names...)
		o.Unclear = o.Unclear || (ix.byFile[file].broken && len(o.Starts) == 0)
	}
}

func (ix *index) startOf(file string) start {
	h := ix.byFile[file]
	s := start{Name: h.name, File: file, Type: h.kind}
	if len(h.timers) > 0 {
		s.Timer, s.Timers = h.timers[0], strings.Join(h.timers, ", ")
		if more := len(h.timers) - 1; more > 0 {
			s.Timer += fmt.Sprintf(" +%d", more)
		}
	}
	return s
}

// dayOf places a timer by its day of the week, one with no day first.
func dayOf(timer string) int {
	day, _, _ := strings.Cut(timer, " ")
	return slices.Index([]string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}, day)
}

// unopened gives each file in the directory that is written as a part and that
// no agent opens.
func (d *dir) unopened(known []part) ([]part, error) {
	var found []part
	err := filepath.WalkDir(d.root, func(full string, e fs.DirEntry, err error) error {
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
	return found, err
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
		p.Tools = append(p.Tools, d.writtenTool(tool, written.Tools[tool].Does, written.Tools[tool].Job, rel))
	}
	return p, true
}

// writtenTool gives a tool as a part's file writes it. Its job is a script's
// path, or a mapping.
func (d *dir) writtenTool(name, does string, written any, file string) partTool {
	t := partTool{Name: name, Does: does, Job: "script"}
	job, _ := written.(map[string]any)
	if job == nil {
		script, _ := written.(string)
		t.File = writtenFrom(file, script)
		return t
	}
	t.Held, _ = job["approve"].(bool)
	t.Allow = allowIn(job, nil)
	script, _ := job["script"].(string)
	t.File = writtenFrom(file, script)
	if ran, ok := job["machine"]; ok {
		named, _ := ran.(string)
		t.Job, t.File = "machine", writtenFrom(file, named)
		if src, _, err := d.read(t.File); err == nil && t.File != "" {
			if c := d.check(t.File, src); c.draft != nil {
				t.Allow = allowOf(c.draft, t.Allow)
			}
		}
	}
	return t
}

// showParts is the Parts page.
func (h *handler) showParts(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("part")
	v := h.dir.partsOf(id)
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
		v := h.dir.partsOf(id)
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

// isDir says a path inside the directory is a directory, and nothing is one
// that does not exist.
func (d *dir) isDir(rel string) (bool, error) {
	full, err := d.inside(rel)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return info.IsDir(), nil
}
