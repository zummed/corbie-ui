# Runs

The Runs page lists runs, and a run's page shows one. A run has Corbie's one shape for a run and the words `corbie run show` prints, wherever the UI shows it.

## Names

Every run has a location: the location of the file it was started from, or, for a child that `machine:` started, its parent's. A run at a keyed location has a key. The UI names a run by the first of these it has:

```
title     the definition's title, read from the record of a run in a tree of the UI's client
address   its location, with #key at a keyed location: /home/rooms/thermostat#kitchen
machine   its machine's name, for a child that machine: started
```

- A run in another client's tree, and every run of a UI that acts as no client, is named by its address, or by its machine where it is a child.
- On a page narrowed to a prefix, an address is written from that prefix: at `/home`, `rooms/thermostat#kitchen`. A location the prefix names in full keeps its last name: at `/home/rooms/thermostat`, `thermostat#kitchen`.
- Holding the pointer over a name shows the run's title, its full address and its ID.
- The UI reads each title once, from the step the run's head gives, while the list holds the run. A run's title, tree and lifetime never change once it has started.

## The list

The list reads every run whose status the UI may read. The UI lists as a viewer where the store lets a viewer read, as `corbie run ls --all` does with no client: every client's runs on a directory, and on a bucket with a viewer's key. Where the store does not, as on a served directory or a bucket with a client's key, it lists its client's own, as `corbie run ls --all` does for that client.

### Locations

On the left, Locations lists every location at which the list holds a run, under the client whose tree holds it. A location the UI knows only from the runs listed: a location with no run is not shown.

```
lab-client
  /home                    tree
    devices/boiler         singleton   running
    rooms/thermostat       keyed       3 running
    scenes/away            transient
ci-client
  /work/digest             keyed       1 running
```

- A tree of the UI's client is a row of its own, its prefix read from the record of a run there, with its locations written from the prefix under it.
- A location in another client's tree is written in full under that client, since only a tree's client reads which tree a run is in.
- A location's lifetime is read from the record of a run there, where its tree is the UI's client's. Elsewhere a location with a keyed run is `keyed`, and any other shows none.
- A singleton shows its run's status. Any other location shows how many of its runs are running.
- A click on a tree or a location narrows the page to it, as `?at=<prefix>`. The first row, every location, takes the narrowing away.

### Sections

A root is a running run whose parent is not running. Each root is one line in Busy, Idle or Singletons, and Ended lists the runs with no parent that ended:

```
Busy         a root with a running child, a job on a runner or a ticket, or with status waiting
Idle         any other root: no running child, no job on a runner and no ticket
Singletons   a root at a singleton location, whatever it waits on
Ended        a run with no parent that ended in the window, newest first
```

The window is 1h, 6h, 24h or 7d, and 6h by default. A line of Busy, Idle or Singletons reads:

```
running  Zummed in DM: Please tell zummed…  chat › … › ask › question [open, waiting]  timer unanswered due 10 Nov 20:29  20:29
```

- The line begins with the root's status and its name. A long address is cut in its middle, since keys differ at their end, and a title at its end.
- The chain runs from the root to the deepest running run, through the newest running child at each level. Each machine has its type's icon and opens its run's page.
- A chain of more than three machines shows the first, `…` and the last, and the `…` gives the whole chain in its tooltip.
- The brackets after the deepest machine hold the states or stages it is in, or what an agent waits for.
- Then come what the deepest run waits on and when the root started.

An ended line holds its status, name, machine, the number of runs under it, when it ended and how long it took.

A click on a line opens its root's page, whose Steps follow the newest row. The `▸` at the line's start unfolds it to its root's own row, where the root runs, and then the runs the root started, newest first: those running and those that ended in the window. This is how a person finds one message in a long conversation:

```
running  chat [waiting]                                   timer quiet due 20:36         3 Nov 20:34
done       message  message › … › attend               done from attend · 8 runs     3 Nov 20:36  1.7s
done       message  message › say › slack-say          done from say · 14 runs       3 Nov 20:34  4.1s
```

```
name         the run's name
chain        the runs below it as a line's chain: the running ones, or for an ended run the newest at each level
ended with   the error it failed with or the last event it took, as its record writes it; for a running run, what the deepest waits on
runs         how many runs are under it
when         when it started, and how long it took
```

