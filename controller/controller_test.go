package controller

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zummed/corbie-ui/page"
	"github.com/zummed/corbie/client/cli"
	"github.com/zummed/corbie/identity"
	"github.com/zummed/corbie/store"
)

func corbie(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	var out, said bytes.Buffer
	if status := cli.Run(context.Background(), args, strings.NewReader(stdin), &out, &said); status != 0 {
		t.Fatalf("corbie %s: %s%s", strings.Join(args, " "), said.String(), out.String())
	}
	return out.String()
}

// lab sets one computer up, enrols a runner with tags and removes it, and
// keeps one secret; it gives the Controller page on it.
func lab(t *testing.T, command page.Command) http.Handler {
	t.Helper()
	dir := t.TempDir()
	config, at := filepath.Join(dir, "config"), filepath.Join(dir, "store")
	corbie(t, "", "init", "--store", at, "--name", "lab", "--ceiling", "network,disk", "--config", config)
	card := regexp.MustCompile(`corbie-card:\S+`).FindString(corbie(t, "", "runner", "card", "--name", "gpu-box", "--config", filepath.Join(dir, "other")))
	corbie(t, "", "controller", "enrol", card, "--tags", "gpu,big", "--config", config)
	corbie(t, "", "runner", "card", "--name", "old-box", "--config", filepath.Join(dir, "old"))
	old := regexp.MustCompile(`corbie-card:\S+`).FindString(corbie(t, "", "runner", "card", "--config", filepath.Join(dir, "old")))
	corbie(t, "", "controller", "enrol", old, "--config", config)
	corbie(t, "", "controller", "remove", "old-box", "--config", config)
	corbie(t, "swordfish", "controller", "secret", "set", "ANTHROPIC_KEY", "-", "--config", config)
	ctl, err := identity.Config{Dir: config}.Load(identity.Controller)
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(at)
	if err != nil {
		t.Fatal(err)
	}
	return New(Config{Controller: ctl, Store: s, Command: command, Interval: time.Second, Shell: page.Shell{}})
}

func get(t *testing.T, h http.Handler) string {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/controller", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("the page answered %d: %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

func post(h http.Handler, act string, form url.Values) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/controller/"+act, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(w, r)
	return w
}

func Test_controller_lists_parties_and_secret_names(t *testing.T) {
	body := get(t, lab(t, nil))
	for _, want := range []string{"lab-runner", "lab-client", "network, disk", "gpu-box", "gpu, big", `<tr data-key="old-box" class="removed">`, "ANTHROPIC_KEY"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not show %s", want)
		}
	}
	if !regexp.MustCompile(`<td class="dim">[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}</td>`).MatchString(body) {
		t.Errorf("the page does not show a card's fingerprint")
	}
}

// recorder runs no command, and keeps every call it is given.
type recorder struct{ calls []page.Call }

func (r *recorder) run(_ context.Context, c page.Call) page.Answer {
	r.calls = append(r.calls, c)
	return page.Answer{Out: `{"card":"corbie-card:controller:lab-controller:x","store":"/srv/store"}`, Said: "enrolled the runner gpu-box\n"}
}

func Test_controller_actions_run_their_commands(t *testing.T) {
	r := &recorder{}
	h := lab(t, r.run)
	w := post(h, "enrol", url.Values{"card": {" corbie-card:runner:new-box:abc "}, "tags": {"gpu, big"}, "replace": {"1"}})
	if !strings.Contains(w.Body.String(), "corbie-card:controller:lab-controller:x\n/srv/store") {
		t.Errorf("Enrol did not show the controller's card and the store's address: %q", w.Body.String())
	}
	post(h, "enrol", url.Values{"card": {"corbie-card:client:new-client:abc"}, "ceiling": {"network,disk"}})
	post(h, "tags", url.Values{"name": {"gpu-box"}, "tags": {"gpu"}})
	post(h, "ceiling", url.Values{"name": {"lab-client"}, "ceiling": {"all"}})
	post(h, "remove", url.Values{"name": {"gpu-box"}})
	post(h, "secret-remove", url.Values{"name": {"ANTHROPIC_KEY"}})
	want := []string{
		"corbie controller enrol corbie-card:runner:new-box:abc --tags=gpu,big --replace",
		"corbie controller enrol corbie-card:client:new-client:abc --ceiling=network,disk",
		"corbie controller enrol gpu-box --tags=gpu",
		"corbie controller enrol lab-client --ceiling=all",
		"corbie controller remove gpu-box",
		"corbie controller secret remove ANTHROPIC_KEY",
	}
	if len(r.calls) != len(want) {
		t.Fatalf("the actions ran %v", r.calls)
	}
	for i, c := range r.calls {
		if c.String() != want[i] {
			t.Errorf("an action ran %q, not %q", c.String(), want[i])
		}
		if found, _ := cli.Find(c.Words); found == nil || found.Name != strings.Join(c.Words, " ") {
			t.Errorf("%q is not one of Corbie's commands", strings.Join(c.Words, " "))
		}
	}
}

func Test_controller_removed_party_offers_only_enrol(t *testing.T) {
	r := &recorder{}
	h := lab(t, r.run)
	body := get(t, h)
	row := regexp.MustCompile(`(?s)<tr data-key="old-box" class="removed">.*?</tr>`).FindString(body)
	if row == "" || strings.Contains(row, "/controller/tags") || strings.Contains(row, "/controller/remove") || !strings.Contains(row, `href="#enrol-runner"`) {
		t.Errorf("the removed party's line offers more than Enrol:\n%s", row)
	}
	if w := post(h, "tags", url.Values{"name": {"old-box"}, "tags": {"gpu"}}); w.Code != http.StatusConflict || len(r.calls) != 0 {
		t.Errorf("setting a removed party's tags answered %d and ran %v", w.Code, r.calls)
	}
}

func Test_controller_secret_value_goes_on_standard_input_only(t *testing.T) {
	r := &recorder{}
	h := lab(t, r.run)
	w := post(h, "secret-set", url.Values{"name": {"GITHUB_TOKEN"}, "value": {"hunter2"}})
	if len(r.calls) != 1 {
		t.Fatalf("Set ran %v", r.calls)
	}
	c := r.calls[0]
	if c.Stdin != "hunter2" || strings.Contains(c.String(), "hunter2") || c.String() != "corbie controller secret set GITHUB_TOKEN -" {
		t.Errorf("Set ran %q with %q on standard input", c.String(), c.Stdin)
	}
	if strings.Contains(w.Body.String(), "hunter2") || strings.Contains(get(t, h), "swordfish") {
		t.Errorf("a secret's value was sent back to the page")
	}
}
