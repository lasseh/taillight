// Package applog wraps the generic logview package with applog-specific
// columns, row rendering, detail panel, and filter.
package applog

import (
	"github.com/lasseh/taillight/internal/tui/client"
	"github.com/lasseh/taillight/internal/tui/view/logview"
)

// Model is an applog log view, type-aliased from the generic logview.Model.
type Model = logview.Model[client.AppLogEvent]

// New creates a new applog view model with the appropriate adapter and filter.
func New(bufferSize int, timeFormat string) Model {
	filter := newFilter()
	return logview.New(bufferSize, timeFormat, adapter, filter)
}

// SetMeta updates the filter's metadata from the API response.
func SetMeta(m *Model, services, components, hosts []string) {
	if f, ok := m.Filter().(*FilterModel); ok {
		f.SetMeta(services, components, hosts)
	}
}

// adapter is the applog-specific Adapter for logview.Model.
var adapter = logview.Adapter[client.AppLogEvent]{
	Columns: columns,
	Row:     eventToRow,
	Detail:  renderDetailPanel,
	ID:      func(e client.AppLogEvent) int64 { return e.ID },
}
