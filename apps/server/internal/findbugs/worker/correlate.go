package worker

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/correlation"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/splunk"
	"github.com/falahlaz/find-bugs-web/apps/server/internal/platform/store"
)

// followBackendIDs re-searches every backend `_id` linked to the job's
// transaction ID (see package correlation) and merges those traces into res.
// It returns the merged result and the IDs whose search added logs. A failed
// backend search is logged and skipped: the first-hop logs are still worth
// analysing.
func (w *Worker) followBackendIDs(ctx context.Context, log *slog.Logger, job store.Job, res splunk.Result) (splunk.Result, []string) {
	raws := make([]string, len(res.Events))
	for i, ev := range res.Events {
		raws[i] = ev.Raw
	}
	var linked []string
	for _, id := range correlation.BackendIDs(raws, job.TransactionID, w.Cfg.CorrelationMaxIDs) {
		log.Info("following backend id", "backend_id", id)
		r, err := w.Splunk.Search(ctx, job.Environment, id, job.TimeRange)
		if ctx.Err() != nil {
			break
		}
		if err != nil {
			log.Warn("backend id search failed, skipping", "backend_id", id, "err", err)
			continue
		}
		if r.Status != splunk.ResultSuccess {
			log.Info("backend id has no logs", "backend_id", id)
			continue
		}
		res = mergeResults(res, r)
		linked = append(linked, id)
	}
	return res, linked
}

// mergeResults combines two searches: duplicate events (the "API Request"
// event matches both IDs) are dropped and the rest sorted oldest first.
func mergeResults(a, b splunk.Result) splunk.Result {
	seen := make(map[splunk.Event]bool, len(a.Events)+len(b.Events))
	var events []splunk.Event
	for _, ev := range append(append([]splunk.Event(nil), a.Events...), b.Events...) {
		if !seen[ev] {
			seen[ev] = true
			events = append(events, ev)
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return eventTime(events[i]).Before(eventTime(events[j])) })
	return splunk.Result{
		Status:     splunk.ResultSuccess,
		Logs:       splunk.RenderLogs(events),
		Events:     events,
		EventCount: a.EventCount + b.EventCount,
		Truncated:  a.Truncated || b.Truncated,
	}
}

// eventTime parses Splunk's _time; unparsable times sort first.
func eventTime(ev splunk.Event) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, ev.Time); err == nil {
			return t
		}
	}
	return time.Time{}
}
