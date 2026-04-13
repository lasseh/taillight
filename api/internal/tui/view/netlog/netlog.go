// Package netlog wraps the generic logview package with netlog-specific
// columns, row rendering, detail panel, and filter.
//
// Netlog events have the same schema as srvlog events, so this package's
// adapter is structurally identical — only the SSE endpoint and stream
// label differ (managed by the parent app).
package netlog

import (
	"github.com/lasseh/taillight/internal/tui/client"
	"github.com/lasseh/taillight/internal/tui/view/logview"
)

// Model is a netlog log view, type-aliased from the generic logview.Model.
type Model = logview.Model[client.NetlogEvent]

// New creates a new netlog view model with the appropriate adapter and filter.
func New(bufferSize int, timeFormat string) Model {
	filter := newFilter()
	return logview.New(bufferSize, timeFormat, adapter, filter)
}

// SetMeta updates the filter's metadata from the API response.
func SetMeta(m *Model, hosts, programs []string) {
	if f, ok := m.Filter().(*FilterModel); ok {
		f.SetMeta(hosts, programs)
	}
}

// adapter is the netlog-specific Adapter for logview.Model.
var adapter = logview.Adapter[client.NetlogEvent]{
	Columns: columns,
	Row:     eventToRow,
	Detail:  renderDetailPanel,
	ID:      func(e client.NetlogEvent) int64 { return e.ID },
}
