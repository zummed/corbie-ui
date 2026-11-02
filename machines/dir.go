package machines

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/zummed/corbie/client"
	"github.com/zummed/corbie/machine"
	"go.yaml.in/yaml/v3"
)

// A dir is the directory of machines: every path a request names is inside
// it, once links are followed.
type dir struct {
	root, real string
}

func newDir(root string) (*dir, error) {
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	return &dir{root: root, real: real}, nil
}

var errOutside = errors.New("the path leaves the directory of machines")

// inside gives the file on the disk that a path a request names is, or an
// error when the path leaves the directory, once links are followed. A
// file that does not exist yet is inside when the directory it would be in
// is.
func (d *dir) inside(rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return "", errOutside
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errOutside
	}
	full := filepath.Join(d.root, clean)
	real, err := filepath.EvalSymlinks(full)
	if errors.Is(err, fs.ErrNotExist) {
		parent, perr := filepath.EvalSymlinks(filepath.Dir(full))
		if perr != nil {
			return "", perr
		}
		real, err = filepath.Join(parent, filepath.Base(full)), nil
	}
	if err != nil {
		return "", err
	}
	if real != d.real && !strings.HasPrefix(real, d.real+string(filepath.Separator)) {
		return "", errOutside
	}
	return full, nil
}

// read reads a file inside the directory, and gives its hash, by which a
// save knows whether it changed.
func (d *dir) read(rel string) ([]byte, string, error) {
	full, err := d.inside(rel)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(full)
	return data, hash(data), err
}

func hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// errChanged is a save of a file that changed on disk since it was read.
var errChanged = errors.New("the file changed on disk since it was read")

// save writes a file only if it is as it was read, whose hash is base, and
// gives what is on the disk when it is not.
func (d *dir) save(rel string, text []byte, base string) (string, []byte, error) {
	full, err := d.inside(rel)
	if err != nil {
		return "", nil, err
	}
	now, err := os.ReadFile(full)
	if err != nil {
		return "", nil, err
	}
	if hash(now) != base {
		return "", now, errChanged
	}
	info, err := os.Stat(full)
	if err != nil {
		return "", nil, err
	}
	temp, err := os.CreateTemp(filepath.Dir(full), ".corbie-ui-*")
	if err != nil {
		return "", nil, err
	}
	_, err = temp.Write(text)
	if cerr := temp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(temp.Name(), info.Mode().Perm())
	}
	if err == nil {
		err = os.Rename(temp.Name(), full)
	}
	if err != nil {
		os.Remove(temp.Name())
		return "", nil, err
	}
	return hash(text), nil, nil
}

var names = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// create writes a new definition's first line into <name>.yaml, and
// refuses a name that a file already has.
func (d *dir) create(kind, name string) (string, error) {
	if _, ok := machine.TypeOf(kind); !ok {
		return "", fmt.Errorf("%q is not a type of machine: the types are %s", kind, strings.Join(machine.Types(), ", "))
	}
	if !names.MatchString(name) {
		return "", fmt.Errorf("%q is not a name: a name is letters, digits, - and _", name)
	}
	rel := name + ".yaml"
	full, err := d.inside(rel)
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("a file named %s is already in the directory", rel)
		}
		return "", err
	}
	_, err = fmt.Fprintf(f, "%s: %s\n", kind, name)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return rel, err
}

// An entry is one machine in the list: its file, its first line and type,
// how many stages, states or tools it has, its problems, the machines it
// names, and the other files it names. Again says it is listed already, with
// what is under it, Here that it is the file open, and Shown that what is
// under it is shown: it is the file open, or above it.
type entry struct {
	Path, First, Type, Size string
	Problems                int
	Again, Here             bool
	Shown                   bool
	Under                   []entry
	Files                   []named
}

// named is a file a definition names, with the field that names it. Here
// says it is the file open.
type named struct {
	Path, Field, Icon string
	Here              bool
}

// what each type of machine is made of, as its definition writes it.
var madeOf = map[string][2]string{"flow": {"stages", "stage"}, "statechart": {"states", "state"}, "agent": {"tools", "tool"}}

