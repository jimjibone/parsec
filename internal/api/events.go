package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Event tells open browsers that something changed, so they refetch.
type Event struct {
	Kind   string `json:"kind"`             // data, git or users
	By     string `json:"by,omitempty"`     // username
	ByName string `json:"byName,omitempty"` // display name
	Client string `json:"client,omitempty"` // browser tab that made the change
	Label  string `json:"label,omitempty"`
}

type hub struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

func newHub() *hub { return &hub{subs: map[chan Event]struct{}{}} }

// publish never blocks; a subscriber that falls behind misses events, which
// is harmless because each one only triggers a full refetch.
func (h *hub) publish(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.subs {
		select {
		case c <- e:
		default:
		}
	}
}

func (h *hub) subscribe() chan Event {
	c := make(chan Event, 16)
	h.mu.Lock()
	h.subs[c] = struct{}{}
	h.mu.Unlock()
	return c
}

func (h *hub) unsubscribe(c chan Event) {
	h.mu.Lock()
	delete(h.subs, c)
	h.mu.Unlock()
}

// pingEvery keeps proxies from closing idle streams; the web UI treats a
// stream silent for more than twice this as dead.
const pingEvery = 25 * time.Second

// serveEvents streams events as server-sent events.
func (h *hub) serveEvents(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // nginx: do not buffer
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "retry: 3000\n\n")
	if err := rc.Flush(); err != nil {
		return
	}

	c := h.subscribe()
	defer h.unsubscribe(c)
	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			// A named event, not a comment, so clients can see it and
			// notice a connection that died silently.
			fmt.Fprint(w, "event: ping\ndata: {}\n\n")
		case e := <-c:
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}
