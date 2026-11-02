# Pages

Every page of the UI shares a header, a look, and a way of following the store. This area is those.

## The header

The header is at the top of every page. It holds Corbie's logo, the pages the UI offers, the parties it acts as, and a dot that pulses while the page follows the store and turns rowan when the store cannot be read.

```
Runs         the list of runs, and each run's page
Machines     the definitions in the directory
Parts        the parts in the directory, and what opens each
Controller   runners, clients and secrets, when the UI acts as the controller
Manual       Corbie's manual, as corbie manual web serves it, in Corbie's own look
```

## The look

The UI is a dashboard. It uses the whole window, with a 20px margin and no column of fixed width.

- Data is set in IBM Plex Mono at 13px, and labels and buttons in IBM Plex Sans. Both are in the binary, and no page of the UI fetches anything from another site.
- A list has one line for each thing, 31px high, with a thin rule between lines.
- A page carries labels and data. It has no sentences that explain it.
- A status is a dot and a word, and a failure adds its kind in brackets: `failed (denied)`. The dot is petrol for running, moss for done and rowan for failed.
- A machine's type is an icon beside its name, drawn as its drawing draws the type: stages for a flow, rounded states for a statechart, and the model's star for an agent.
- Corbie's gradient, from petrol through blue to violet, colours the icons of the types and the timeline's edge at now. The status colours are used for status only.
- A page follows the computer's light or dark setting, with the manual's colours.

## Commands

Every button that changes the store runs one of Corbie's commands. Holding the pointer over it, or moving to it with the keyboard, shows the command:

```
Cancel    $ corbie run cancel laptop-client/20261015T091204.118Z-3be1c07a2f
```

Save and New on the Machines page write files in the directory, and run no command.

## Following the store

A page that shows something that changes follows it.

1. As the page loads, it opens a WebSocket to the UI.
2. The UI draws each part that changed with the template that drew it in the page, and sends it.
3. The page puts the part in its place.

A part is drawn by one template, on the first load and on every change. When the connection is cut, the page opens it again and is sent every part as it is then. A browser holds no more than six requests open to one address, and a WebSocket is not one of them, so any number of pages can be open.

## Motion

Motion shows a change, and nothing else moves.

```
a run that is running      its dot pulses, every 2.4s
a line that appears        it fades in, lit in petrol that fades over 1.6s
a line that changes        it is lit for 1.2s
a run that ends            its line is lit in its status's colour, and moves to the ended runs; its bar takes that colour
a bar that appears         it grows from nothing at now, in 0.8s
time passing               the timeline moves left, and the bars of running runs grow to now
a record entry             it slides in from below, in 0.35s
the state a run is in      its outline glows, every 2.6s
a transition just taken    a dash moves along its arrow, three times
a count that changes       it lifts and settles, in 0.5s
another page               the pages cross-fade, in 0.18s
```

With the computer's setting for reduced motion, nothing moves.

## Errors

When an action fails, the error its command printed is shown beside its button, untouched. A page that cannot read the store keeps what it shows, turns the header's dot rowan, and reads again at the interval.
