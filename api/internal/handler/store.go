// Package handler provides HTTP handlers for the taillight API.
package handler

import (
	"context"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

// SrvlogStore defines the srvlog data access interface.
type SrvlogStore interface {
	GetSrvlog(ctx context.Context, id int64) (model.SrvlogEvent, error)
	ListSrvlogs(ctx context.Context, f model.SrvlogFilter, cursor *model.Cursor, limit int) ([]model.SrvlogEvent, *model.Cursor, error)
	ListSrvlogsSince(ctx context.Context, f model.SrvlogFilter, sinceID int64, limit int) ([]model.SrvlogEvent, error)
	ListHosts(ctx context.Context) ([]string, error)
	ListPrograms(ctx context.Context) ([]string, error)
	ListTags(ctx context.Context) ([]string, error)
	ListFacilities(ctx context.Context) ([]int, error)
	GetVolume(ctx context.Context, interval model.VolumeInterval, rangeDur time.Duration) ([]model.VolumeBucket, error)
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
