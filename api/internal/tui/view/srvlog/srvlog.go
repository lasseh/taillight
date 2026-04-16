// Package srvlog wraps the generic logview package with srvlog-specific
// columns, row rendering, detail panel, and filter.
package srvlog

import (
	"github.com/lasseh/taillight/internal/tui/client"
	"github.com/lasseh/taillight/internal/tui/view/logview"
)

// Model is a srvlog log view, type-aliased from the generic logview.Model.
type Model = logview.Model[client.SrvlogEvent]

// New creates a new srvlog view model with the appropriate adapter and filter.
func New(bufferSize int, timeFormat string) Model {
	filter := newFilter()
	return logview.New(bufferSize, timeFormat, adapter, filter)
}

// SetMeta updates the filter's metadata from the API response. The app calls
// this when MetaLoadedMsg arrives. Type assertion is safe because we always
// construct the model with a *FilterModel.
func SetMeta(m *Model, hosts, programs []string) {
	if f, ok := m.Filter().(*FilterModel); ok {
		f.SetMeta(hosts, programs)
	}
}

// Filter returns the srvlog FilterModel from a Model for popup access. The
// cast is safe because we always construct models with *FilterModel.
func Filter(m *Model) *FilterModel {
	if f, ok := m.Filter().(*FilterModel); ok {
		return f
	}
	return nil
}

// adapter is the srvlog-specific Adapter for logview.Model.
var adapter = logview.Adapter[client.SrvlogEvent]{
	Columns: columns,
	Row:     eventToRow,
	Detail:  renderDetailPanel,
	ID:      func(e client.SrvlogEvent) int64 { return e.ID },
}