// list gives the list of machines: each machine in the directory that no
// other names, those to be kept running first, leaving out those in
// directories whose names begin with a dot, and under each what it names.
// Where a directory cannot be read, it gives what it read and the error.
func (d *dir) list(open string) ([]entry, error) {
	found, err := client.ReadMachines(d.root)
	entries := map[string]entry{}
	names := map[string][]string{}
	isNamed := map[string]bool{}
	for _, m := range found {
		en, ok := d.entryOf(m.Path)
		if !ok {
			continue
		}
		en.Here = m.Path == open
		entries[m.Path] = en
		names[m.Path] = machinesOf(m.Draft)
		if m.Draft == nil {
			names[m.Path] = writtenIn(en)
		}
		for _, child := range names[m.Path] {
			isNamed[child] = true
		}
	}
	var starts []entry
	for _, rel := range slices.Sorted(maps.Keys(entries)) {
		if !isNamed[rel] && !dotted(rel) {
			starts = append(starts, entries[rel])
		}
	}
	seen := map[string]bool{}
	var follow func(en entry) entry
	follow = func(en entry) entry {
		if seen[en.Path] {
			en.Again, en.Here, en.Files = true, false, nil
			return en
		}
		seen[en.Path] = true
		en.Shown = en.Here
		for _, child := range names[en.Path] {
			if under, ok := entries[child]; ok {
				under = follow(under)
				en.Shown = en.Shown || under.Shown
				en.Under = append(en.Under, under)
			}
		}
		en.Files = slices.DeleteFunc(slices.Clone(en.Files), func(f named) bool {
			_, listed := entries[f.Path]
			return listed && slices.Contains(names[en.Path], f.Path)
		})
		for i := range en.Files {
			if en.Files[i].Path == open {
				en.Files[i].Here, en.Shown = true, true
			}
		}
		return en
	}
	for i := range starts {
		starts[i] = follow(starts[i])
	}
	// Machines that name each other, which Corbie's check refuses, are named
	// by none outside them.
	for _, rel := range slices.Sorted(maps.Keys(entries)) {
		if !seen[rel] && !dotted(rel) {
			starts = append(starts, follow(entries[rel]))
		}
	}
	return starts, err
}

// writtenIn gives the machines a machine with problems names, from its text:
// it has no draft, and what it names is still to be listed.
func writtenIn(en entry) []string {
	var files []string
	for _, f := range en.Files {
		if _, ok := madeOf[f.Icon]; ok {
			files = append(files, f.Path)
		}
	}
	return files
}

// dotted says a file is in a directory whose name begins with a dot.
func dotted(rel string) bool {
	parts := strings.Split(rel, "/")
	return slices.ContainsFunc(parts[:len(parts)-1], func(p string) bool { return strings.HasPrefix(p, ".") })
}

// fromFile gives a path as a file writes it, from the directory of machines: one
// starting ./ reads from the file's own directory, and any other from the top.
func fromFile(file, name string) string {
	if strings.HasPrefix(name, "./") {
		return path.Join(path.Dir(file), name)
	}
	return path.Clean(name)
}

// machinesOf gives the files of the machines a draft names, itself and the
// machines written in place in it, in order.
func machinesOf(draft *machine.Draft) []string {
	if draft == nil {
		return nil
	}
	var files []string
	for name, child := range draft.Children {
		if strings.HasPrefix(name, "in-place-") {
			files = append(files, machinesOf(child)...)
		} else {
			files = append(files, path.Clean(name))
		}
	}
	slices.Sort(files)
	return slices.Compact(files)
}

// firstLine gives a definition's type and name from its first line that is
// not a comment or the mark that begins a document, or nothing when it does
// not begin as a definition does.
func firstLine(src []byte) (string, string) {
	for line := range strings.Lines(string(src)) {
		line = strings.TrimSpace(line)
		if line == "" || line == "---" || strings.HasPrefix(line, "#") {
			continue
		}
		kind, name, ok := strings.Cut(line, ":")
		if _, known := machine.TypeOf(strings.TrimSpace(kind)); !ok || !known {
			return "", ""
		}
		return strings.TrimSpace(kind), strings.TrimSpace(name)
	}
	return "", ""
}

func (d *dir) entryOf(rel string) (entry, bool) {
	src, _, err := d.read(rel)
	if err != nil {
		return entry{}, false
	}
	kind, name := firstLine(src)
	if kind == "" {
		return entry{}, false
	}
	en := entry{Path: rel, First: kind + ": " + name, Type: kind}
	c := d.check(rel, src)
	en.Problems = len(c.problems)
	en.Size = sizeOf(kind, c)
	if written, err := machine.Written(src); err == nil {
		for i, asked := range c.asked {
			field := fieldOf(written, rel, asked)
			if field == "" || slices.Contains(c.asked[:i], asked) {
				continue
			}
			file := asked
			icon := "file"
			switch {
			case field == "script" || field == "turn" || field == "job":
				icon = "script"
			case strings.HasSuffix(file, ".yaml") || strings.HasSuffix(file, ".yml"):
				if data, _, err := d.read(file); err == nil {
					if k, _ := firstLine(data); k != "" {
						icon = k
					}
				}
			}
			en.Files = append(en.Files, named{Path: file, Field: field + ":", Icon: icon})
		}
	}
	return en, true
}

