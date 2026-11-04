# Runs

The rules behind [the Runs overview](../runs.md). Area code: `runs`.

## The list

**R-runs-01** Each root, a running run whose parent is not running, is one line in Services, Busy or Idle by its key and its head, with the chain of running runs from it to the deepest through the newest running child at each level, and what the deepest waits on, by name from its latest step where the UI's client started it; below them, each run with no parent that ended in the window. *Test:* `runs_list_roots_by_section`, `runs_own_runs_wait_by_name`

**R-runs-02** The page is narrowed by label as `corbie run ls --label` reads it, and by client; a root or ended run the narrowing leaves is listed with the runs under it. *Test:* `runs_narrowing_holds_for_the_whole_page`

**R-runs-06** A root whose `hash=` label differs from that of the newest run with no parent that its client keyed `file:<path>`, where `<path>` is its `machine=` label, is marked as on an old version; Cancel on Idle runs `corbie run cancel` for each idle root on an old version the UI's client started, and for no other run. *Test:* `runs_cancel_idle_runs_on_old_versions`

## One run

**R-runs-04** A run the UI's client started is shown in full: its drawing with its state marked, what it waits on with each ticket's value and each timer's due time from its latest step, and its record in the words `corbie run show --record` prints, each entry named by its run's machine. *Test:* `runs_own_run_shown_in_full`

**C-runs-01** A file of a run's result is sent as a download, with `Content-Disposition: attachment` and `X-Content-Type-Options: nosniff`. A result can hold HTML or SVG a script fetched from anywhere, and shown in a page it would run with the UI's cookie. *Test:* `runs_result_file_is_a_download`

**C-runs-02** A run another client started is read only through the list and Corbie's client package, which gives its status and nothing from its record. The UI holds the controller's keys too, and must not read a run another way. *Test:* `runs_other_clients_run_shows_no_record`

## Acting on a run

**R-runs-05** Reply, Send and Cancel run `corbie run reply`, `corbie run send` and `corbie run cancel`, an answer and an event's data written out again from the JSON typed; Reply runs on any run with a ticket's value, Send and Cancel only on the client's own runs, with Send offered on its statecharts alone, and with the controller Cancel on another client's run runs `corbie controller cancel`. *Test:* `runs_actions_run_their_commands`

## Interfaces

Runs uses Pages for the shell, the stream and the commands its buttons run (`page.Shell`, `page.Write`, `page.Stream`, `page.Part`, `page.Call`, `page.Command`), and Drawing to draw a run's machine (`drawing.Draw`, `drawing.Run`, `drawing.Step`, `drawing.Child`).
