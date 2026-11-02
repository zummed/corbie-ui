# Corbie UI

Corbie UI shows [Corbie](https://github.com/zummed/corbie)'s runs in a browser, and does to them what Corbie's commands do. It also draws a directory of machines and the parts their agents can open, and needs no store for that.

## Start

```sh
go install github.com/zummed/corbie-ui/cmd/corbie-ui@latest
corbie ui --dir my-project
```

`corbie ui` runs the program, and prints an address to open. With no store it shows the directory's machines. `specs/README.md` says what each way of starting it shows.

| Page | What it shows |
|---|---|
| Runs | every run, its record and what waits on a person |
| Machines | each machine in a directory, drawn from top to bottom |
| Parts | each part, what opens it and what starts that |

## License

Copyright 2026 Paul Young. Licensed under the Apache License, Version 2.0: see `LICENSE` and `NOTICE`. The IBM Plex fonts are under the SIL Open Font License 1.1.

Corbie UI was written with Claude Code, designed and directed by Paul Young.
