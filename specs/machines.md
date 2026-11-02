# Machines

The Machines page shows the machines in one directory: a list of what names what, the drawing of the one chosen, and its text. The Parts page shows the same directory from the tools up: each part, what opens it and what starts that. Every UI but `none` offers both, to read. A UI that acts as a client edits, tests and starts as well.

## The directory

The directory is the one `--dir` names, by default the one the UI was started in. A machine is a `.yaml` file there or below it whose first key gives a type, read with Corbie's `client.ReadMachines`, as `corbie run submit` reads it. A path in a machine may name any file inside the directory. Every path a request names is inside it, and a path that leaves it is refused.

## The list

The list starts at each machine no other machine names, and leaves out those in directories whose names begin with a dot, which are reached by following what names them. Under a machine are the machines it names, by `machine:` or a `send:`, then the other files it names, each with the field that names it. Only the machine chosen and those above it show what is under them:

```
bridges/telegram.yaml       flow: telegram-bridge · 1 stage
  listen.yaml               statechart: telegram · 2 states
    chat.yaml               statechart: chat · 3 states
      who.yaml              flow: who · 2 stages
        lookup.yaml         flow: lookup · 1 stage
        who.js              script:
        rules.yaml          files:
      answer.yaml           flow: answer · 2 stages
        helper.yaml         agent: helper · 2 tools
          base.yaml         include:
          part.yaml         include:
```

Each file is shown by its name, with its path as a tooltip. Each machine has its type's icon, its first line and how many stages, states or tools it has, and the count of its problems and a dot while it has unsaved edits are on the same line. A machine is followed once: where it is named again, it is listed with nothing under it. A machine with problems is followed by the paths its text writes, and machines that name each other are listed last. A directory that cannot be read ends the list there, and the list says so.

## The drawing

Choosing a machine shows its drawing, as Drawing says, with its problems above it. A machine with problems has no drawing. A file that changes on disk is drawn again. Text shows the file, and Drawing goes back.

## The text

A file is shown as text, with its lines numbered, and its keys, values and expressions set apart. The text is read-only unless the UI acts as a client, and a file that is not a machine has only its text.

These are the files `corbie run submit` reads and a model edits. The UI writes to the directory only when Save or New is pressed.

## Parts

A part is a group of tools under an agent's `open:`. The Parts page lists every part in the directory, with its `does:` and its count of tools, and shows the one chosen:

```
net                                              parts/net/part.yaml     Text

The network: the firewall and switch, looked at and changed.

Opened by             Started by                                For
helper                telegram-bridge                           alice
firewall-load         watch-firewall-load · sun 19:00 Europe/London

Approved by alice

show_status           script show-status.js                     net:192.0.2.1:22 …
change_config         needs approval · machine change-config.yaml
```

| Field | What it shows |
|---|---|
| Opened by | each agent whose `open:` holds the part, in its own file or written in place in another machine |
| Started by | each machine above that agent that no other machine names, with its timers, or `only by hand` where there is none, or `a machine with problems` where none is found and one of those is above it |
| Tools | each tool's `does:`, its job, `needs approval` where the job has `approve: true`, and the `allow:` entries of the job and of the machines it runs |

- Two agents that open the same file open one part, whichever path and name each gives it.
- A machine in a directory whose name begins with a dot starts nothing here: a bridge is listed only while its file is in the project.
- A file with `does:` and `tools:` at its top level that no agent opens, outside directories whose names begin with a dot, is listed as opened by nothing, under the name of its directory. A new part therefore shows before anything uses it.
- A machine with problems is listed under the parts, since what it opens cannot be read. The machines it names keep it as a machine above them, by the paths its text writes.

The page reads the directory again at each interval, and writes nothing.

A directory with `.rook/` is a Rook project, and the page reads its `rules.yaml` as well.

| Field | What it shows |
|---|---|
| For | the `for:` of each entry under `agents:` that names the agent or a machine above it |
| Approved by | the names under `approve:` for the `part:` of each `ask:` input that opens it, by default its name, or that none is named where it needs approval |

For shows names as `rules.yaml` writes them, and a group is not listed out: who is in one comes from the project's `lookup.yaml`, which a project can replace. A part no agent opens has no name under `approve:` yet, and shows no Approved by. A directory without `.rook/` shows neither field, and its `rules.yaml` is not read.

## Editing

Acting as a client, the page edits the text.

- Save writes the file only if it has not changed on disk since it was read. If it has, the page shows the file as it is now beside the edits.
- A file that changes on disk while it has no unsaved edits is read again, so what a model writes appears as it is written.
- New writes `<name>.yaml` in the directory, holding a definition's first line for the type and name given, and refuses a name that a file already has.

## Checking

The definition is checked a moment after each change, by Corbie's own check, which `corbie run submit` uses, and the files it names are read as `corbie run submit` reads them.

- Each problem is listed at its place in the definition, as Corbie gives it: `states.gave_up.colour  "colour" is not a field that belongs here`. Where the place is found in the text, its line is marked.
- The drawing beside the text is drawn again each time the definition checks without problems. While it has problems, the last drawing stays.
- The fields that can be written where the cursor is are listed from the definition's shapes, each with an example of how it is written, and those already written are marked.

## Secrets

Beside the text, under the problems, the page lists the secrets the definition names: each `NAME` of a `secret:NAME@…` entry in its `allow:` list and in each job's, once, in the order written. An entry whose name holds `*` names none. A definition that names none shows no Secrets section.

```
GRAPH_REFRESH     set
CALENDLY_TOKEN    not set   Set
```

Acting as the controller, the UI reads which are set as the Controller page reads them. Set beside one that is not set is the Controller page's Set, which runs `corbie controller secret set NAME -` with the value on its standard input, and after it the row reads `set`. Acting as a client alone, the UI lists the names with no state, and one line says the secret is set on the controller's computer with `corbie controller secret set NAME`.

The list is read from the text at each check, so an edit, a save or a change read from disk changes it.

## Testing and starting

```
Test    corbie test <file> --watch    runs it in the UI's process, and shows its record as it is written
Start   corbie run submit <file>      starts a run on the store, and opens the run's page
```

Each saves the file first, as Save does, and goes no further when the save is refused. Neither is offered while the definition has problems, or by a UI that does not act as a client.

Both take input as JSON, and run secrets by name, each read from the UI's environment as `--secret-env` reads it. Start also takes labels, the permissions that narrow the run, and a timeout, as `corbie run submit` does. Files as input are left to the command line.

Test runs as `corbie test` does: with the store in memory and ceilings that allow everything, so a script may do on this computer whatever its definition's allow lists let it. The record of a test run is on the page that ran it and nowhere else. A test stops when its page closes, and one that waits on a ticket or an event waits until then.
