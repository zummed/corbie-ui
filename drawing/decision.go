package drawing

import (
	"slices"
	"strings"

	"github.com/zummed/corbie/machine/agent"
)

// drawDecision draws two things, one below the other: the model, and the
// answers, with each question's name.
func drawDecision(dec *agent.Decision, run *Run) drawn {
	name := dec.Model.Name
	if name == "" {
		name = "model"
	}
	answers := item{name: "answers"}
	if questions, ok := dec.Questions.(map[string]any); ok {
		names := make([]string, 0, len(questions))
		for question := range questions {
			names = append(names, question)
		}
		slices.Sort(names)
		for _, question := range names {
			answers.lines = append(answers.lines, fit(question))
		}
	}
	model := item{icon: "model", name: name}
	top, below := model.size(), answers.size()
	total := max(top.w, below.w)
	top.x, top.y = margin+(total-top.w)/2, margin+18
	below.x, below.y = margin+(total-below.w)/2, top.bottom()+2*rowGap

	var d drawn
	d.down(top, below, "", "")
	mark := jobMark(run, "decide")
	if mark != "" && mark != "failed" && mark != "now" {
		mark = "done"
	}
	d.paint(model, top, strings.TrimSpace("state model "+mark), cardR, "")
	d.paint(answers, below, "state reply", cardR, "")
	return d
}
