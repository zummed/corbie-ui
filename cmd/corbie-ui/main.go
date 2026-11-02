// Command corbie-ui is the UI, which corbie ui runs.
package main

import (
	"os"

	"github.com/zummed/corbie-ui/serve"
	"github.com/zummed/corbie/client/cli"
	"github.com/zummed/corbie/runner"

	_ "github.com/zummed/corbie/agent"
	_ "github.com/zummed/corbie/flow"
	_ "github.com/zummed/corbie/plugin/bundled"
	_ "github.com/zummed/corbie/statechart"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == runner.JobArg {
		runner.JobMain()
		return
	}
	os.Exit(cli.Program("ui", serve.Commands, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
