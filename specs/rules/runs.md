# Runs

The rules behind [the Runs overview](../runs.md). Area code: `runs`.

## The list

**R-runs-01** The list holds the running runs first, each running child under its running parent, then the runs that ended in the timeline's window, each with when it ended and how long it took. *Test:* `runs_list_running_then_ended_in_the_window`

**R-runs-02** The page is narrowed by status, by label as `corbie run ls --label` reads it, and by client; the counts, the timeline and the list show only the runs the narrowing leaves, and each status is offered with its count. *Test:* `runs_narrowing_holds_for_the_whole_page`

## The timeline

**R-runs-03** The timeline has a lane for each machine, client or label value with a run in the window, in the order of their names, with the runs that lack the label last; each run is a bar from its start to its end or to now, and runs that overlap in a lane are drawn one under another. *Test:* `runs_timeline_lanes_and_bars`

## One run

**R-runs-04** A run the UI's client started is shown in full: its drawing with its state marked, each ticket with its value, each timer with its time, and its record in the words `corbie run show --record` prints. *Test:* `runs_own_run_shown_in_full`

**C-runs-01** A file of a run's result is sent as a download, with `Content-Disposition: attachment` and `X-Content-Type-Options: nosniff`. A result can hold HTML or SVG a script fetched from anywhere, and shown in a page it would run with the UI's cookie. *Test:* `runs_result_file_is_a_download`

**C-runs-02** A run another client started is read only through the list and Corbie's client package, which gives its status and nothing from its record. The UI holds the controller's keys too, and must not read a run another way. *Test:* `runs_other_clients_run_shows_no_record`

## Acting on a run

**R-runs-05** Reply, Send and Cancel run `corbie run reply`, `corbie run send` and `corbie run cancel`, an answer and an event's data written out again from the JSON typed; Reply runs on any run with a ticket's value, Send and Cancel only on the client's own runs, and with the controller Cancel on another client's run runs `corbie controller cancel`. *Test:* `runs_actions_run_their_commands`

## Interfaces

Runs uses Pages for the shell, the stream and the commands its buttons run (`page.Shell`, `page.Write`, `page.Stream`, `page.Part`, `page.Call`, `page.Command`), and Drawing to draw a run's machine (`drawing.Draw`, `drawing.Run`).
