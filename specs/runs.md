# Runs

The Runs page lists runs, and a run's page shows one. A run has Corbie's one shape for a run and the words `corbie run show` prints, wherever the UI shows it.

## The list

The list reads every run whose status the UI may read. The UI lists as a viewer where the store lets a viewer read, as `corbie run ls --all` does with no client: every client's runs on a directory, and on a bucket with a viewer's key. Where the store does not, as on a served directory or a bucket with a client's key, it lists its client's own, as `corbie run ls --all` does for that client.

A root is a running run whose parent is not running. Each root is one line, in one of four sections:

```
Services   a root whose key begins file:, as corbie run submit of a directory starts it
Busy       a root with a running child, a job on a runner or a ticket, or with status waiting
Idle       any other root: one that waits only on timers
Ended      a run with no parent that ended in the window, newest first
```

The window is 1h, 6h, 24h or 7d, and 6h by default. A line of the first three sections reads:

```
running  slack:D0CFF1XQXLY  chat › message › answer › helper › approval › ask › question [open, waiting]  timer unanswered due 10 Nov 20:29, events at key question:9de2a8913b  20:29  old version
```

- The title is the root's key, or its machine's name where it has none.
- The chain runs from the root to the deepest running run, through the newest running child at each level. Each machine has its type's icon and opens its run's page.
- The brackets after the deepest machine hold the states or stages it is in, or what an agent waits for.
- Then come what the deepest run waits on and when the root started.

A line unfolds to the root's running tree, one line for each run, indented under its parent, each with what it waits on. An ended line holds its status, title, machine, the number of runs under it, when it ended and how long it took, and unfolds to those runs.

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

`corbie run submit` of a directory labels each run `machine=<path>` and `hash=<hash>`, and the runs under it and those its sends start carry the same labels. The newest root keyed `file:<path>` has the current hash of that path, and a root whose `hash=` differs is marked `old version`.

Where Idle holds roots on old versions that the UI's client started, its head offers one button, `Cancel the 14 idle runs on old versions`. It runs `corbie run cancel <id>` for each, and holding the pointer over it shows every command.

The page is narrowed by label, as `--label` narrows `corbie run ls`, and by client, the first part of a run's ID, which `corbie run ls` has no flag for. The client is chosen only where the list holds more than one, or the address names one. A root or ended run the narrowing leaves is listed with every run under it.

## One run

A run's page opens from its line in the list. Its top line is the run: its machine, type, status and key, then its ID, when it started, and when it ended and how long it took. Under it is the run's path, each run a link:

```
chat › message › answer › helper › approval › ask › question
```

The path goes up from the run, by parent, to its root, and down to the deepest running run, by the newest running child at each level. The run itself is in bold.

- The drawing of its machine shows the state it is in (see Drawing).
- What it waits on is listed as in the list, with each running child linked and each ticket's value, and its labels below that.
- Record lists every entry as it is written, its children's included, in the words `corbie run show --record` prints. Each entry is named by its run's machine, and the line a run ends with is in its status's colour.
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
