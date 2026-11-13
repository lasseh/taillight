package handler

import "time"

// A message search walks every chunk in its time window when the term matches
// few rows, so the window bounds what one search can cost. The list routes
// reject a search with no lower bound or a window longer than maxSearchSpan;
// a stream's backfill and resume queries clamp theirs instead, so clients that
// search only on streams need no change.
const (
	// searchNeedsFrom is the 400 message for a list search without 'from'.
	searchNeedsFrom = "search requires 'from' (RFC3339) to bound the time window"
	// searchSpanTooLong is the 400 message for a list search whose window
	// is longer than maxSearchSpan.
	searchSpanTooLong = "search window from 'from' to 'to' (or now) must be 31 days or less"
	// maxSearchSpan is the longest time window a search may cover.
	maxSearchSpan = 31 * 24 * time.Hour
	// sseSearchWindow is the backfill window of a searching stream that set
	// no 'from'.
	sseSearchWindow = 24 * time.Hour
)

// searchWindowError returns the 400 message for a list search whose time
// window is missing or too long, or "" when the window is fine or there is no
// search.
func searchWindowError(search string, from, to *time.Time) string {
	if search == "" {
		return ""
	}
	if from == nil {
		return searchNeedsFrom
	}
	if searchEnd(to).Sub(*from) > maxSearchSpan {
		return searchSpanTooLong
	}
	return ""
}

// searchBackfillFrom returns the lower time bound for a stream's backfill or
// resume query: for a search, from as given but no more than maxSearchSpan
// before to (or now), or sseSearchWindow before it when from is unset.
// Without a search it returns from unchanged.
func searchBackfillFrom(search string, from, to *time.Time) *time.Time {
	if search == "" {
		return from
	}
	end := searchEnd(to)
	if from == nil {
		start := end.Add(-sseSearchWindow)
		return &start
	}
	if earliest := end.Add(-maxSearchSpan); from.Before(earliest) {
		return &earliest
	}
	return from
}

// searchEnd is the upper end of a search window: to, or now when it is unset.
func searchEnd(to *time.Time) time.Time {
	if to != nil {
		return *to
	}
	return time.Now()
}
