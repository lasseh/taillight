package model

import "time"

// JuniperSyslogRef represents a row from the juniper_syslog_ref table.
type JuniperSyslogRef struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Message     string    `json:"message"`
	Description string    `json:"description"`
	Type        string    `json:"type"`
	Severity    string    `json:"severity"`
	Cause       string    `json:"cause"`
	Action      string    `json:"action"`
	OS          string    `json:"os"`
	CreatedAt   time.Time `json:"created_at"`
}
