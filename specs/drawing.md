# Drawing

A machine is drawn top to bottom from its definition in full form, read with Corbie's own types for a flow, a statechart and an agent, and a run's state is shown on the drawing. The UI draws it as SVG, at the size it needs. The Machines page makes it narrower to fit its panel, and never larger; a run's page draws it at its size, in a panel that scrolls. The same drawing is on a run's page and on the Machines page.

Every card has one width and one corner radius: an icon for what its job is, its name, and one line for each fact about the job, such as `script: ./who.js` or `flow: who`. A card's tooltip gives its `when:` and a tool's `does:`. A holder is drawn round cards: a statechart's state that holds states, an agent's tools, and each of its parts.

A card whose job names a machine shows the machine's type as its icon and `type: name` as its line. The Machines page knows the machine from its file, and a run's page from the definition the run was started from. A machine known by neither is written as the definition names it, `machine: ./who.yaml`.

An arrow goes straight down. Arrows from one card, or into one row, run along a rail above the row, so a branch is one line that divides. A drawing has as few arrows as say what it does.

## A flow

- A stage is a card.
- An arrow goes to a stage from each stage it waits for: each stage an expression of it reads, whether its output, its error, whether it failed or was skipped, or its emitted items; each stage it names in `follows:`; and the stage it names in `emits:`. No arrow is drawn from a stage it waits for through another.
- Stages are placed top to bottom in the order the arrows give, and stages that run at the same time are placed side by side.
- A stage with `each:`, `emits:` or `blob:` is drawn as a stack of cards, and a stage with `when:` has a dashed border.

Which stage waits for which is the flow package's rule, which it keeps to itself. The UI applies the same rule to the same expressions, read with Corbie's `machine.Reads`, `machine.ReadsIn` and `machine.ReadsOf`.

## A statechart

- A state is a card, and a state that holds states is a holder drawn around them. A state with no job has no icon.
- States are placed top to bottom by how many transitions each is from the state its level starts in.
- The state each level starts in has a short arrow into it.
- A final state is a narrow card with a double border, drawn in rowan for `end: failed`.
- An arrow goes from a state to each later state its `on:` names, one arrow for all the rules between the two, with their events and conditions as written on a chip, shortened to fit, and whole in its tooltip: `verdict, when event.data.ok`. A timer's arrow is dashed and its chip gives its time: `too_long, 48h`.
- A rule to an earlier state, or to the same state, has no arrow. It is written in the card: `done, failed ↩ waiting`, or `done ↻`.
- A state's ticket and timers are written in its card.
- A `send:` that names a machine is a card of its own, below the state, with a dashed arrow whose chip gives the event.

## An agent

An agent is three things, one below another, joined by two arrows.

1. The model is a card, holding the model's name.
2. Its tools are in one holder. Each part is a holder inside it, with its name, its tools and its ask, and on the Machines page its tooltip gives the file it is included from. Tools in no part are cards beside the parts, and the agent's ask is a card below them. The chip on the arrow into the holder gives how many turns the agent has.
3. `reply` is a narrow card.

A tool with `approve: true` says so in its card. Tools that are Corbie's own, whose script begins `corbie:`, are not drawn.

## A machine inside a machine

On a run's page, a card whose job started a run links to that run's page.

On the Machines page, a card whose job names a machine is a link, and so is the card of a `send:`. The link unfolds that machine's drawing inside the card, and the card then has the link that folds it again. Any number can be unfolded at once, at any depth, and the address names them, so a person can send the address to another.

A job whose machine is an expression is known only when it runs, and has no link.

## A run on the drawing

Each machine type shows a watcher its state, in each step of the record, and the drawing marks what it shows. A run is therefore drawn only when the UI may read its record.

A run is drawn as of one step: the step selected in Steps. Its marks come from its record up to that step, and the jobs going and how the run stood come from the state that step left, so a later step changes nothing on it.

```
flow         each stage's status: waiting, running, done, failed or skipped
statechart   the states it is in; states it was in before are outlined in moss, and states never entered faintly
agent        the turn it is on; each tool and ask going now, held for the ask, used at an earlier step, or never used
```

A run that ended is marked by how it ended. A stage still running when it ended is outlined in rowan where the run failed, and grey where it was cancelled. In a failed run, the states it ended in are outlined in rowan, and so is the card of the job whose failure ended it, such as the model of an agent whose turn failed.

A statechart's run is followed through its steps: from the states it was in before a step, by the events the step took, to the states it is in after. A state it passed through within one step was entered, such as the state it started in and left at its first step. Where an event has rules to several states, the rule taken is the one to a state the run is in after the step.

The transition the step drawn took is drawn in petrol, heavier than the other arrows and over any arrow it shares a line with, or written in petrol in its card. The states a run that did not fail ended in are outlined in moss, as states it was in.

A part of an agent is folded where the run has not asked for its tools or its ask by the step drawn. A folded part is a small holder with its name and how many tools it has, `2 tools`, and its tooltip names them. The folded parts are one below another, in one column after the others, so the parts the run uses are drawn side by side and in view together.

An agent's ask is going while a tool waits for its approval. The tool is outlined in amber, as held, and the ask as going now: a part's ask where the tool is in that part, and the agent's otherwise. A run knows each ask by where it is written, so the run an ask started is linked from that ask's card alone.
