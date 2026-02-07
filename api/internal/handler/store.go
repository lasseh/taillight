// Package handler provides HTTP handlers for the taillight API.
package handler

import (
	"context"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

// SyslogStore defines the syslog data access interface.
type SyslogStore interface {
	Ping(ctx context.Context) error
	GetSyslog(ctx context.Context, id int64) (model.SyslogEvent, error)
	ListSyslogs(ctx context.Context, f model.SyslogFilter, cursor *model.Cursor, limit int) ([]model.SyslogEvent, *model.Cursor, error)
	ListSyslogsSince(ctx context.Context, f model.SyslogFilter, sinceID int64, limit int) ([]model.SyslogEvent, error)
	ListHosts(ctx context.Context) ([]string, error)
	ListPrograms(ctx context.Context) ([]string, error)
	ListTags(ctx context.Context) ([]string, error)
	ListFacilities(ctx context.Context) ([]int, error)
	GetVolume(ctx context.Context, interval model.VolumeInterval, rangeDur time.Duration) ([]model.VolumeBucket, error)
	LookupJuniperRef(ctx context.Context, name string) ([]model.JuniperSyslogRef, error)
}

// AppLogStore defines the application log data access interface.
type AppLogStore interface {
	GetAppLog(ctx context.Context, id int64) (model.AppLogEvent, error)
	ListAppLogs(ctx context.Context, f model.AppLogFilter, cursor *model.Cursor, limit int) ([]model.AppLogEvent, *model.Cursor, error)
	ListAppLogsSince(ctx context.Context, f model.AppLogFilter, sinceID int64, limit int) ([]model.AppLogEvent, error)
	ListServices(ctx context.Context) ([]string, error)
	ListComponents(ctx context.Context) ([]string, error)
	ListAppLogHosts(ctx context.Context) ([]string, error)
	GetAppLogVolume(ctx context.Context, interval model.VolumeInterval, rangeDur time.Duration) ([]model.VolumeBucket, error)
	InsertLogBatch(ctx context.Context, events []model.AppLogEvent) ([]model.AppLogEvent, error)
}
