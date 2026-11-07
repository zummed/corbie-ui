# Machines

The Machines page shows the machines in one directory as the tree `corbie run submit` records: a list of its machines and its pieces, the problems Corbie finds in them, and for the one chosen its location, the secrets and values it uses, its drawing, its text and its definition written out. The Toolkits page shows the same directory from the tools up: each toolkit, what offers it and what starts that. Every UI but `none` offers both, and both only read: the files are what `corbie run submit` reads and a model edits.

## The directory

The directory is the one `--dir` names, by default the one the UI was started in. It is read with Corbie's `client.ReadTree`, as `corbie run submit` reads it, at a prefix:

```
acting as the controller, the directory last submitted at a tree   that tree's prefix, such as /home
any other                                                         the prefix a submit gives by default: / and the directory's name
```

Its locations, and where its secrets and values are set, are then those of the runs the tree starts.

```
house/devices/boiler.yaml       /house/devices/boiler       a machine: its first key gives a type
house/devices/boiler-job.yaml   no location                 a piece
house/.rook/review.yaml         /house/.rook/review         a machine in a directory whose name begins with a dot
```

- A machine is a `.yaml` file whose first key gives a type, and its location is its path under the prefix, without `.yaml`.
- A piece is a `.yaml` file with no type, or any other file a machine takes a value from, such as `style.md`.
- A path in a machine may name any file inside the directory. Every path a request names is inside it, and a path that leaves it is refused.
- The UI reads the directory again only when a file in it changes, by name, size or time of change, or its prefix does, and the Machines and Toolkits pages share what it read.

The page gives the prefix above the list, `at /house`.

## The list

The list holds the machines by directory, then the pieces:

```
house                                    at /house
answer.yaml        flow: answer · transient           2
devices/
boiler.yaml        statechart: boiler · singleton
rooms/
thermostat.yaml    statechart: thermostat · keyed
PIECES
devices/boiler-job.yaml   included by 1 machine
people.yaml               included by none
```

- A machine has its type's icon, its file's name, its first line, its lifetime, and the count of the problems shown at its text. Its name's tooltip gives its location.
- A piece has its path and how many machines include it: those whose definition written out takes a value from it.
- A file beside a directory of the same name, and the files below it, have no location. Each is listed with Corbie's problem: `it is beside a directory of the same name, which holds the machine at /house/boiler/x: a location has nothing below it`.
- When a directory cannot be read, Corbie gives no tree. The page then lists no machine, piece or toolkit, only Corbie's error.

## Problems

Above the list of Machines and of Toolkits, each problem of the directory's machines is listed once, at the file Corbie names, with how many machines it breaks:

```
2 PROBLEMS
toolkits/messages.yaml:5
tools.tell.description  a tool says what it is for, for the model to choose by
breaks 3 machines
toolkits/messages.yaml:6
tools.tell.does  "does" is not a field that belongs here
breaks 3 machines
```

- The place is given as the file writes it, with the line where the text writes it. A place in a file that a machine includes is found in that file.
- Paths in a problem's text are written from the directory.
- A problem is drawn the same way above a list, above a file's text and above a toolkit: a box with a rowan border, holding each problem's place, its text and the machines it breaks.
- A file an `include:` names that cannot be read is one problem at that file, breaking each machine that includes it, however each writes its path. The other problems of those machines are not listed.
- A machine with problems that reads without any from a directory below this one is listed under one problem for that directory: `slackbot/ is the top of a tree, and its machines read their paths from there: start the UI in slackbot/`. The machine's own problems are not listed, here or at its text.

## A machine

Choosing a machine shows, above its view, where it lives and how its runs start, as its file says:

```
location   /house/rooms/thermostat
lifetime   keyed: a send to a key starts the run for that key, at most 3 running at once
update     restart: a changed file cancels its runs
receives   set from schedules/*, client:panel
           and anything from its tree's client
sends to   devices/boiler, /work/log
```

