package model

import (
	"encoding/json"
	"time"
)

// RsyslogStatsSummary contains aggregated KPIs for a time range.
type RsyslogStatsSummary struct {
	TotalSubmitted   int64                   `json:"total_submitted"`
	TotalProcessed   int64                   `json:"total_processed"`
	TotalFailed      int64                   `json:"total_failed"`
	TotalSuspended   int64                   `json:"total_suspended"`
	MainQueueSize    int64                   `json:"main_queue_size"`
	MainQueueMaxSize int64                   `json:"main_queue_max_size"`
	TotalDiscarded   int64                   `json:"total_discarded"`
	FilterRate       float64                 `json:"filter_rate"`
	FailureRate      float64                 `json:"failure_rate"`
	IngestRate       float64                 `json:"ingest_rate"`
	Components       []RsyslogStatsComponent `json:"components"`
}

// RsyslogStatsComponent is the latest snapshot for one rsyslog component.
type RsyslogStatsComponent struct {
	CollectedAt time.Time       `json:"collected_at"`
	Origin      string          `json:"origin"`
	Name        string          `json:"name"`
	Stats       json.RawMessage `json:"stats"`
}

// RsyslogStatsTimeSeries is one time-bucketed data point.
type RsyslogStatsTimeSeries struct {
	Time  time.Time `json:"time"`
	Name  string    `json:"name"`
	Value float64   `json:"value"`
}
