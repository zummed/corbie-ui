package runs

import (
	"encoding/json/v2"
	"fmt"
	"strconv"
	"strings"

	"github.com/zummed/corbie/client"
	"github.com/zummed/corbie/instance"
)

// A line is one line of a run's record, in the words `corbie run show
// --record` prints: when, which instance, the step, which way it went, and
// what.
type line struct {
	Key   string
	Time  string
	Short string
	Step  int
	Way   string // "in" for what arrived, "out" for what the step asked
	Text  string
	End   bool
}

// lines gives an entry of a record as Corbie's command line prints it,
// which it keeps to itself.
func lines(e client.Entry) []line {
	at := e.Time.Local().Format("15:04:05")
	short := e.ID[strings.LastIndexByte(e.ID, '-')+1:]
	key := func(i int) string { return e.ID + "/" + strconv.Itoa(e.Step) + "/" + e.Job + "/" + e.Name + "/" + strconv.Itoa(i) }
	if e.Step == 0 {
		data, _ := json.Marshal(e.Data, json.Deterministic(true))
		text := string(data)
		if s, ok := e.Data.(string); ok {
			text = s
		}
		return []line{{Key: key(0), Time: at, Short: short, Text: fmt.Sprintf("%s %s: %s", e.Job, e.Name, text)}}
	}
	var out []line
	add := func(way, text string) {
		out = append(out, line{Key: key(len(out)), Time: at, Short: short, Step: e.Step, Way: way, Text: text})
	}
	for _, event := range e.Events {
		text := event.Name
		if event.From != "" {
			text += " from " + event.From
		}
		if failed, _ := event.Data.(map[string]any); failed["refused"] != nil {
			text += fmt.Sprintf(" denied: %v", failed["refused"])
		} else if event.Data != nil {
			data, _ := json.Marshal(event.Data, json.Deterministic(true))
			text += " " + clip(string(data))
		}
		add("in", text)
	}
	for _, ask := range e.Asks {
		text := ""
		switch ask.Kind {
		case instance.AskScript:
			text = fmt.Sprintf("job %s: a script, on %s", ask.Name, ask.Runner)
		case instance.AskChild:
			text = fmt.Sprintf("job %s: a machine, %s", ask.Name, ask.Child)
		case instance.AskCancel:
			text = "cancel " + ask.Name
		case instance.AskEvent:
			to := "its parent"
			if ask.Name != "" {
				to = ask.Name
			}
			text = fmt.Sprintf("send %s to %s", ask.Event.Name, to)
			if ask.Undelivered {
				text += " (undelivered: it is not running)"
			}
		case instance.AskFail:
			text = fmt.Sprintf("%s fails: %s", ask.Name, ask.Error.Error)
			if ask.Error.Refused != "" {
				text = fmt.Sprintf("%s fails, denied: %s", ask.Name, ask.Error.Refused)
			}
		case instance.AskHold:
			text = fmt.Sprintf("hold %s for the ask", ask.Name)
		case instance.AskNote:
			text = ask.Text
		default:
			continue
		}
		add("out", text)
	}
	if e.Status != instance.Running {
		add("", e.Status)
		out[len(out)-1].End = true
	}
	return out
}

func clip(text string) string {
	if len(text) > 200 {
		return text[:200] + "…"
	}
	return text
}