// sizeOf says how many stages, states or tools a machine has, in its full
// form, so an agent's tools in parts are counted.
func sizeOf(kind string, c checked) string {
	if c.draft == nil {
		return ""
	}
	var body map[string]any
	json.Unmarshal(c.draft.Definition.Body, &body)
	m, ok := body[madeOf[kind][0]].(map[string]any)
	if !ok {
		return ""
	}
	n := 0
	for _, each := range m {
		if !corbies(each) {
			n++
		}
	}
	size := fmt.Sprintf("%d %s", n, madeOf[kind][1])
	if n != 1 {
		size += "s"
	}
	return size
}

// corbies says a tool in a definition's full form is one of Corbie's own,
// which no definition writes.
func corbies(tool any) bool {
	t, _ := tool.(map[string]any)
	job, _ := t["job"].(map[string]any)
	script, _ := job["script"].(string)
	return strings.HasPrefix(script, "corbie:")
}

// partFile gives the file a part of an agent in a file is included from, or
// nothing. The agent may be written in place, and its `open:` in a file it
// includes. Where one file writes several agents of that name, the part's
// `does:` tells them apart.
func (d *dir) partFile(file, name, part, does string) string {
	var files []string
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if fmt.Sprint(v["agent"]) == name {
				found, _ := d.openedIn(v, file, part, map[string]bool{})
				files = append(files, found...)
			}
			for _, k := range sortedKeys(v) {
				walk(v[k])
			}
		case []any:
			for _, each := range v {
				walk(each)
			}
		}
	}
	src, _, err := d.read(file)
	if err != nil {
		return ""
	}
	written, err := machine.Written(src)
	if err != nil {
		return ""
	}
	walk(written)
	for _, f := range files {
		data, _, err := d.read(f)
		if err != nil {
			continue
		}
		var part struct{ Does string }
		if yaml.Unmarshal(data, &part) == nil && part.Does == does {
			return f
		}
	}
	if len(files) == 1 {
		return files[0]
	}
	return ""
}

// openedIn gives the files a part is included from in the `open:` of a mapping,
// or of the first file it includes that writes one, as Corbie reads them: a
// mapping's own field stands before a file's. A part written in place gives
// one empty name. The bool says an `open:` was found.
func (d *dir) openedIn(m map[string]any, file, part string, seen map[string]bool) ([]string, bool) {
	if open, ok := m["open"].(map[string]any); ok {
		entry, ok := open[part].(map[string]any)
		if !ok {
			return nil, true
		}
		names := includes(entry)
		if len(names) == 0 {
			return []string{""}, true
		}
		for i := range names {
			names[i] = fromFile(file, names[i])
		}
		return names, true
	}
	for _, name := range includes(m) {
		next := fromFile(file, name)
		if seen[next] {
			continue
		}
		seen[next] = true
		src, _, err := d.read(next)
		if err != nil {
			continue
		}
		written, err := machine.Written(src)
		if err != nil {
			continue
		}
		if found, ok := d.openedIn(written, next, part, seen); ok {
			return found, true
		}
	}
	return nil, false
}

// includes gives the files a mapping's `include:` names: one, or a list.
func includes(m map[string]any) []string {
	switch v := m["include"].(type) {
	case string:
		return []string{v}
	case []any:
		var names []string
		for _, each := range v {
			if name, ok := each.(string); ok {
				names = append(names, name)
			}
		}
		return names
	}
	return nil
}

// sortedKeys gives a map's keys in order.
func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

// fieldOf gives the field where a definition writes a path: the key that
// holds it, or holds a list that does. It is nothing when the definition
// does not write the path itself, as for a file its child names.
func fieldOf(written any, file, asked string) string {
	switch v := written.(type) {
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(v)) {
			switch item := v[k].(type) {
			case string:
				if fromFile(file, item) == asked {
					return k
				}
			case []any:
				for _, each := range item {
					if s, ok := each.(string); ok && fromFile(file, s) == asked {
						return k
					}
				}
			}
			if f := fieldOf(v[k], file, asked); f != "" {
				return f
			}
		}
	case []any:
		for _, each := range v {
			if f := fieldOf(each, file, asked); f != "" {
				return f
			}
		}
	}
	return ""
}