A line shows at most 20 such rows, then `N more`, which opens the root's page. A run in another client's tree shows nothing it ended with.

What a run waits on is written as:

```
ticket <name>               a ticket
timer <name> due <time>     a timer; a run in another client's tree shows the number of timers
job <name>                  a job on a runner; a run in another client's tree shows the number of jobs
held <name>                 a job held for the ask
child <machine>             a running child
```

The names and times come from the latest step of a run in a tree of the UI's client, read once for each step.

### Narrowing

The page is narrowed by location prefix, `?at=/home`, and by label, as `--label` narrows `corbie run ls`. A prefix keeps the runs at it and below it: `/home` keeps `/home/rooms/thermostat` and not `/homes/x`. A root or ended run the narrowing leaves is listed with every run under it.

### One location

Narrowed to one location, the page has a head above the sections with the location, its lifetime, and its tree and client where the UI knows them. Its keyed runs are each listed by key. With a client among the UI's parties, the head offers:

```
Cancel every run here   every run of the location in the client's tree     corbie run cancel <location>
Send                    an event to a key, with data; keyed locations       corbie run send <location>#<key> <event> <data>
Start                   a run, with input; transient locations             corbie run submit <location> --input <input>
```

- Cancel is offered at a location in a tree of the UI's client. The command cancels each root running there, one after another, and a run that starts in between is not cancelled.
- Send is offered at any keyed location, and Start at a transient location in a tree of the UI's client. The receipt says whether the controller took it.

## One run

A run's page opens from its line in the list. Its title line holds the run: its machine, type, status and title, then its address, which links to the page narrowed to its location, then its ID, when it started, and when it ended and how long it took. Under it is the run's path, each run a link:

```
slack-listen ⇢ chat › message › answer › helper › approval › ask › question
```

The path goes up from the run, by parent, to its root, and down to the deepest running run, by the newest running child at each level. Where a send or a call started the root, the run that sent or called comes first, with `⇢`; its tooltip says which, where the root is in a tree of the UI's client. The run itself is in bold, and the run of the step selected is lit. Holding the pointer over a run shows its title, its address and its ID, as over a name.

On the left are four tabs: Steps, Waiting on, Output and Children. On the right are the drawing and the details of the step selected.

### Steps

Steps lists what happened in the run and in every run under it, one row each, read from the record as `corbie run show --record` reads it. A row holds its time to the second, with its date where that is not the date the run started, its run's machine in the colour of the machine's type, and what happened:

```
statechart   entered answering · left asking, entered open and waiting · timer unanswered set, due 10 Nov 20:29 · timer unanswered fired · state answering started message
flow         stage who started who · stage who done · stage refuse skipped · stage brief failed
agent        turn 2 to claude-sonnet-5-5 · turn 2 called tell(person: “zummed”, text: “The…”) · open done · held tell for approval · ask started approval
any run      started · started by /home/bridges/slack · done · failed (denied) · cancelled
```

Sends and calls between runs read:

```
⇠ set from /home/rooms/thermostat#kitchen                 an event a run sent; links to that run
⇠ set from client:panel                                   an event a client sent
⇢ sent set to /home/devices/boiler · boiler               links to the run that took it
⇢ sent message to /home/chat#slack:C1 · started chat      the send started a run at a keyed location
⇢ sent set to /home/rooms/thermostat#hall · undelivered   its details say why
⇢ called /home/services/weather · weather                 a call job; links to the run it started
```

- Holding the pointer over a row's machine shows its run's title, its address and its ID.
- A child's rows are under the row that started it, one level in, with a faint line at each level. Rows deeper than four levels are drawn at the fourth, and the path gives their depth.
- A send and an event from a run have a dashed mark.
- An undelivered send gives the reason in its details: the receiver's `receive:` does not list the sender, no run is running there, the location's limit of runs is reached, the run is a flow or an agent, which takes no events, or the run was cancelled while it kept the event under `defer:`.
- A send a layer of the sender refused is `denied`: a send job's row says so, and a statechart's send ends its run `failed (denied)`, whose details give what was refused, the layer and the fix.
- The row that started a child ends with the child's title where it has one, its status, its steps and those of the runs under it, and how long it took: `stage helper started helper · running · 22 steps`. The child's rows fold into that row.
- An ended child is folded, and a running one open. Open all and Fold all fold every group.
- On the page of a run in another client's tree, there are no Steps.

