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
)

// A dir is the directory of machines: every path a request names is inside
// it, once links are followed.
type dir struct {
	root, real string
}

func newDir(root string) (*dir, error) {
	full, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(full)
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

// files gives every file in the directory and below it, but for .git, by
// its path from the directory, as Corbie reads a tree.
func (d *dir) files() ([]string, error) {
	var found []string
	err := filepath.WalkDir(d.real, func(full string, e fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case e.IsDir() && e.Name() == ".git":
			return filepath.SkipDir
		case e.IsDir():
			return nil
		}
		rel, err := filepath.Rel(d.real, full)
		if err != nil {
			return err
		}
		found = append(found, filepath.ToSlash(rel))
		return nil
	})
	return found, err
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

// sortedKeys gives a map's keys in order.
func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}
