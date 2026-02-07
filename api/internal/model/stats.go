package model

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// VolumeInterval is a validated PostgreSQL interval for time bucketing.
type VolumeInterval string

// Valid interval constants.
const (
	Interval1Min  VolumeInterval = "1 minute"
	Interval5Min  VolumeInterval = "5 minutes"
	Interval15Min VolumeInterval = "15 minutes"
	Interval30Min VolumeInterval = "30 minutes"
	Interval1Hour VolumeInterval = "1 hour"
	Interval6Hour VolumeInterval = "6 hours"
	Interval1Day  VolumeInterval = "1 day"
)

// String returns the PostgreSQL interval string.
func (i VolumeInterval) String() string {
	return string(i)
}

// IsValid returns true if the interval is a known valid value.
func (i VolumeInterval) IsValid() bool {
	switch i {
	case Interval1Min, Interval5Min, Interval15Min, Interval30Min, Interval1Hour, Interval6Hour, Interval1Day:
		return true
	}
	return false
}

// allowedIntervals maps user-facing interval labels to VolumeInterval values.
var allowedIntervals = map[string]VolumeInterval{
	"1m":  Interval1Min,
	"5m":  Interval5Min,
	"15m": Interval15Min,
	"30m": Interval30Min,
	"1h":  Interval1Hour,
	"6h":  Interval6Hour,
	"1d":  Interval1Day,
}

// allowedRanges maps user-facing range labels to Go durations.
var allowedRanges = map[string]time.Duration{
	"1h":  1 * time.Hour,
	"6h":  6 * time.Hour,
	"12h": 12 * time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

// VolumeParams holds validated parameters for a volume query.
type VolumeParams struct {
	Interval VolumeInterval // Validated PostgreSQL interval.
	RangeDur time.Duration  // Go duration for the lookback window.
}

// VolumeBucket is a single time bucket in the volume response.
type VolumeBucket struct {
	Time   time.Time        `json:"time"`
	Total  int64            `json:"total"`
	ByHost map[string]int64 `json:"by_host"`
}

// SeverityCount holds a count for a specific severity level.
type SeverityCount struct {
	Severity int     `json:"severity"`
	Label    string  `json:"label"`
	Count    int64   `json:"count"`
	Pct      float64 `json:"pct"`
}

// LevelCount holds a count for a specific log level.
type LevelCount struct {
	Level string  `json:"level"`
	Count int64   `json:"count"`
	Pct   float64 `json:"pct"`
}

// TopSource holds a count for a specific source (host or service).
type TopSource struct {
	Name  string  `json:"name"`
	Count int64   `json:"count"`
	Pct   float64 `json:"pct"`
}

// SyslogSummary contains summary statistics for syslog events.
type SyslogSummary struct {
	Total             int64           `json:"total"`
	Trend             float64         `json:"trend"` // percentage change vs previous period
	Errors            int64           `json:"errors"`
	Warnings          int64           `json:"warnings"`
	SeverityBreakdown []SeverityCount `json:"severity_breakdown"`
	TopHosts          []TopSource     `json:"top_hosts"`
}

// AppLogSummary contains summary statistics for applog events.
type AppLogSummary struct {
	Total          int64        `json:"total"`
	Trend          float64      `json:"trend"` // percentage change vs previous period
	Errors         int64        `json:"errors"`
	Warnings       int64        `json:"warnings"`
	LevelBreakdown []LevelCount `json:"level_breakdown"`
	TopServices    []TopSource  `json:"top_services"`
}

// ParseRange parses the "range" query parameter into a duration.
// Defaults to 24h if not specified.
func ParseRange(r *http.Request) (time.Duration, error) {
	rangeName := r.URL.Query().Get("range")
	if rangeName == "" {
		rangeName = "24h"
	}
	rangeDur, ok := allowedRanges[rangeName]
	if !ok {
		keys := make([]string, 0, len(allowedRanges))
		for k := range allowedRanges {
			keys = append(keys, k)
		}
		return 0, fmt.Errorf("invalid range %q, allowed: %s", rangeName, strings.Join(keys, ", "))
	}
	return rangeDur, nil
}

// ParseVolumeParams validates the interval and range query parameters.
func ParseVolumeParams(r *http.Request) (VolumeParams, error) {
	q := r.URL.Query()

	interval := q.Get("interval")
	if interval == "" {
		interval = "1h"
	}
	pgInterval, ok := allowedIntervals[interval]
	if !ok {
		keys := make([]string, 0, len(allowedIntervals))
		for k := range allowedIntervals {
			keys = append(keys, k)
		}
		return VolumeParams{}, fmt.Errorf("invalid interval %q, allowed: %s", interval, strings.Join(keys, ", "))
	}

	rangeName := q.Get("range")
	if rangeName == "" {
		rangeName = "24h"
	}
	rangeDur, ok := allowedRanges[rangeName]
	if !ok {
		keys := make([]string, 0, len(allowedRanges))
		for k := range allowedRanges {
			keys = append(keys, k)
		}
		return VolumeParams{}, fmt.Errorf("invalid range %q, allowed: %s", rangeName, strings.Join(keys, ", "))
	}

	return VolumeParams{
		Interval: pgInterval,
		RangeDur: rangeDur,
	}, nil
}