| Row | What it shows |
|---|---|
| location | the prefix and the file's path, without `.yaml`; where the UI offers Runs, a link to the runs there |
| lifetime | `singleton`, `keyed` or `transient`, what starts its runs, and `limits: { runs: }` |
| update | `restart` or `drain`, and what a changed file does to its runs |
| receives | who `receive:` lets send to it and call it, by event; with no `receive:`, its own tree and its tree's client |
| sends to | the `send:` entries of its `allow:`, with `**` read as every location in its tree, `/**` and `all` as every location; `none` where there are none |

A machine with problems shows only its text, with no location.

The tabs Drawing, Text and Written out choose the view, Drawing by default:

```
Drawing       the machine drawn, as Drawing says; a card that names a machine unfolds it
Text          the file as it is written
Written out   the definition Corbie stores: what it includes in place, each value with its file and line
```

The address of a machine is `/machines?file=<file>`, with `open=<card path>` for each card unfolded, a card's path being the names of the cards down to it joined by `/`: `/machines?file=schedules/away.yaml&open=pause`. A run's page links to its machine's address where the run's tree was submitted from this directory at the prefix the page reads it at (see Runs).

Written out is what `corbie test <file> --definition` prints. Each value's file and line links to that line of that file's text:

```
model:
  name: claude-sonnet-5-5                    # base.yaml:1
  effort: high                               # family.yaml:3
```

A file that changes on disk is shown again.

## A piece

Choosing a piece shows its text, and above it the machines that include it, each a link. A piece that no machine takes a value from says so.

Any other file a link opens, such as a tool's script, is headed `a file` and shows its text alone.

## The text

A file is shown as text, read-only, with its lines numbered, and its keys, expressions and comments set apart. Above the text are the problems found in it, and the lines they are found at are marked.

```
a machine        the problems Corbie gives for it, wherever each is
any other file   the problems found in it by the machines that read it
```

A file with no problems shows no problems section.

## Secrets and values

Above the view of a machine, the page lists the secrets and the values it uses, read from the machine written out, its child machines included:

```
secret   each NAME of a secret:NAME@… entry in an allow: list; an entry whose name holds * names none
value    each NAME an expression reads as values.NAME
```

Acting as the controller, the UI reads where each is set, as `corbie controller secret list` and `corbie controller value list` read it, and gives the location nearest the machine's, at it or above:

```
ANTHROPIC_KEY    secret · set at /
BOILER_PASSWORD  secret · not set at /house/devices/boiler or above
HUB              value · set at /house
```

Acting as a client alone, it lists the names with no state, and one line gives the commands that set them at the machine's location. A machine that uses none shows no section.

## Toolkits

A toolkit is a group of tools under an agent's `toolkits:`. The Toolkits page lists every toolkit in the directory, with its `description:` and its count of tools, and shows the one chosen:

```
messages                                toolkits/messages.yaml
Drawing   Text

Messages to the people this bot knows.

Offered by                              Started by
helper  agents/helper.yaml              answer  transient: a call, or corbie run submit /bot/agents/helper, starts each run

tell        needs approval
Say a text to a person, by their name in people.yaml.
machine tell.yaml
```

| Field | What it shows |
|---|---|
| Offered by | each agent whose `toolkits:` holds the toolkit, in its own file or written in place in another machine |
| Started by | for an agent in its own file, each machine that runs it with `machine:` and how its location's runs start; for one written in place, the machine it is written in |
| Tools | each tool's `description:`, its job, and `needs approval` where the job has `approve: true` |

- A toolkit's file is the file its tools are written in, as the agent written out gives it. Two agents that take a toolkit's tools from the same file offer one toolkit, whichever name each gives it.
- Opening a toolkit with `approve: true` is shown as `opening it needs approval`.
- A piece with `description:` and `tools:` at its top level that no agent offers is listed under the name of its directory, with a grey `not offered`, and its page says `not offered`. A new toolkit therefore shows before anything uses it.
- A machine with problems offers no toolkit, since Corbie does not write it out. A toolkit whose file has problems is marked `has problems`, and the problems above it name the machines it breaks.
- A toolkit's file with problems shows them above the toolkit, as the text does.
- The tabs Drawing and Text choose the view, as on the Machines page, and Text shows the toolkit's file as the text view does. A toolkit written in place in a machine has no tabs.
