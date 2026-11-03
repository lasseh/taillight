package handler

import "time"

// A message search with no lower time bound walks every retained chunk when
// the term matches few rows. The list routes reject it; a stream's
// connect-time backfill falls back to a default window instead, so clients
// that search only on streams need no change.
const (
	// searchNeedsFrom is the 400 message for a list search without 'from'.
	searchNeedsFrom = "search requires 'from' (RFC3339) to bound the time window"
	// sseSearchWindow is the backfill window of a searching stream that set
	// no 'from'.
	sseSearchWindow = 24 * time.Hour
)

// searchBackfillFrom returns the lower time bound for a stream's backfill:
// from as given, or, for a search with none, sseSearchWindow before to (or
// before now).
func searchBackfillFrom(search string, from, to *time.Time) *time.Time {
	if search == "" || from != nil {
		return from
	}
	end := time.Now()
	if to != nil {
		end = *to
	}
	start := end.Add(-sseSearchWindow)
	return &start
}
