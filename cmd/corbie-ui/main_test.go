package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/zummed/corbie/client"
	"github.com/zummed/corbie/machine"
	"github.com/zummed/corbie/permission"
	"github.com/zummed/corbie/runner"
)

func Test_serve_main_runs_a_jobs_process(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "corbie-ui")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the program does not build: %v\n%s", err, out)
	}
	l, err := client.NewLocal(context.Background(), permission.Layer{Allow: []string{"all"}}, func(string, error) {})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	l.Controller.AnyRunner = true
	// The runner starts each job's process by running the program, as a
	// runner on a computer runs corbie.
	l.Runner.Command = []string{bin, runner.JobArg}
	draft, err := machine.Parse([]byte("flow: two\nstages:\n  a: { job: ./two.js }\n"), machine.Given(map[string]string{"two.js": "return 1 + 1"}))
	if err != nil {
		t.Fatal(err)
	}
	id, err := l.Start(ctx, draft, client.Start{})
	if err != nil {
		t.Fatal(err)
	}
	r, err := l.Wait(ctx, id, 50*time.Millisecond)
	if err != nil || r.Status != "done" {
		t.Errorf("a script run by the program's job process gave %v: %+v", err, r)
	}
}
