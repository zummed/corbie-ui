# Machines

The rules behind [the Machines overview](../machines.md). Area code: `machines`.

## The directory

**R-machines-10** The directory is read with Corbie's `client.ReadMachines` again only when a file in it changes, and the Machines and Parts pages share what was read. *Test:* `machines_directory_is_read_once_per_change`

**R-machines-11** A file in a directory whose name begins with a dot is reached only through a machine that names it: it is never listed on its own, its problems count only through the machines that read it, and it never starts an agent. *Test:* `machines_dotted_files_are_reached_through_what_names_them`

**C-machines-01** Every path a request names is inside the directory once links are followed, and only `.yaml` files are read as machines. A request could otherwise read any file the UI's user can. *Test:* `machines_path_outside_the_directory_is_refused`

**C-machines-06** The Machines and Parts pages have no address that writes a file or runs a command, and show no form. A model edits the files, and a second way to edit them would be a second path. *Test:* `machines_pages_only_read`

## The list

**R-machines-01** The list holds each machine outside directories whose names begin with a dot, by its directory, each with its type's icon, its file's name, its first line and the count of its problems. *Test:* `machines_list_shows_what_each_item_is`

## Problems

**R-machines-08** Above the list, each problem of the listed machines is given once, at the file Corbie names, with its place as that file writes it, its line, and the machines it breaks. *Test:* `machines_problems_are_listed_once_at_their_file`

**R-machines-09** A machine with problems that reads without any from a directory below this one is listed under one problem for that directory, which is the top of its project, and its own problems are not listed. *Test:* `machines_directory_below_the_top_is_one_problem`

## The drawing and the text

**R-machines-05** Choosing a machine shows its drawing, unfolded as the address says; a machine with problems shows its text. Text shows the file. *Test:* `machines_drawing_unfolds_as_the_address_says`

**R-machines-02** A file's text is read-only, with its lines numbered, under the problems found in it, and the lines they are found at are marked: for a machine, those Corbie gives for it; for any other file, those found in it by the machines that read it. A file with none shows no problems. *Test:* `machines_text_shows_the_problems_found_in_it`

## Secrets

**R-machines-04** The page lists each secret that a `secret:NAME@…` entry in any `allow:` list of a machine's text names, once, in the order written, an entry whose name holds `*` naming none. Acting as the controller, it shows each as `set` or `not set`, as the Controller page reads them; acting as a client alone, by name only. *Test:* `machines_secrets_named_are_listed`

## Parts

**R-machines-06** The Parts page lists each part an agent opens, in its own file or written in place, and each file with `does:` and `tools:` at its top level that no agent opens, outside directories whose names begin with a dot, marked `not opened`. A part shows each tool's `does:`, its job, and whether the job has `approve: true`; each agent that opens it; and for each agent the machine it is written in, or the nearest machine above it on each path outside directories whose names begin with a dot, or `by hand`. *Test:* `machines_parts_show_what_opens_each`

## Interfaces

Machines uses Pages for the shell and the stream (`page.Shell`, `page.Write`, `page.Stream`, `page.Part`), and Drawing to draw a definition (`drawing.Unfolded`).
