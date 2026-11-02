package page

import (
	"context"
	"encoding/json/v2"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func Test_page_header_offers_the_parties_pages(t *testing.T) {
	w := httptest.NewRecorder()
	Write(w, Shell{Title: "Runs", Current: "Runs", Offered: []string{"Runs", "Machines"}, Parties: []string{"laptop-client"}})
	body := w.Body.String()
	for _, want := range []string{`<a href="/runs" aria-current="page">Runs</a>`, `<a href="/machines">Machines</a>`, `<code>laptop-client</code>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the header does not hold %s:\n%s", want, body)
		}
	}
	if strings.Contains(body, "/controller") {
		t.Errorf("the header offers the Controller page to a UI that is not the controller")
	}
	w = httptest.NewRecorder()
	Write(w, Shell{Title: "Runs", Current: "Runs", Offered: []string{"Runs"}})
	if !strings.Contains(w.Body.String(), "a viewer") {
		t.Errorf("a UI with no party does not say it is a viewer")
	}
}

// parts is a page whose parts change when it is told to, or fail to be
// read.
type parts struct {
	mu      sync.Mutex
	shown   []Part
	err     error
	changed chan struct{}
}

func (p *parts) set(shown []Part, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.shown, p.err = shown, err
	close(p.changed)
	p.changed = make(chan struct{})
}

func (p *parts) read(context.Context) ([]Part, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.shown, p.err
}

func (p *parts) Changed() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.changed
}

func follow(t *testing.T, p *parts) (*websocket.Conn, func() map[string]string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Stream(w, r, time.Hour, p.Changed, p.read)
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn, func() map[string]string {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]string
		json.Unmarshal(data, &m)
		return m
	}
}

func Test_page_follows_over_a_websocket(t *testing.T) {
	p := &parts{shown: []Part{{ID: "now", HTML: "<b>1</b>"}}, changed: make(chan struct{})}
	_, next := follow(t, p)
	if m := next(); m["id"] != "now" || m["html"] != "<b>1</b>" {
		t.Errorf("the page was sent %v over its WebSocket", m)
	}
}

func Test_page_stream_sends_every_part_then_what_changed(t *testing.T) {
	p := &parts{shown: []Part{{ID: "a", HTML: "one"}, {ID: "b", HTML: "two"}}, changed: make(chan struct{})}
	_, next := follow(t, p)
	first := map[string]template.HTML{}
	for range 2 {
		m := next()
		first[m["id"]] = template.HTML(m["html"])
	}
	if first["a"] != "one" || first["b"] != "two" {
		t.Fatalf("the page was not sent every part as it connected: %v", first)
	}
	p.set([]Part{{ID: "a", HTML: "one"}, {ID: "b", HTML: "three"}}, nil)
	if m := next(); m["id"] != "b" || m["html"] != "three" {
		t.Errorf("after b changed, the page was sent %v", m)
	}
	p.set([]Part{{ID: "a", HTML: "one"}, {ID: "b", HTML: "three"}}, errors.New("the store cannot be read"))
	if m := next(); m["store"] != "lost" {
		t.Errorf("a page was not told the store is lost, but sent %v", m)
	}
	p.set([]Part{{ID: "a", HTML: "four"}, {ID: "b", HTML: "three"}}, nil)
	if m := next(); m["store"] != "live" {
		t.Errorf("a page was not told the store is read again, but sent %v", m)
	}
	if m := next(); m["id"] != "a" || m["html"] != "four" {
		t.Errorf("after the store was read again, the page was sent %v", m)
	}
}
