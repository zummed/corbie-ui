// Command corbie-ui is the UI, which corbie ui runs.
package main

import (
	"os"

	"github.com/zummed/corbie-ui/serve"
	"github.com/zummed/corbie/client/cli"

	_ "github.com/zummed/corbie/agent"
	_ "github.com/zummed/corbie/flow"
	_ "github.com/zummed/corbie/plugin/bundled"
	_ "github.com/zummed/corbie/statechart"
)

func main() {
	os.Exit(cli.Program("ui", serve.Commands, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
