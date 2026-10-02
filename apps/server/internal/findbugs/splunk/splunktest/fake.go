// Package splunktest provides a fake Splunk REST API for tests and local dev.
package splunktest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Fake serves the subset of the Splunk API the client uses.
type Fake struct {
	mu sync.Mutex
	// Expired makes every request answer 401.
	Expired bool
	// Logs maps a substring of the search to the returned raw events; a
	// search matching nothing returns zero events.
	Logs map[string][]string
	// Searches records every search string received.
	Searches  []string
	Cancelled []string
	Cookie    string // last Cookie header
	// EventPages counts /events requests.
	EventPages int
	FormKey    string // last X-Splunk-Form-Key header
	seq        int
	jobs       map[string]string
}

// New returns a Fake with no logs.
func New() *Fake { return &Fake{Logs: map[string][]string{}, jobs: map[string]string{}} }

// SetExpired toggles the expired-session mode.
func (f *Fake) SetExpired(v bool) { f.mu.Lock(); f.Expired = v; f.mu.Unlock() }

// SetLogs sets events returned for searches containing key.
func (f *Fake) SetLogs(key string, lines ...string) {
	f.mu.Lock()
	f.Logs[key] = lines
	f.mu.Unlock()
}

func (f *Fake) match(search string) []string {
	for k, v := range f.Logs {
		if strings.Contains(search, k) {
			return v
		}
	}
	return nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// ServeHTTP implements http.Handler.
func (f *Fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Cookie, f.FormKey = r.Header.Get("Cookie"), r.Header.Get("X-Splunk-Form-Key")
	if f.Expired {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/en-US/splunkd/__raw")
	switch {
	case p == "/services/authentication/current-context":
		writeJSON(w, map[string]any{"entry": []any{map[string]any{"content": map[string]any{"username": "fake"}}}})
	case p == "/services/search/v2/jobs" && r.Method == http.MethodPost:
		r.ParseForm()
		f.seq++
		sid := fmt.Sprintf("sid-%d", f.seq)
		f.jobs[sid] = r.Form.Get("search")
		f.Searches = append(f.Searches, r.Form.Get("search"))
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, map[string]string{"sid": sid})
	case strings.HasSuffix(p, "/control"):
		sid := strings.TrimSuffix(strings.TrimPrefix(p, "/services/search/v2/jobs/"), "/control")
		f.Cancelled = append(f.Cancelled, sid)
		writeJSON(w, map[string]any{})
	case strings.HasSuffix(p, "/events"):
		sid := strings.TrimSuffix(strings.TrimPrefix(p, "/services/search/v2/jobs/"), "/events")
		// Like Splunk: newest first, paged by offset/count.
		lines := f.match(f.jobs[sid])
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		count, _ := strconv.Atoi(r.URL.Query().Get("count"))
		f.EventPages++
		res := []any{}
		for i := len(lines) - 1 - offset; i >= 0 && (count <= 0 || len(res) < count); i-- {
			res = append(res, map[string]any{
				"_raw":  map[string]string{"value": lines[i]},
				"_time": fmt.Sprintf("2026-10-01T08:%02d:%02d", i/60%60, i%60),
				"host":  "fake-host", "source": "fake-service", "sourcetype": "_json",
			})
		}
		writeJSON(w, map[string]any{"results": res})
	case strings.HasPrefix(p, "/services/search/v2/jobs/"):
		sid := strings.TrimPrefix(p, "/services/search/v2/jobs/")
		search, ok := f.jobs[sid]
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]any{"entry": []any{map[string]any{"content": map[string]any{
			"isDone": true, "dispatchState": "DONE", "eventCount": len(f.match(search)),
		}}}})
	default:
		http.NotFound(w, r)
	}
}
