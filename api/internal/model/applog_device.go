package model

import "time"

// AppLogDeviceSummary holds aggregated information for a single applog host.
type AppLogDeviceSummary struct {
	Host           string             `json:"host"`
	LastSeenAt     *time.Time         `json:"last_seen_at"`
	TotalCount     int64              `json:"total_count"`
	ErrorCount     int64              `json:"error_count"`
	LevelBreakdown []LevelCount       `json:"level_breakdown"`
	TopMessages    []AppLogTopMessage `json:"top_messages"`
	ErrorLogs      []AppLogEvent      `json:"error_logs"`
}

// AppLogTopMessage holds a normalized message pattern with its count and a sample.
type AppLogTopMessage struct {
	Pattern  string    `json:"pattern"`
	Sample   string    `json:"sample"`
	Count    int64     `json:"count"`
	LatestID int64     `json:"latest_id"`
	LatestAt time.Time `json:"latest_at"`
	Level    string    `json:"level"`
}
