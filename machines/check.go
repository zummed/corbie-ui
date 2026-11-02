package machines

import (
	"errors"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/zummed/corbie/machine"
	"github.com/zummed/corbie/secret"
	"go.yaml.in/yaml/v3"
)

// checked is a definition as Corbie's check reads it: its problems, the
// files it asked for, and its draft when it has no problems.
type checked struct {
	problems []problem
	asked    []string
	draft    *machine.Draft
}

// A problem is one of Corbie's, with the line of its place where the place
// is found in the text.
type problem struct {
	Place, Text string
	Line        int
}

// check checks a definition's text with Corbie's own check, reading the
// files it names as `corbie run submit` reads them: the directory of machines
// is the context, and a path may name any file inside it and none outside it.
func (d *dir) check(rel string, src []byte) checked {
	var c checked
	load := func(name string) ([]byte, error) {
		c.asked = append(c.asked, name)
		data, _, err := d.read(name)
		return data, err
	}
	draft, err := machine.ParseIn(path.Dir(rel), src, load)
	var problems machine.Problems
	switch {
	case errors.As(err, &problems):
		for _, p := range problems {
			c.problems = append(c.problems, problem{Place: p.Place, Text: p.Text, Line: lineOf(src, p.Place)})
		}
	case err != nil:
		c.problems = append(c.problems, problem{Text: err.Error()})
	default:
		c.draft = draft
	}
	return c
}

var placeParts = regexp.MustCompile(`([^.\[\]]+)|\[(\d+)\]`)

// lineOf gives the line where a place of a definition is written, as far
// as the text writes it, or 0 when it does not, as for a place in a file
// the definition names.
func lineOf(src []byte, place string) int {
	if place == "" || strings.Contains(place, ": ") {
		return 0
	}
	var doc yaml.Node
	if yaml.Unmarshal(src, &doc) != nil || len(doc.Content) == 0 {
		return 0
	}
	n, line := doc.Content[0], 0
	for _, m := range placeParts.FindAllStringSubmatch(place, -1) {
		if m[2] != "" {
			i, _ := strconv.Atoi(m[2])
			if n.Kind != yaml.SequenceNode || i >= len(n.Content) {
				return line
			}
			n = n.Content[i]
			line = n.Line
			continue
		}
		if n.Kind != yaml.MappingNode {
			return line
		}
		found := false
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == m[1] {
				line, n, found = n.Content[i].Line, n.Content[i+1], true
				break
			}
		}
		if !found {
			return line
		}
	}
	return line
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

// A fieldView is a field that can be written where the cursor is: its
// name, an example of how it is written, what it is for, and whether it is
// written there already.
type fieldView struct {
	Name, Like, Does string
	Here             bool
}

// fieldsAt gives what is written at a line of a definition, as its type's
// shapes say: the shape's name and its fields.
func fieldsAt(src []byte, kind string, line int) (string, []fieldView) {
	desc, ok := machine.Describe(kind)
	if !ok {
		return "", nil
	}
	var doc yaml.Node
	if yaml.Unmarshal(src, &doc) != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return "", nil
	}
	n := doc.Content[0]
	fields := append(append([]machine.Field{}, desc.Shapes["definition"].Fields...), desc.Shapes[kind].Fields...)
	what := "a definition"
	named := false // the next key names a thing, and is not a field
	for {
		key, value := under(n, line)
		if value == nil {
			break
		}
		if named {
			named, n = false, value
			continue
		}
		var f *machine.Field
		for i := range fields {
			if fields[i].Name == key {
				f = &fields[i]
			}
		}
		shape := (*machine.Shape)(nil)
		if f != nil {
			shape = f.Holds()
		}
		if shape == nil {
			break
		}
		for value.Kind == yaml.SequenceNode {
			at := value
			value = nil
			for _, item := range at.Content {
				if item.Line <= line {
					value = item
				}
			}
			if value == nil {
				return what, viewsOf(fields, n)
			}
		}
		if value.Kind != yaml.MappingNode {
			break
		}
		n, fields, what = value, shape.Fields, shape.What
		named = f.Kind == machine.KindNamed
	}
	if named {
		return what, nil
	}
	return what, viewsOf(fields, n)
}

// under gives the key of a mapping whose value holds a line after the
// key's own, and that value.
func under(n *yaml.Node, line int) (string, *yaml.Node) {
	if n.Kind != yaml.MappingNode {
		return "", nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		end := lastLine(v)
		if (v.Kind == yaml.MappingNode || v.Kind == yaml.SequenceNode) && line >= v.Line && line <= end && !(line == k.Line && v.Line != k.Line) {
			return k.Value, v
		}
	}
	return "", nil
}

func lastLine(n *yaml.Node) int {
	last := n.Line
	for _, c := range n.Content {
		last = max(last, lastLine(c))
	}
	return last
}

func viewsOf(fields []machine.Field, n *yaml.Node) []fieldView {
	written := map[string]bool{}
	if n.Kind == yaml.MappingNode {
		for i := 0; i < len(n.Content); i += 2 {
			written[n.Content[i].Value] = true
		}
	}
	var out []fieldView
	for _, f := range fields {
		out = append(out, fieldView{Name: f.Name, Like: f.Like, Does: f.Does, Here: written[f.Name]})
	}
	return out
}
