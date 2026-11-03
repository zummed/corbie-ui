package machines

import (
	"errors"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/zummed/corbie/machine"
	"github.com/zummed/corbie/secret"
	"go.yaml.in/yaml/v3"
)

// A problem is one of Corbie's, at the file it names: the file, the place in
// it and its line where the text writes the place, what is wrong, and the
// machines of the project it breaks. Top says the file is a directory below
// the one the UI reads that is the top of a project.
type problem struct {
	File, Place, Text string
	Line              int
	Breaks            []string
	Top               bool
}

// problemsOf gives the problems Corbie found reading the machine in a file,
// each at the file it names, with its place as that file writes it and its
// paths from the directory.
func (d *dir) problemsOf(file string, err error) []problem {
	from := strings.NewReplacer(d.real+string(filepath.Separator), "", d.root+string(filepath.Separator), "")
	var found machine.Problems
	if !errors.As(err, &found) {
		return []problem{{File: file, Text: from.Replace(err.Error())}}
	}
	var out []problem
	for _, p := range found {
		at := file
		if p.File != "" {
			at = path.Clean(strings.TrimPrefix(p.File, "./"))
		}
		pr := problem{File: at, Place: p.Place, Text: from.Replace(p.Text)}
		if src, _, err := d.read(at); err == nil {
			pr.Place, pr.Line = placeIn(src, p.Place)
		}
		out = append(out, pr)
	}
	return out
}

var placeParts = regexp.MustCompile(`([^.\[\]]+)|\[(\d+)\]`)

// placeIn gives a place as a file writes it, and the line where it does, or 0
// where the text does not write it. A place in a file that is included is
// given from the machine that includes it, so its first fields are left off
// until the rest is found in the text.
func placeIn(src []byte, place string) (string, int) {
	var doc yaml.Node
	if place == "" || strings.Contains(place, ": ") || yaml.Unmarshal(src, &doc) != nil || len(doc.Content) == 0 {
		return place, 0
	}
	parts := placeParts.FindAllStringSubmatch(place, -1)
	best, line, at := 0, 0, 0
	for from := range parts {
		found, l := walk(doc.Content[0], parts[from:])
		if found > best {
			best, line, at = found, l, from
		}
		if found == len(parts)-from {
			break
		}
	}
	if at == 0 {
		return place, line
	}
	return strings.TrimPrefix(place[len(joined(parts[:at])):], "."), line
}

// joined writes parts of a place as the place writes them.
func joined(parts [][]string) string {
	var b strings.Builder
	for i, p := range parts {
		if p[2] != "" {
			b.WriteString("[" + p[2] + "]")
			continue
		}
		if i > 0 {
			b.WriteString(".")
		}
		b.WriteString(p[1])
	}
	return b.String()
}

// walk follows the parts of a place down a YAML node, and gives how many it
// found and the line of the last.
func walk(n *yaml.Node, parts [][]string) (int, int) {
	found, line := 0, 0
	for _, m := range parts {
		if m[2] != "" {
			i, _ := strconv.Atoi(m[2])
			if n.Kind != yaml.SequenceNode || i >= len(n.Content) {
				return found, line
			}
			n = n.Content[i]
			found, line = found+1, n.Line
			continue
		}
		if n.Kind != yaml.MappingNode {
			return found, line
		}
		next := (*yaml.Node)(nil)
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == m[1] {
				line, next = n.Content[i].Line, n.Content[i+1]
				break
			}
		}
		if next == nil {
			return found, line
		}
		n, found = next, found+1
	}
	return found, line
}

// secretsNamed gives the secrets a definition's text names: each NAME of a
// secret:NAME@… entry in any allow list, the definition's own and each
// job's, once, in the order written. An entry whose name holds * names
// none.
func secretsNamed(src []byte) []string {
	var doc yaml.Node
	if yaml.Unmarshal(src, &doc) != nil {
		return nil
	}
	var allow []string
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n.Kind != yaml.MappingNode {
			for _, c := range n.Content {
				walk(c)
			}
			return
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Value == "allow" && v.Kind == yaml.SequenceNode {
				for _, item := range v.Content {
					allow = append(allow, item.Value)
				}
			} else {
				walk(v)
			}
		}
	}
	walk(&doc)
	return secret.Named(allow)
}
