package machines

import (
	"cmp"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/zummed/corbie/client"
	"github.com/zummed/corbie/machine"
)

// An index is what the pages show of the directory, read once each time a
// file in it changes or its prefix does: each machine as Corbie reads it, at
// its location under the prefix, the list, the pieces, each
// problem once, and the toolkits.
type index struct {
	Prefix   string
	machines map[string]*client.Machine
	own      map[string][]problem
	Groups   []group
	Pieces   []entry
	included map[string][]string
	Problems []problem
	Unread   string
	toolkits []toolkit
}

// A group is the machines of the list in one directory.
type group struct {
	Dir     string
	Entries []entry
}

// An entry is one machine or one piece in the list: its file; a machine's
// type and first line, location, lifetime and how many problems it has; and
// how many machines include a piece.
type entry struct {
	Path, Type, First, Location, Lifetime string
	Problems, Included                    int
}

// indexed keeps the index of a directory, and reads it again only when the
// directory changed, or the prefix it is read at.
type indexed struct {
	dir       *dir
	mu        sync.Mutex
	stamp, at string
	ix        *index
}

// current gives the index of the directory as it is now, read at the
// prefix at, or at the prefix a submit gives it by default where at is "".
func (c *indexed) current(at string) *index {
	c.mu.Lock()
	defer c.mu.Unlock()
	if stamp := c.dir.stamp(); c.ix == nil || stamp != c.stamp || at != c.at {
		c.ix, c.stamp, c.at = c.dir.index(at), stamp, at
	}
	return c.ix
}

// index reads every machine in the directory with Corbie's own reader, as
// a tree at the prefix at, or at the prefix a submit gives it by default
// where at is "".
func (d *dir) index(at string) *index {
	ix := &index{machines: map[string]*client.Machine{}, own: map[string][]problem{}, included: map[string][]string{}}
	tree, err := client.ReadTree(d.root, at)
	if err != nil {
		ix.Unread = err.Error()
		return ix
	}
	ix.Prefix = tree.Prefix
	found := tree.Machines
	for _, m := range found {
		ix.machines[m.Path] = m
		if m.Problem != nil {
			ix.own[m.Path] = slices.SortedStableFunc(slices.Values(d.problemsOf(m.Path, m.Problem)), byPlace)
		}
	}
	ix.unreadIncluded(d)
	ix.Problems = d.problemsOnce(ix.own)
	groups := map[string][]entry{}
	for _, m := range found {
		if ix.machines[m.Path] == nil {
			continue
		}
		en := entry{Path: m.Path, Location: m.Location, Problems: len(ix.at(m.Path))}
		if m.Draft != nil {
			en.Type, en.First, en.Lifetime = m.Draft.Definition.Type, m.Draft.Definition.Name, m.Draft.Definition.Lifetime
			for _, file := range filesOf(m.Draft.Definition) {
				if file != m.Path && ix.machines[file] == nil {
					ix.included[file] = append(ix.included[file], m.Path)
				}
			}
		} else if src, _, err := d.read(m.Path); err == nil {
			en.Type, en.First = firstLine(src)
		}
		if en.Type != "" {
			en.First = en.Type + ": " + en.First
		}
		groups[path.Dir(m.Path)] = append(groups[path.Dir(m.Path)], en)
	}
	for _, at := range sortedKeys(groups) {
		ix.Groups = append(ix.Groups, group{Dir: at, Entries: groups[at]})
	}
	files, err := d.files()
	if err != nil {
		ix.Unread = err.Error()
	}
	pieces := map[string]bool{}
	for _, file := range files {
		if strings.HasSuffix(file, ".yaml") && ix.machines[file] == nil {
			pieces[file] = true
		}
	}
	for file := range ix.included {
		pieces[file] = true
	}
	for _, file := range sortedKeys(pieces) {
		ix.Pieces = append(ix.Pieces, entry{Path: file, Included: len(ix.included[file])})
	}
	ix.toolkits = d.toolkitsOf(ix)
	return ix
}

// piece tells whether a file is a piece: a .yaml file with no type, or a
// file a machine takes a value from.
func (ix *index) piece(file string) bool {
	return slices.ContainsFunc(ix.Pieces, func(e entry) bool { return e.Path == file })
}

