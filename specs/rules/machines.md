# Machines

The rules behind [the Machines overview](../machines.md). Area code: `machines`.

## The directory

**R-machines-01** The list starts at each machine no other machine names, outside directories whose names begin with a dot. Each machine has its type's icon, its first line, how many stages, states or tools it has, and the count of its problems; under the machine chosen and those above it are the machines it names, each followed once, then each other file it names with the field where the definition writes its path. *Test:* `machines_list_shows_what_each_item_is`

**R-machines-05** Choosing a machine shows its drawing, unfolded as the address says, and its problems. Text shows the file, read-only unless the UI acts as a client. *Test:* `machines_drawing_unfolds_as_the_address_says`

**C-machines-01** Every path a request names is inside the directory once links are followed, and only `.yaml` files are read as machines. A request could otherwise read or write any file the UI's user can. *Test:* `machines_path_outside_the_directory_is_refused`

**C-machines-05** A UI that does not act as a client has no address that saves, writes a new file, tests or starts, and shows no control for them. *Test:* `machines_read_only_writes_nothing`

## Parts

**R-machines-06** The Parts page lists each part an agent in the directory opens, in its own file or written in place, and each file with `does:` and `tools:` at its top level that no agent opens, outside directories whose names begin with a dot. A part shows each tool's `does:`, its job, whether the job has `approve: true`, and the `allow:` entries of the job and of the machines it runs. It shows each agent that opens it, and each machine above that agent that no other machine names, leaving out those in directories whose names begin with a dot. A machine with problems is listed apart, and stays above the machines its text names. *Test:* `machines_parts_show_what_opens_each`

**R-machines-07** A directory with `.rook/` also shows, from its `rules.yaml`, the `for:` of each entry under `agents:` that names an agent or a machine above it, and the names under `approve:` for each part an agent opens, under the `part:` of its `ask:` input and by default its name, or that none is named where it needs approval. A `rules.yaml` that cannot be read is said on the page. A directory without `.rook/` shows neither, and its `rules.yaml` is not read. *Test:* `machines_parts_show_rules_only_in_a_rook_project`

## Editing

**C-machines-02** Save writes only if the file is as it was read. A model or another editor may have changed it since, and its change would be lost. *Test:* `machines_save_refuses_a_file_changed_on_disk`

**C-machines-03** New refuses a name that a file already has. *Test:* `machines_new_refuses_a_name_in_use`

## Checking

**R-machines-02** A definition's text is checked with Corbie's own check as it is written: each problem is given at its place, with the line where the place is found; it is drawn when it has no problems, and not while it has any; and the fields that can be written at the cursor are given from its shapes, those written there marked. *Test:* `machines_check_gives_problems_drawing_and_fields`

## Secrets

**R-machines-04** With each check, the page lists each secret that a `secret:NAME@…` entry in any `allow:` list of the text names, once, in the order written, an entry whose name holds `*` naming none. Acting as the controller, it reads which are set as the Controller page does, shows each as `set` or `not set`, and offers Set beside one that is not set, which is the Controller page's Set: `corbie controller secret set NAME -`, with the value on standard input. Acting as a client alone, it lists the names with no state, and offers no Set. *Test:* `machines_secrets_named_are_listed_and_set`

## Testing and starting

**R-machines-03** Test runs `corbie test <file> --watch` and sends the page each line it prints, and Start runs `corbie run submit <file>` and answers with the run's page; both take the input as JSON and run secrets by name as `--secret-env`, and Start labels, permissions and a timeout. *Test:* `machines_test_and_start_run_their_commands`

**C-machines-04** The input of Test and Start is JSON the UI has read, and goes to the command as `--input=` followed by it. Written as `@` and a path, the input would be read from a file on the disk. *Test:* `machines_input_is_json_and_never_a_file`

## Interfaces

Machines uses Pages for the shell, the stream and the commands it runs (`page.Shell`, `page.Write`, `page.Stream`, `page.Part`, `page.Call`, `page.Command`), and Drawing to draw a definition (`drawing.Draw`).
