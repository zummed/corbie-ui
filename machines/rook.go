package machines

import (
	"slices"

	"github.com/zummed/corbie/machine"
	"go.yaml.in/yaml/v3"
)

// rules is what the Parts page reads of a Rook project's rules.yaml.
type rules struct {
	Agents []struct {
		Name string
		For  []string
	}
	Approve map[string][]string
}

// rulesOf reads rules.yaml, or gives nothing in a directory that is not a Rook
// project: one with no `.rook/`.
func (d *dir) rulesOf() (*rules, error) {
	rook, err := d.isDir(".rook")
	if !rook {
		return nil, err
	}
	src, _, err := d.read("rules.yaml")
	if err != nil {
		return nil, err
	}
	var r rules
	if err := yaml.Unmarshal(src, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// approveName gives the name a part's approvers are listed under: the `part:`
// of its ask's input, which Rook's approval reads, by default its name.
func approveName(name string, ask *machine.JobSpec) string {
	if ask == nil {
		return name
	}
	input, _ := ask.Input.(map[string]any)
	if part, ok := input["part"].(string); ok && !machine.HasExpr(part) {
		return part
	}
	return name
}

// approvers gives the names under `approve:` for each name given.
func (r *rules) approvers(names []string) []string {
	var who []string
	for _, name := range names {
		who = added(who, r.Approve[name])
	}
	return who
}

// forAny gives the `for:` of each entry under `agents:` that names one of the
// machines given.
func (r *rules) forAny(names []string) []string {
	var who []string
	for _, a := range r.Agents {
		if slices.Contains(names, a.Name) {
			who = added(who, a.For)
		}
	}
	return who
}

// added adds names to a list, each once.
func added(list, names []string) []string {
	for _, name := range names {
		if !slices.Contains(list, name) {
			list = append(list, name)
		}
	}
	return list
}
