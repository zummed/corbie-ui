# Controller

The rules behind [the Controller overview](../controller.md). Area code: `controller`.

## Runners and clients

**R-controller-01** The page lists each runner and client the controller trusts with its card's fingerprint, a runner's tags, a client's ceiling and deny list, and whether it was removed, and the names of its secrets, read as `corbie controller parties` and `corbie controller secret list` read them. *Test:* `controller_lists_parties_and_secret_names`

**R-controller-02** Enrol, Tags, Ceiling, Remove and a secret's Remove run their `corbie controller` commands, and Enrol shows the controller's card and the store's address that the command printed back. *Test:* `controller_actions_run_their_commands`

**C-controller-01** A removed party's line offers only Enrol. `corbie controller enrol <name>` writes a new trust for the name, which trusts a removed party again. *Test:* `controller_removed_party_offers_only_enrol`

## Secrets

**C-controller-02** A secret's value goes to its command on standard input, and no page and no answer carries it. On a command line it would be seen by every process on this computer. *Test:* `controller_secret_value_goes_on_standard_input_only`

## Interfaces

Controller uses Pages for the shell, the stream and the commands it runs (`page.Shell`, `page.Write`, `page.Stream`, `page.Part`, `page.Call`, `page.Command`).
