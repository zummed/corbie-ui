# Runs

The Runs page lists runs, and a run's page shows one. A run has Corbie's one shape for a run and the words `corbie run show` prints, wherever the UI shows it.

## The list

The list holds every run whose status the UI may read. The UI lists as a viewer where the store lets a viewer read, as `corbie run ls --all` does with no client: every client's runs on a directory, and on a bucket with a viewer's key. Where the store does not, as on a served directory or a bucket with a client's key, it lists its client's own, as `corbie run ls --all` does for that client.

Each run is one line, with its machine's type as an icon beside the machine's name:

```
running  review   laptop-client/20261015T091204.118Z-3be1c07a2f   4   1 timer, ticket verdict   doc=q3-report   09:12
```

A running child is listed under its running parent, and every other run on its own. Below the running runs are those that ended in the timeline's window, with when each ended and how long it took.

The page is narrowed by status and by label, as `--status` and `--label` narrow `corbie run ls`, and by client, the first part of a run's ID, which `corbie run ls` has no flag for. The counts, the timeline and the list show only the runs the narrowing leaves. Each status is offered with its count.

## The timeline

Above the list, the timeline draws the runs of a window of time that ends now.

- Each machine with a run in the window has a lane, in the order of the machines' names. The lanes can instead be by client, or by the value of one label, such as `team=`, with the runs that lack the label in a last lane.
- Each run is a bar in its lane, from when it started to when it ended, in its status's colour. The bar of a running run is drawn up to now, and runs that overlap in a lane are drawn one under another.
- The window is 1h, 6h, 24h or 7d, and 6h by default. An ended run is kept for 7d by default.
- Beside the timeline is the number of runs running, and of the jobs, timers and tickets they wait on.

Pointing at a bar lights the run's line in the list, and pointing at a line lights its bar. A bar opens the run's page, as its line does. The address of the page holds the window, the lanes and the narrowing.

## One run

A run's page opens from its line in the list. Its top line is the run: its machine, type, status and key, then its ID, when it started, and when it ended and how long it took. Under it is the run's path, each run a link:

```
chat › message › answer › helper › approval › ask › question
```

The path goes up from the run, by parent, to its root, and down to the deepest running run, by the newest running child at each level. The run itself is in bold.

- The drawing of its machine shows the state it is in (see Drawing).
- What it waits on is listed, and its labels below that.
- Record lists every entry as it is written, its children's included, in the words `corbie run show --record` prints. Each entry is named by its run's machine, and the line a run ends with is in its status's colour.
- Output shows the output, or the error; a denial shows what was refused, the layer that refused it, and the fix. The files of its result are listed below, each a download, as `corbie run show --save` fetches it, and never shown in a page.
- Children lists the runs it started, with each one's status and when it started or ended.

A running run waits on these:

```
ticket verdict                      a ticket, with its value to hand to whoever should answer
child question                      a running child, linked
job find                            a script on a runner
held tell                           a tool held for the ask
timer unanswered due 10 Nov 20:29   a timer, by when it is due
events at key question:9de2a8913b   events sent to its key
```

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