### Following a request

A send or a call to a run in a tree of the UI's client folds the rows it caused in that run, so one request can be followed from run to run on one page. Its row ends with the run's status, and its group is folded:

```
call    every row of the run the call started
send    the rows of the run that took the event, from the step that took it up to the next step that took another sent event, with the runs started in between
```

- The rows are read when the group is first opened, and read again as the page follows the run.
- The rows of a send's group can hold another send or call, which folds in the same way.
- A send's group is approximate where events from two senders arrive close together: it ends at the first other sent event, whoever sent it.
- A send or a call to a run in another client's tree is a link to its page, which shows only its status.

For Rook, one Slack message is followed from the page of the chat run at `/my-bot/.rook/blocks/chat#slack:C1`:

1. `⇠ message from /my-bot/bridges/slack`, the message as it arrived.
2. `⇢ sent message to /my-bot/.rook/blocks/conversation#slack:C1:helper · conversation`, which unfolds to the conversation's rows.
3. In them, `⇢ called /my-bot/agents/helper · helper`, which unfolds to the agent's turns and tool calls.
4. `held deploy for approval`, then `ask started approval`.
5. Inside approval, `⇢ called /my-bot/.rook/blocks/question · question` unfolds to the question run.
6. In the question run, `⇠ answer from /my-bot/bridges/slack`.
7. The tool runs, then `turn 2 replied`.

### Selecting a row

One row is selected. On a running run it is the newest row, or the first row of the folded group it is in, until the person selects another; on an ended run it is the last. Folding the group of the selected row selects the group's first row. Rows of a send's or a call's group are never the newest row.

The keys act when the list has the focus, or nothing else on the page has it:

```
↑ ↓      selects the row above or below, of those shown
←        folds the group the row starts, or the group it is in
→        opens the group the row starts
click    selects the row and shows its details
```

The drawing shows the selected row's run as it was at the row's step, from its record up to that step and the state that step left (see Drawing). The drawing is at its own size, and its panel scrolls to the first card that is going, held or failed.

The details of a row have four tabs, and Open opens the row's run's page:

```
What happened   the row's facts: the job and its machine or script, a tool's arguments, the run a tool is held for, the turn, the error, the address sent to and the run that took it
Input           what the row's job, call or event was given
Output          what it gave back
Record          the values the run's jobs emitted since the step before, and the row's step, in the words corbie run show --record prints, each named by its run's machine
```

### Waiting on, Output and Children

- What the run waits on is listed as in the list, with each running child linked and each ticket's value, and its labels below.
- Output shows the output, or the error; a denial shows what was refused, the layer that refused it, and the fix. The files of its result are listed below, each a download, as `corbie run show --save` fetches it, and never shown in a page.
- Children lists the runs it started, with each one's status, name, machine where that is not its name, and when it started or ended.

A run in another client's tree shows its status, its path, its children and its labels, and nothing from its record. Its jobs and timers are counted and its tickets named.

## Acting on a run

```
Reply    answers a ticket, by its value, with an answer   corbie run reply <id> <ticket> <answer>
Send     sends the run an event, with data                corbie run send <id> <event> <data>
Cancel   cancels the run                                  corbie run cancel <id>
```

With a client among the UI's parties, Send is on the page of every running statechart. The run's `receive:` decides whether a run in another client's tree takes it, and the receipt says.

On a run in a tree of the UI's client, each ticket's value is filled in. On any other running run, Reply takes a ticket's value typed in, as a client that was handed one answers it. An answer and an event's data are typed as JSON.

Cancel is on every running run in a tree of the UI's client. With the controller among the UI's parties, it is on every running run's page, and on a run in another client's tree it is `corbie controller cancel <id>`.

The page shows the answer the command prints. For Reply, Send and Cancel that is the receipt: taken, or refused with the reason, for an event whether the machine acted on it, and for a send the run that took it. `corbie controller cancel` does not wait for a receipt, and the page says the cancel is in the controller's inbox.
