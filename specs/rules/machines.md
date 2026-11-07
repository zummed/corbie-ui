# Machines

The rules behind [the Machines overview](../machines.md). Area code: `machines`.

## The directory

**R-machines-10** The directory is read with Corbie's `client.ReadTree` again only when a file in it changes, and the Machines and Toolkits pages share what was read. *Test:* `machines_directory_is_read_once_per_change`

**R-machines-11** A file in a directory whose name begins with a dot is read as any other: a machine there is listed at its location, its problems are listed, and an agent in it offers its toolkits. *Test:* `machines_dotted_files_are_read_as_any_other`

**C-machines-01** Every path a request names is inside the directory once links are followed, and only `.yaml` files are read as machines. A request could otherwise read any file the UI's user can. *Test:* `machines_path_outside_the_directory_is_refused`

**C-machines-06** The Machines and Toolkits pages have no address that writes a file or runs a command, and show no form. A model edits the files, and a second way to edit them would be a second path. *Test:* `machines_pages_only_read`

## The list

**R-machines-01** The list holds each machine by its directory, each with its type's icon, its file's name, its first line, its lifetime, its location in its name's tooltip and the count of the problems shown at its text; then each piece, with how many machines take a value from it. A location is read at the prefix the page gives: acting as the controller, that of the tree the directory was last submitted at, where there is one; otherwise the prefix a submit gives the directory by default. *Test:* `machines_list_shows_what_each_item_is`, `machines_directory_is_read_at_its_recorded_prefix`

## Problems

**R-machines-08** Above the lists of Machines and of Toolkits, each problem of the listed machines is given once, at the file Corbie names, with its place as that file writes it, its line, and the machines it breaks; a file an `include:` names that cannot be read is one problem at that file, and not a machine. *Test:* `machines_problems_are_listed_once_at_their_file`

**R-machines-09** A machine with problems that reads without any from a directory below this one is listed under one problem for that directory, which is the top of its tree, and its own problems are not listed. *Test:* `machines_directory_below_the_top_is_one_problem`

## A machine

**R-machines-13** A machine without problems shows its location, a link to the Runs page narrowed to it where the UI offers Runs, its lifetime and what starts its runs, `limits: { runs: }`, its `update:` and what a changed file does to its runs, who its `receive:` lets send to it and call it, and the `send:` entries of its `allow:`, or `all`. *Test:* `machines_a_machine_shows_its_location`

**R-machines-05** Choosing a machine shows its drawing, unfolded as the address says; a machine with problems shows its text. Text shows the file. *Test:* `machines_drawing_unfolds_as_the_address_says`

**R-machines-14** Machines gives Runs the address of the machine at a location, `/machines?file=<file>` with `open=<card path>` for each card unfolded down to a child, where the location's tree was submitted from the directory the page reads, at the prefix it reads it at; and none for any other. *Test:* `machines_a_runs_machine_is_found_by_its_location`

**R-machines-12** Written out shows a machine without problems as Corbie writes it out, each value with its file and line, which links to that line of that file's text. *Test:* `machines_written_out_shows_where_each_value_is_from`

**R-machines-02** A file's text is read-only, with its lines numbered, under the problems found in it, and the lines they are found at are marked: for a machine, those Corbie gives for it; for any other file, those found in it by the machines that read it. A file with none shows no problems. *Test:* `machines_text_shows_the_problems_found_in_it`

## Secrets and values

**R-machines-04** The page lists each secret and each value a machine uses, read from the machine written out, its child machines included. Acting as the controller, it shows where each is set: the location nearest the machine's, at it or above, or `not set` at its location or above; acting as a client alone, by name only. *Test:* `machines_secrets_and_values_used_are_listed`

## Toolkits

**R-machines-06** The Toolkits page lists each toolkit an agent in a machine without problems offers, in its own file or written in place, and each `.yaml` file `.corbieignore` lists with `description:` and `tools:` at its top level and no type that no agent offers, under its file's name, marked `not offered`, or `has problems` where its file has problems. A toolkit's file is the one its tools are written in, as the agent written out gives it, where that is not the agent's own file. A toolkit shows the first line of its `instructions:` or the file they are included from; each tool's `description:`, its job, for a call, written `call:` or as a job's `call:`, the location with a link to the file of the machine there and the input it fixes, for a job's `send:` the event and the address with a link to the file of the machine at its location, its `when:`, and whether the job has `approve: true`; each agent that offers it, with its entry's `when:`; and for an agent in its own file each machine that runs it with `machine:` or calls its location as written out, and how its location's runs start, or for one written in place the machine it is written in. *Test:* `machines_toolkits_show_what_offers_each`, `machines_toolkits_call_machines_by_their_location`, `machines_toolkits_in_one_directory_are_named_by_their_files`, `machines_toolkits_written_in_a_placed_agent_are_in_its_file`

## Interfaces

Machines uses Pages for the shell, the stream and its errors (`page.Shell`, `page.Write`, `page.Stream`, `page.Part`, `page.Error`), Controller to read the trees the controller recorded (`controller.Trees`), and Drawing to draw a definition and to find a toolkit's file (`drawing.Unfolded`, `drawing.Unfold`, `drawing.ToolkitFile`), and to write an address as a drawing does (`drawing.Address`).
