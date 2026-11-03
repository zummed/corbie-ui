# Machines

The Machines page shows the machines in one directory: a list of them, the problems Corbie finds in them, the drawing of the one chosen, and its text. The Parts page shows the same directory from the tools up: each part, what opens it and what starts that. Every UI but `none` offers both, and both only read: the files are what `corbie run submit` reads and a model edits.

## The directory

The directory is the one `--dir` names, by default the one the UI was started in. A machine is a `.yaml` file there or below it whose first key gives a type, read with Corbie's `client.ReadMachines`, as `corbie run submit` reads it. A path in a machine may name any file inside the directory. Every path a request names is inside it, and a path that leaves it is refused.

The UI reads the directory again only when a file in it changes, by name, size or time of change, and the Machines and Parts pages share what it read.

A file in a directory whose name begins with a dot is reached only through a machine that names it. It is never listed on its own, its problems count only through the machines that read it, and it never starts an agent on the Parts page.

## The list

The list holds the project's own machines, those outside directories whose names begin with a dot, by directory:

```
answer.yaml        flow: answer          2
attend.yaml        flow: attend
AGENTS/
helper.yaml        agent: helper         2
team.yaml          agent: team
BRIDGES/
slack.yaml         flow: slack-bridge    2
```

Each machine has its type's icon, its file's name, its first line, and the count of its problems. The drawing of a machine shows what it names, and unfolds it. A directory that cannot be read ends the list there, and the list says so.

## Problems

Above the list, each problem of the project's machines is listed once, at the file Corbie names, with how many machines it breaks:

```
2 PROBLEMS
parts/messages/part.yaml:5
tools.tell.does  a tool says what it does, for the model to choose by
breaks 3 machines
parts/messages/part.yaml:6
tools.tell.description  "description" is not a field that belongs here
breaks 3 machines
```

- The place is given as the file writes it, with the line where the text writes it. A place in a file that a machine includes is found in that file.
- Paths in a problem's text are written from the directory.
- A machine with problems that reads without any from a directory below this one is listed under one problem for that directory: `slackbot/ is the top of a project, and its machines read their paths from there: start the UI in slackbot/`. The machine's own problems are not listed, here or at its text.

## The drawing

Choosing a machine shows its drawing, as Drawing says, with the secrets it names above it. A machine with problems has no drawing, and shows its text. A file that changes on disk is shown again. Text shows the file, and Drawing goes back.

## The text

A file is shown as text, read-only, with its lines numbered, and its keys, values and expressions set apart. Above the text are the problems found in it, and the lines they are found at are marked.

```
a machine        the problems Corbie gives for it, wherever each is
any other file   the problems found in it by the machines that read it
```

A file with no problems shows no problems section.

## Secrets

Above the drawing or the text of a machine, the page lists the secrets it names: each `NAME` of a `secret:NAME@…` entry in its `allow:` list and in each job's, once, in the order written. An entry whose name holds `*` names none. A machine that names none shows no Secrets section.

```
GRAPH_REFRESH     set
CALENDLY_TOKEN    not set
```

Acting as the controller, the UI reads which are set as the Controller page reads them. Acting as a client alone, it lists the names with no state, and one line says a secret is set on the controller's computer with `corbie controller secret set NAME`.

## Parts

A part is a group of tools under an agent's `open:`. The Parts page lists every part in the directory, with its `does:` and its count of tools, and shows the one chosen:

```
messages                                parts/messages/part.yaml       Text

Messages to the people this bot knows.

Opened by                               Started by
helper  agents/helper.yaml              answer

tell        needs approval
Say a text to a person, by their name in people.yaml.
machine tell.yaml
```

| Field | What it shows |
|---|---|
| Opened by | each agent whose `open:` holds the part, in its own file or written in place in another machine |
| Started by | the machine the agent is written in, or the nearest machine above it on each path outside directories whose names begin with a dot, or `by hand` where there is none |
| Tools | each tool's `does:`, its job, and `needs approval` where the job has `approve: true` |

- Two agents that open the same file open one part, whichever path and name each gives it.
- Opening a part with `approve: true` is shown as `opening it needs approval`.
- A file with `does:` and `tools:` at its top level that no agent opens, outside directories whose names begin with a dot, is listed under the name of its directory, with a grey `not opened`. A new part therefore shows before anything uses it.
- A part's file with problems shows them above the part, as the text does.
- What a machine with problems opens cannot be read. The list says how many of the project's machines have problems, with a link to Machines.
