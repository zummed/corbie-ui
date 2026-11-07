package machines

import (
	"cmp"
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

// A toolkit is a group of tools under an agent's `toolkits:`, as the
// Toolkits page shows it. File is the file it is included from, where one is
// written, Instructions the first line of its `instructions:`, or
// InstructionsFile the file they are included from, Opening says opening it
// needs approval, and Broken that its file has problems.
type toolkit struct {
	ID, Name, Description, File    string
	Instructions, InstructionsFile string
	Opening, Broken                bool
	Tools                          []toolkitTool
	Offered                        []offer
	key                            string
}

// A toolkitTool is a tool of a toolkit: its job's kind, script, machine,
// call or send, the file that job names or of the machine at the location
// it calls or sends to, the location a call names, the event a send sends
// and the address it sends it to, the names of the input a call fixes, its
// `when:`, and whether the job needs approval.
type toolkitTool struct {
	Name, Description, Job, File, Call, Event, To, When string
	Fixed                                               []string
	Approve                                             bool
}

// An offer is an agent that offers a toolkit, with the `when:` of its entry,
// the machines that start it, and for an agent in a file of its own, how its
// location's runs start.
type offer struct {
	Name, File, When, Lifetime, Started string
	Starts                              []start
}

// A start is a machine that starts an agent.
type start struct {
	Name, File, Type string
}

// toolkitsView is the Toolkits page as it is drawn.
// It lists the project's problems as the Machines page does, and shows the
// toolkit chosen with the problems found in its file, or its file's text.
type toolkitsView struct {
	Dir      string
	Toolkits []toolkit
	Open     *toolkit
	Problems []problem
	Found    []problem
	ShowText bool
	Text     []codeLine
	Unread   string
}

// A node is one file of a machine as the toolkits are read: the type and name of
// the machine in it.
type node struct {
	name, kind string
}

// toolkitIndex is what the machines in a directory say of toolkits: above
// gives the files of the machines that run or call the machine in a file.
type toolkitIndex struct {
	ix       *index
	byFile   map[string]*node
	above    map[string][]string
	toolkits map[string]*toolkit
}

// toolkitsOf reads every toolkit in the directory: those that its machines
// offer, and through them the machines they name and call, and those that
// no agent offers. A machine with problems offers none, since Corbie does
// not write it out.
func (d *dir) toolkitsOf(ix *index) []toolkit {
	px := &toolkitIndex{ix: ix, byFile: map[string]*node{}, above: map[string][]string{}, toolkits: map[string]*toolkit{}}
	for _, file := range sortedKeys(ix.machines) {
		if m := ix.machines[file]; m.Draft != nil {
			px.add(file, m.Draft)
		}
	}
	var toolkits []toolkit
	for _, p := range px.toolkits {
		for i := range p.Offered {
			o := &p.Offered[i]
			o.Starts = px.startsOf(o.File)
			if m := ix.machines[o.File]; m != nil && m.Draft != nil && px.byFile[o.File].kind == "agent" {
				o.Lifetime, o.Started = m.Draft.Definition.Lifetime, startedBy(m.Draft.Definition.Lifetime, m.Location)
			}
		}
		slices.SortFunc(p.Offered, func(a, b offer) int { return strings.Compare(a.File+a.Name, b.File+b.Name) })
		toolkits = append(toolkits, *p)
	}
	toolkits = append(toolkits, d.unoffered(ix, toolkits)...)
	for i := range toolkits {
		toolkits[i].Broken = toolkits[i].File != "" && slices.ContainsFunc(ix.Problems, func(p problem) bool { return p.File == toolkits[i].File })
	}
	slices.SortFunc(toolkits, func(a, b toolkit) int { return strings.Compare(a.Name+a.key, b.Name+b.key) })
	uses := map[string]int{}
	for i := range toolkits {
		p := &toolkits[i]
		uses[p.Name]++
		p.ID = p.Name
		if uses[p.Name] > 1 {
			p.ID = fmt.Sprintf("%s-%d", p.Name, uses[p.Name])
		}
	}
	return toolkits
}

// toolkitsView gives the Toolkits page with a toolkit chosen, by default the
// first.
func (ix *index) toolkitsView(open string) toolkitsView {
	v := toolkitsView{Toolkits: ix.toolkits, Problems: ix.Problems, Unread: ix.Unread}
	for i := range v.Toolkits {
		if v.Toolkits[i].ID == open || (open == "" && i == 0) {
			v.Open = &v.Toolkits[i]
		}
	}
	if v.Open != nil && v.Open.File != "" {
		v.Found = ix.at(v.Open.File)
	}
	return v
}

// add reads a machine in a file once, and what it names.
func (px *toolkitIndex) add(file string, d *machine.Draft) {
	if px.byFile[file] != nil {
		return
	}
	px.byFile[file] = &node{name: d.Definition.Name, kind: d.Definition.Type}
	px.read(file, d)
}

// read reads a machine, in its own file or written in place in that file: the
// toolkits of an agent, the machines it names, and those its calls name.
func (px *toolkitIndex) read(file string, d *machine.Draft) {
	if d.Definition.Type == "agent" {
		var a agent.Agent
		if json.Unmarshal(d.Definition.Body, &a) == nil {
			px.offers(file, &a, d)
		}
	}
	for _, j := range machine.Jobs(d.Definition) {
		if j.Job.Call == nil || j.Job.Call.Expr != "" {
			continue
		}
		if called := px.ix.fileAt(j.Job.Call.At); called != "" && !slices.Contains(px.above[called], file) {
			px.above[called] = append(px.above[called], file)
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

// offers notes the toolkits of an agent. Two agents that include one file
// offer one toolkit, whichever path and name each gives it. Toolkits in no
// file are one where their names, their `description:` and their tools are
// the same.
func (px *toolkitIndex) offers(file string, a *agent.Agent, d *machine.Draft) {
	for _, name := range sortedKeys(a.Toolkits) {
		var tools []string
		for _, tool := range sortedKeys(a.Tools) {
			if a.Tools[tool].Toolkit == name {
				tools = append(tools, tool)
			}
		}
		included := drawing.ToolkitFile(d.Definition, name, file)
		key := included
		if key == "" {
			key = name + "\x00" + a.Toolkits[name].Description + "\x00" + strings.Join(tools, " ")
		}
		p := px.toolkits[key]
		if p == nil {
			p = &toolkit{Name: name, Description: a.Toolkits[name].Description, File: included, key: key}
			p.Instructions, p.InstructionsFile = instructionsOf(d.Definition, name, a.Toolkits[name].Instructions, cmp.Or(included, file))
			for _, tool := range tools {
				p.Tools = append(p.Tools, px.ix.toolOf(tool, a.Tools[tool], d))
			}
			px.toolkits[key] = p
		}
		p.Opening = p.Opening || a.Toolkits[name].Approve
		if !slices.ContainsFunc(p.Offered, func(o offer) bool { return o.File == file && o.Name == d.Definition.Name }) {
			p.Offered = append(p.Offered, offer{Name: d.Definition.Name, File: file, When: a.Toolkits[name].When})
		}
	}
}

// instructionsOf gives a toolkit's instructions as the page shows them: the
// file they are included from, where that is not the toolkit's own, or
// their first line.
func instructionsOf(def *machine.Definition, name, text, own string) (string, string) {
	if text == "" {
		return "", ""
	}
	if file := fileOf(def.Lines["toolkits."+name+".instructions"]); file != "" && file != own {
		return "", file
	}
	first, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return first, ""
}

// fileOf gives the file of a value's file and line, as base.yaml:3.
func fileOf(line string) string {
	if i := strings.LastIndexByte(line, ':'); i >= 0 {
		return line[:i]
	}
	return line
}

// toolOf gives a tool as the page shows it, its paths written from the
// directory: a call's or a send's file is that of the machine at the
// location it calls or sends to. A call is written `call:` or as a job's
// `call:`.
func (ix *index) toolOf(name string, t *agent.Tool, d *machine.Draft) toolkitTool {
	out := toolkitTool{Name: name, Description: t.Description, When: t.When}
	if t.Job == nil {
		return out
	}
	out.Approve = t.Job.Approve
	called := t.Call
	if called == "" && t.Job.Call != nil {
		called = cmp.Or(t.Job.Call.At, t.Job.Call.Expr)
	}
	if called != "" {
		fixed, _ := t.Job.Input.(map[string]any)
		out.Job, out.Call, out.File, out.Fixed = "call", called, ix.fileAt(called), sortedKeys(fixed)
		return out
	}
	if s := t.Job.Send; s != nil {
		out.Job, out.Event, out.To = "send", s.Event, drawing.Address(&s.To)
		if s.To.Expr == "" {
			out.File = ix.fileAt(s.To.At)
		}
		return out
	}
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

// fileAt gives the file of the machine at a location a definition writes
// out, from the tree's prefix or from / where it begins with /, or nothing
// where the directory has no machine there.
func (ix *index) fileAt(at string) string {
	if at == "" || machine.HasExpr(at) {
		return ""
	}
	location := at
	if !strings.HasPrefix(at, "/") {
		location = path.Join(ix.Prefix, at)
	}
	return ix.located[location]
}

// called gives the address on the Machines page of the machine at a
// location a call writes out, or none.
func (ix *index) called(at string) string {
	if file := ix.fileAt(at); file != "" {
		return "/machines?" + url.Values{"file": {file}}.Encode()
	}
	return ""
}

// startsOf gives the machines that start the agent in a file: for an agent
// in a file of its own, each machine that runs that file with `machine:` or
// calls its location; for one written in place, the machine it is written
// in.
func (px *toolkitIndex) startsOf(file string) []start {
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

// unoffered gives each file .corbieignore lists that is written as a toolkit
// and that no agent offers. A .yaml file in the list is one .corbieignore
// lists, since Corbie reads every other as a machine.
func (d *dir) unoffered(ix *index, known []toolkit) []toolkit {
	var found []toolkit
	for _, piece := range ix.Pieces {
		if p, ok := d.writtenToolkit(ix, piece.Path); ok && !slices.ContainsFunc(known, func(k toolkit) bool { return k.File == piece.Path }) {
			found = append(found, p)
		}
	}
	return found
}

// A writtenTool is a tool as a toolkit's file writes it.
type writtenTool struct {
	Description, Call, When string
	Job                     any
	Input                   map[string]any
	Approve                 bool
}

// writtenToolkit reads a .yaml file with `description:` and `tools:` at its
// top level, and no key that gives a type, as a toolkit. A toolkit has no
// name until an agent offers it, so it goes by its file's name.
func (d *dir) writtenToolkit(ix *index, rel string) (toolkit, bool) {
	if !strings.HasSuffix(rel, ".yaml") {
		return toolkit{}, false
	}
	src, _, err := d.read(rel)
	if err != nil {
		return toolkit{}, false
	}
	var top map[string]any
	if yaml.Unmarshal(src, &top) != nil {
		return toolkit{}, false
	}
	for key := range top {
		if _, typed := machine.TypeOf(key); typed {
			return toolkit{}, false
		}
	}
	var written struct {
		Description  string
		Instructions any
		Tools        map[string]writtenTool
	}
	if yaml.Unmarshal(src, &written) != nil || written.Description == "" || written.Tools == nil {
		return toolkit{}, false
	}
	p := toolkit{Name: strings.TrimSuffix(path.Base(rel), ".yaml"), Description: written.Description, File: rel, key: rel}
	switch in := written.Instructions.(type) {
	case string:
		p.Instructions, _, _ = strings.Cut(strings.TrimSpace(in), "\n")
	case map[string]any:
		included, _ := in["include"].(string)
		p.InstructionsFile = writtenFrom(rel, included)
	}
	for _, tool := range sortedKeys(written.Tools) {
		p.Tools = append(p.Tools, ix.writtenTool(tool, written.Tools[tool], rel))
	}
	return p, true
}

// writtenTool gives a tool as a toolkit's file writes it. A tool written
// `call:` takes its description from the machine at the location it calls;
// any other job is a script's path, or a mapping.
func (ix *index) writtenTool(name string, w writtenTool, file string) toolkitTool {
	t := toolkitTool{Name: name, Description: w.Description, Job: "script", When: condition(w.When)}
	if w.Call != "" {
		t.Job, t.Call, t.File, t.Fixed, t.Approve = "call", w.Call, ix.fileAt(w.Call), sortedKeys(w.Input), w.Approve
		if m := ix.machines[t.File]; m != nil && m.Draft != nil {
			t.Description = m.Draft.Definition.Description
		}
		return t
	}
	written := w.Job
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
	if called, ok := job["call"].(map[string]any); ok {
		at, _ := called["at"].(string)
		fixed, _ := job["input"].(map[string]any)
		t.Job, t.Call, t.File, t.Fixed = "call", at, ix.fileAt(at), sortedKeys(fixed)
	}
	if sent, ok := job["send"].(map[string]any); ok {
		t.Job, t.File = "send", ""
		t.Event, _ = sent["event"].(string)
		switch to := sent["to"].(type) {
		case string:
			t.To = to
		case map[string]any:
			var a machine.AddressSpec
			a.At, _ = to["at"].(string)
			a.Key, _ = to["key"].(string)
			a.Run, _ = to["run"].(string)
			t.To, t.File = drawing.Address(&a), ix.fileAt(a.At)
		}
	}
	return t
}

// condition gives a `when:` as Corbie keeps it: the expression, without the
// ${ } it is written in.
func condition(when string) string {
	if src, ok := machine.OneExpr(when); ok {
		return src
	}
	return when
}

// toolkitsPage reads the Toolkits page as an address asks for it: the
// toolkit chosen, and its file's text where the address says view=text.
func (h *handler) toolkitsPage(ctx context.Context, q url.Values) (toolkitsView, error) {
	ix, err := h.tree(ctx)
	if err != nil {
		return toolkitsView{}, err
	}
	v := ix.toolkitsView(q.Get("toolkit"))
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

// showToolkits is the Toolkits page.
func (h *handler) showToolkits(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	v, err := h.toolkitsPage(r.Context(), q)
	if err != nil {
		page.Error(w, http.StatusNotFound, err.Error())
		return
	}
	id := q.Get("toolkit")
	if id != "" && v.Open == nil {
		page.Error(w, http.StatusNotFound, "there is no toolkit "+id)
		return
	}
	stream := url.Values{}
	if v.Open != nil {
		stream.Set("toolkit", v.Open.ID)
	}
	if v.ShowText {
		stream.Set("view", "text")
	}
	main, err := render("toolkits", v)
	if err != nil {
		page.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	s := h.Shell
	s.Title, s.Current, s.Stream, s.Main = "Toolkits", "Toolkits", "/toolkits/stream?"+stream.Encode(), main
	page.Write(w, s)
}

// streamToolkits sends the page the toolkits as the directory now holds them.
func (h *handler) streamToolkits(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	never := make(chan struct{})
	page.Stream(w, r, h.Interval, func() <-chan struct{} { return never }, func(ctx context.Context) ([]page.Part, error) {
		v, err := h.toolkitsPage(ctx, q)
		if err != nil {
			return nil, err
		}
		var parts []page.Part
		for _, name := range []string{"toolkitlist", "toolkitview"} {
			html, err := render(name, v)
			if err != nil {
				return nil, err
			}
			parts = append(parts, page.Part{ID: name, HTML: html})
		}
		return parts, nil
	})
}
