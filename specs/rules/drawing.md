# Drawing

The rules behind [the Drawing overview](../drawing.md). Area code: `drawing`.

## A flow

**R-drawing-01** A stage is drawn below every stage it waits for, by the flow package's own rule, stages that run at the same time are drawn side by side, and no arrow is drawn from a stage it waits for through another. *Test:* `drawing_flow_stage_follows_what_it_waits_for`

## A statechart

**R-drawing-02** A transition to a later state is an arrow with a chip giving its event, its condition as written and a timer's time, one arrow for all the rules between two states; a timer's arrow is dashed; a transition to an earlier state or the same state is written in its card with no arrow; a state's ticket and timers are written in it, and a final state that fails is marked as failed. *Test:* `drawing_statechart_arrows_name_event_condition_and_timer`

**R-drawing-05** A `send:` that names a machine is drawn as a box of its own below the state, with a dashed arrow labelled with the event. *Test:* `drawing_statechart_send_is_a_box`

## An agent

**R-drawing-06** An agent is drawn as its model, a box of its tools, with each part a box holding its tools and its ask, and `reply`, one below another, with two arrows; a tool with `approve: true` says so. *Test:* `drawing_agent_is_a_loop_of_model_and_tools`

## A machine inside a machine

**R-drawing-07** A card whose job names a machine is a link that unfolds the machine's drawing inside the card, at any depth, and the same card then folds it. A job whose machine is an expression has no link. *Test:* `drawing_machine_unfolds_in_its_box`

## A run on the drawing

**R-drawing-03** A run is marked on its drawing: a flow's stages by their status, a statechart's states as current, entered before or never entered, and an agent's model or the tools its latest step started. *Test:* `drawing_run_is_marked_on_the_drawing`

**R-drawing-04** The transition the latest step took is marked on its arrow or in its card, and no other. *Test:* `drawing_transition_just_taken_is_marked`

## Interfaces

Drawing uses no other area of the UI.
