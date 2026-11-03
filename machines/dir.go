package machines

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

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
// error when the path leaves the directory, once links are followed.
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
	if err != nil {
		return "", err
	}
	if real != d.real && !strings.HasPrefix(real, d.real+string(filepath.Separator)) {
		return "", errOutside
	}
	return full, nil
}

// read reads a file inside the directory, and gives its hash, by which a
// page knows the file changed on disk.
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

// stamp gives the name, size and time of change of every file in the
// directory, by which the index knows the directory changed without reading
// a machine.
func (d *dir) stamp() string {
	sum := sha256.New()
	err := filepath.WalkDir(d.real, func(full string, e fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case e.IsDir() && e.Name() == ".git":
			return filepath.SkipDir
		case e.IsDir():
			return nil
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(sum, "%s\x00%d\x00%d\n", full, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		io.WriteString(sum, err.Error())
	}
	return hex.EncodeToString(sum.Sum(nil))
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
