// Package handler provides HTTP handlers for the taillight API.
package handler

import (
	"context"
	"time"

	"github.com/lasseh/taillight/internal/model"
)

// SrvlogStore defines the srvlog data access interface.
//
// SSE backfill dedup relies on the result ordering of the two list methods:
// ListSrvlogs returns newest-first (DESC); ListSrvlogsSince returns
// chronological (ASC). Implementations must preserve this.
type SrvlogStore interface {
	GetSrvlog(ctx context.Context, id int64) (model.SrvlogEvent, error)
	ListSrvlogs(ctx context.Context, f model.SrvlogFilter, cursor *model.Cursor, limit int) ([]model.SrvlogEvent, *model.Cursor, error)
	ListSrvlogsSince(ctx context.Context, f model.SrvlogFilter, sinceID int64, limit int) ([]model.SrvlogEvent, error)
	ListSrvlogHosts(ctx context.Context) ([]string, error)
	ListSrvlogPrograms(ctx context.Context) ([]string, error)
	ListSrvlogTags(ctx context.Context) ([]string, error)
	ListSrvlogFacilities(ctx context.Context) ([]int, error)
	GetSrvlogDeviceSummary(ctx context.Context, hostname string) (model.SrvlogDeviceSummary, error)
}

// NetlogStore defines the netlog data access interface.
//
// As with SrvlogStore, ListNetlogs returns newest-first (DESC) and
// ListNetlogsSince returns chronological (ASC); SSE backfill dedup depends
// on this ordering.
type NetlogStore interface {
	GetNetlog(ctx context.Context, id int64) (model.NetlogEvent, error)
	ListNetlogs(ctx context.Context, f model.NetlogFilter, cursor *model.Cursor, limit int) ([]model.NetlogEvent, *model.Cursor, error)
	ListNetlogsSince(ctx context.Context, f model.NetlogFilter, sinceID int64, limit int) ([]model.NetlogEvent, error)
	ListNetlogHosts(ctx context.Context) ([]string, error)
	ListNetlogPrograms(ctx context.Context) ([]string, error)
	ListNetlogTags(ctx context.Context) ([]string, error)
	ListNetlogFacilities(ctx context.Context) ([]int, error)
	GetNetlogVolume(ctx context.Context, interval model.VolumeInterval, rangeDur time.Duration) ([]model.VolumeBucket, error)
	GetNetlogSeverityVolume(ctx context.Context, interval model.VolumeInterval, rangeDur time.Duration) ([]model.SeverityVolumeBucket, error)
	GetNetlogSummary(ctx context.Context, rangeDur time.Duration) (model.SyslogSummary, error)
	GetNetlogDeviceSummary(ctx context.Context, hostname string) (model.NetlogDeviceSummary, error)
	LookupJuniperRef(ctx context.Context, name string) ([]model.JuniperNetlogRef, error)
}

// NetboxStore is a narrow interface for the Netbox enrichment handler — it
// only needs to fetch a netlog event by id.
type NetboxStore interface {
	GetNetlog(ctx context.Context, id int64) (model.NetlogEvent, error)
}

// AppLogStore defines the application log data access interface.
//
// As with SrvlogStore, ListAppLogs returns newest-first (DESC) and
// ListAppLogsSince returns chronological (ASC); SSE backfill dedup depends
// on this ordering.
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
