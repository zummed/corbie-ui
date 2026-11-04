# Runs

The Runs page lists runs, and a run's page shows one. A run has Corbie's one shape for a run and the words `corbie run show` prints, wherever the UI shows it.

## The list

The list reads every run whose status the UI may read. The UI lists as a viewer where the store lets a viewer read, as `corbie run ls --all` does with no client: every client's runs on a directory, and on a bucket with a viewer's key. Where the store does not, as on a served directory or a bucket with a client's key, it lists its client's own, as `corbie run ls --all` does for that client.

A root is a running run whose parent is not running. Each root is one line in Busy, Idle or Services, and Ended lists the runs with no parent that ended:

```
Busy       a root with a running child, a job on a runner or a ticket, or with status waiting
Idle       any other root: no running child, no job on a runner and no ticket
Services   a root whose key begins file:, as corbie run submit of a directory starts it, whatever it waits on
Ended      a run with no parent that ended in the window, newest first
```

The window is 1h, 6h, 24h or 7d, and 6h by default. A line of Busy, Idle or Services reads:

```
running  slack:D0CFF1XQXLY  chat › … › ask › question [open, waiting]  timer unanswered due 10 Nov 20:29  20:29  old version
```

- The title is the root's key, or its machine's name where it has none. A long title is cut in its middle, since keys differ at their end.
- The chain runs from the root to the deepest running run, through the newest running child at each level. Each machine has its type's icon and opens its run's page.
- A chain of more than four machines shows the first, `…` and the last two, and the `…` gives the names between in its tooltip.
- The brackets after the deepest machine hold the states or stages it is in, or what an agent waits for.
- Then come what the deepest run waits on and when the root started. Events at the root's key are left out, since the title is that key.

A line unfolds to the root's running tree, one line for each run, indented under its parent, each with what it waits on and when it started. An ended line holds its status, title, machine, the number of runs under it, when it ended and how long it took, and unfolds to those runs, each with when it ended.

What a run waits on is written as:

```
ticket <name>               a ticket
timer <name> due <time>     a timer; a run another client started shows the number of timers
job <name>                  a job on a runner; a run another client started shows the number of jobs
held <name>                 a job held for the ask
child <machine>             a running child
events at key <key>         a statechart with a key
```

The names and times come from the latest step of a run the UI's client started, read once for each step.

### Old versions

`corbie run submit` of a directory labels each run `machine=<path>` and `hash=<hash>`, and the runs under it and those its sends start carry the same labels. Each client has its own current hash of a path: the `hash=` of the newest run with no parent that the client keyed `file:<path>`. A root of that client whose `hash=` differs is marked `old version`.

Where Idle holds roots on old versions that the UI's client started, its title line offers one button, `Cancel the 14 idle runs on old versions`. It runs `corbie run cancel <id>` for each, and holding the pointer over it shows every command.

The page is narrowed by label, as `--label` narrows `corbie run ls`, and by client, the first part of a run's ID, which `corbie run ls` has no flag for. The client is chosen only where the list holds more than one, or the address names one. A root or ended run the narrowing leaves is listed with every run under it.

## One run

A run's page opens from its line in the list. Its title line holds the run: its machine, type, status and key, then its ID, when it started, and when it ended and how long it took. Under it is the run's path, each run a link:

```
slack-listen ⇢ chat › message › answer › helper › approval › ask › question
```

The path goes up from the run, by parent, to its root, and down to the deepest running run, by the newest running child at each level. Where a send to a key started the root, the run that sent it comes first, with `⇢`. The run itself is in bold, and the run of the step selected is lit.

On the left are three tabs: Steps, Output and Children. On the right are the drawing, what the run waits on, and the details of the step selected.

### Steps

Steps lists what happened in the run and in every run under it, one row each, read from the record as `corbie run show --record` reads it. A row holds its time, its run's machine in the colour of the machine's type, and what happened:

```
statechart   entered answering · left asking, entered open and waiting · timer unanswered set, due 10 Nov 20:29 · timer unanswered fired · state answering started message
flow         stage who started who · stage who done · stage refuse skipped · stage brief failed
agent        turn 2 to claude-sonnet-5-5 · turn 2 called tell(person: “zummed”, text: “The…”) · open done · held tell for approval · ask started approval
any run      started · done · failed (denied) · cancelled
a send       ⇢ sent answer to the key question:1d3473b2cc · question       in the run that sent it
             ⇠ message from slack-listen                                  in the run it reached
```

- A child's rows are under the row that started it, one level in, with a faint line at each level. Rows deeper than four levels are drawn at the fourth, and the path gives their depth.
- A send to a key has a dashed mark and links to the run that held the key when it was sent. The row in the run it reached links to the sender.
- The row that started a child ends with the child's status, its steps and those of the runs under it, and how long it took: `stage helper started helper · running · 22 steps`. The child's rows fold into that row.
- An ended child is folded, and a running one open. Open all and Fold all fold every group.
- On the page of a run another client started, there are no Steps.

One row is selected. On a running run it is the newest row until the person selects another; on an ended run it is the last.

```
↑ ↓      selects the row above or below, of those shown
←        folds the group the row starts, or the group it is in
→        opens the group the row starts
click    selects the row and shows its details
```

The drawing shows the selected row's run as it was at the row's step, from its record up to that step and the state that step left (see Drawing). The drawing is at its own size, and its panel scrolls.

The details of a row have four tabs, and Open opens the row's run's page:

```
What happened   the row's facts: the job and its machine or script, a tool's arguments, the run a tool is held for, the turn, the error
Input           what the row's job, call or event was given
Output          what it gave back
Record          the entries of the row's step, in the words corbie run show --record prints, each named by its run's machine
```

### Waiting on, Output and Children

- What the run waits on is listed as in the list, with each running child linked and each ticket's value, and its labels below. Events at the run's own key are left out, since its title line gives the key.
- Output shows the output, or the error; a denial shows what was refused, the layer that refused it, and the fix. The files of its result are listed below, each a download, as `corbie run show --save` fetches it, and never shown in a page.
- Children lists the runs it started, with each one's status and when it started or ended.

A run that another client started shows its status, its path, its children and its labels, and nothing from its record. Its jobs and timers are counted and its tickets named.

## Acting on a run

```
Reply    answers a ticket, by its value, with an answer   corbie run reply <id> <ticket> <answer>
Send     sends the run an event, with data                corbie run send <id> <event> <data>
Cancel   cancels the run                                  corbie run cancel <id>
```

Send is on the page of a running statechart the UI's client started, and of no other run.

On a run the UI's client started, each ticket's value is filled in. On any other running run, Reply takes a ticket's value typed in, as a client that was handed one answers it. An answer and an event's data are typed as JSON.

With the controller among the UI's parties, Cancel is on every running run's page, and on a run another client started it is `corbie controller cancel <id>`.

The page shows the answer the command prints. For Reply, Send and Cancel that is the receipt: taken, or refused with the reason, and for an event whether the machine acted on it. `corbie controller cancel` does not wait for a receipt, and the page says the cancel is in the controller's inbox.
