# Controller

The Controller page lists the runners and clients the controller trusts and the names of its secrets, and changes them. It is offered when the UI acts as the controller.

The page reads the parties and the secrets' names through Corbie's packages, as `corbie controller parties` and `corbie controller secret list` read them. Each action runs its `corbie controller` command, with the controller's config, which is why the page needs the controller.

## Runners and clients

Each is one line: its name, its card's fingerprint, and a runner's tags or a client's ceiling and deny list. One that was removed is listed as removed.

```
Enrol     corbie controller enrol <card> --tags <list>      trusts a runner, with its tags
Enrol     corbie controller enrol <card> --ceiling <list>   trusts a client, with its ceiling
Tags      corbie controller enrol <name> --tags <list>      sets a runner's tags again
Ceiling   corbie controller enrol <name> --ceiling <list>   sets a client's ceiling again
Remove    corbie controller remove <name>                   stops trusting it
```

Enrol shows what the command prints back, the controller's card and the store's address, for the other side's `corbie runner trust` or `corbie client trust`. A card with another key under a name already used is refused, unless Replace is ticked, as `--replace` allows it.

Remove keeps the name with its key, marked as removed and signed by the controller. A removed party's line offers only Enrol, since setting its tags or ceiling again would trust it again.

## Secrets

Secrets are listed by name.

```
Set      corbie controller secret set <NAME> -    keeps a secret, in place of any kept before under the name
Remove   corbie controller secret remove <NAME>   deletes it
```

A value is typed into a field that does not show it, and goes to the command on its standard input, never on its command line. No page shows a value, and none is sent back.
