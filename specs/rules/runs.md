# Runs

The rules behind [the Runs overview](../runs.md). Area code: `runs`.

## Names

**R-runs-09** A run the UI's client started is named by its title where it has one, read from its record once while the list holds it; any other run, and a run with no title, by its key, or by its machine where it has no key. The name is what its line, its row and Children show, the row that started it and its page's title line show its title, the title line beside the key; where a title names a run, the tooltip of its name gives its key. *Test:* `runs_a_run_is_named_by_its_title`

## The list

**R-runs-01** Each root, a running run whose parent is not running, is one line in Busy, Idle or Services by its key, its status, its jobs, its tickets and its running children, with the chain of running runs from it to the deepest through the newest running child at each level, and what the deepest waits on, by name from its latest step where the UI's client started it; below them, each run with no parent that ended in the window. *Test:* `runs_list_roots_by_section`, `runs_own_runs_wait_by_name`

**R-runs-07** A line unfolds to the runs its root started that run or ended in the window, newest first, at most 20 and the number of the rest; each with the chain below it, what it ended with from its last step or what its deepest waits on, where the UI's client started it, and how many runs are under it. *Test:* `runs_line_unfolds_to_the_runs_its_root_started`

**R-runs-02** The page is narrowed by label as `corbie run ls --label` reads it, and by client; a root or ended run the narrowing leaves is listed with the runs under it. *Test:* `runs_narrowing_holds_for_the_whole_page`

**R-runs-06** A root whose `hash=` label differs from that of the newest run with no parent that its client keyed `file:<path>`, where `<path>` is its `machine=` label, is marked as on an old version; Cancel on Idle runs `corbie run cancel` for each idle root on an old version the UI's client started, and for no other run. *Test:* `runs_cancel_idle_runs_on_old_versions`

## One run

**R-runs-04** A run the UI's client started is shown in full: what it waits on with each ticket's value and each timer's due time from its latest step, leaving out events at its own key, and Steps, one row for each thing that happened in it and in the runs under it, read from its record, each child's rows under the row that started it, with an ended child folded and a running one open. *Test:* `runs_own_run_shown_in_full`

**R-runs-08** The drawing beside Steps shows the selected row's run as of the row's step, from its record up to that step and the state that step left, and the row's details give the entries of that step, with the values emitted since the step before, in the words `corbie run show --record` prints. A send to a key is linked to the run it went to, and the path begins with the run whose send started the root. *Test:* `runs_a_step_shows_its_run_as_it_was`

**C-runs-01** A file of a run's result is sent as a download, with `Content-Disposition: attachment` and `X-Content-Type-Options: nosniff`. A result can hold HTML or SVG a script fetched from anywhere, and shown in a page it would run with the UI's cookie. *Test:* `runs_result_file_is_a_download`

**C-runs-02** A run another client started is read only through the list and Corbie's client package, which gives its status and nothing from its record. The UI holds the controller's keys too, and must not read a run another way. *Test:* `runs_other_clients_run_shows_no_record`

## Acting on a run

**R-runs-05** Reply, Send and Cancel run `corbie run reply`, `corbie run send` and `corbie run cancel`, an answer and an event's data written out again from the JSON typed; Reply runs on any run with a ticket's value, Send and Cancel only on the client's own runs, with Send offered on its statecharts alone, and with the controller Cancel on another client's run runs `corbie controller cancel`. *Test:* `runs_actions_run_their_commands`

## Interfaces

Runs uses Pages for the shell, the stream, its errors, the commands its buttons run and how a time is shown (`page.Shell`, `page.Write`, `page.Stream`, `page.Part`, `page.Error`, `page.Call`, `page.Command`, `page.Answer`, `page.When`), and Drawing to draw a run's machine (`drawing.Draw`, `drawing.Run`, `drawing.Step`, `drawing.Child`, `drawing.Machine`).