// filesOf gives the files a definition written out takes its values from,
// in order, its own among them.
func filesOf(def *machine.Definition) []string {
	files := map[string]bool{}
	for _, at := range def.Lines {
		if i := strings.LastIndexByte(at, ':'); i > 0 {
			files[at[:i]] = true
		}
	}
	return sortedKeys(files)
}

// unreadIncluded reads a file an `include:` names that cannot be read, and
// that does not begin as a machine does, as one the machines that include it
// read: its problems are theirs.
func (ix *index) unreadIncluded(d *dir) {
	for _, file := range sortedKeys(ix.own) {
		for _, p := range ix.own[file] {
			if m := ix.machines[p.File]; p.included && m != nil && m.Draft == nil && p.File != file && !d.beginsAsMachine(p.File) {
				delete(ix.machines, p.File)
			}
		}
	}
	for _, file := range sortedKeys(ix.own) {
		var out []problem
		for _, p := range ix.own[file] {
			if own, ok := ix.own[p.File]; p.included && ok && ix.machines[p.File] == nil {
				out = append(out, own...)
				continue
			}
			out = append(out, p)
		}
		ix.own[file] = out
	}
	for file := range ix.own {
		if ix.machines[file] == nil {
			delete(ix.own, file)
		}
	}
}

// beginsAsMachine says a file's first line gives a type of machine.
func (d *dir) beginsAsMachine(file string) bool {
	src, _, err := d.read(file)
	if err != nil {
		return false
	}
	kind, _ := firstLine(src)
	return kind != ""
}

// problemsOnce gives each problem of the directory's machines once, at the
// file Corbie names, with the machines it breaks. A machine that reads
// without problems from a directory below this one gives one problem: that
// directory is the top of its tree.
func (d *dir) problemsOnce(own map[string][]problem) []problem {
	var tops, rest []*problem
	found := map[string]*problem{}
	for _, file := range sortedKeys(own) {
		if top := d.topOf(file); top != "" {
			if found[top] == nil {
				found[top] = &problem{File: top, Top: true}
				tops = append(tops, found[top])
			}
			found[top].Breaks = append(found[top].Breaks, file)
			continue
		}
		for _, p := range own[file] {
			key := p.File + "\x00" + p.Place + "\x00" + p.Text
			if found[key] == nil {
				p.Breaks = nil
				found[key] = &p
				rest = append(rest, found[key])
			}
			if !slices.Contains(found[key].Breaks, file) {
				found[key].Breaks = append(found[key].Breaks, file)
			}
		}
	}
	slices.SortStableFunc(rest, func(a, b *problem) int { return byPlace(*a, *b) })
	var out []problem
	for _, p := range append(tops, rest...) {
		out = append(out, *p)
	}
	return out
}

// byPlace orders problems by their file, then their line.
func byPlace(a, b problem) int {
	return cmp.Or(strings.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line))
}

// topOf gives the directory below this one from which a machine reads without
// problems, nearest first, or nothing.
func (d *dir) topOf(file string) string {
	for at := path.Dir(file); at != "."; at = path.Dir(at) {
		rel := strings.TrimPrefix(file, at+"/")
		if _, err := client.ReadMachine(filepath.Join(d.root, filepath.FromSlash(at)), rel); err == nil {
			return at
		}
	}
	return ""
}

// at gives the problems found in a file: a machine's own, each with the
// machines it breaks as the list gives them, or the one problem of the
// directory that is the top of its tree, or for any other file those
// found in it by the machines that read it.
func (ix *index) at(file string) []problem {
	for _, p := range ix.Problems {
		if p.Top && slices.Contains(p.Breaks, file) {
			return []problem{p}
		}
	}
	if _, ok := ix.machines[file]; ok {
		var out []problem
		for _, p := range ix.own[file] {
			for _, listed := range ix.Problems {
				if listed.File == p.File && listed.Place == p.Place && listed.Text == p.Text {
					p.Breaks = listed.Breaks
				}
			}
			out = append(out, p)
		}
		return out
	}
	var out []problem
	for _, p := range ix.Problems {
		if p.File == file && !p.Top {
			out = append(out, p)
		}
	}
	return out
}
