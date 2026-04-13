package applog

import (
	"net/url"

	tea "charm.land/bubbletea/v2"

	"github.com/lasseh/taillight/internal/tui/theme"
	"github.com/lasseh/taillight/internal/tui/view/logview"
)

// FilterModel is the applog-specific filter. It wraps the shared search
// input and adds applog metadata (services, components, hosts) for display.
type FilterModel struct {
	search     logview.SearchFilter
	services   []string
	components []string
	hosts      []string
}

func newFilter() *FilterModel {
	return &FilterModel{search: logview.NewSearchFilter()}
}

// Update implements logview.Filter.
func (f *FilterModel) Update(msg tea.Msg) (logview.Filter, tea.Cmd) {
	cmd := f.search.UpdateInput(msg)
	return f, cmd
}

// Focus implements logview.Filter.
func (f *FilterModel) Focus() tea.Cmd { return f.search.Focus() }

// Blur implements logview.Filter.
func (f *FilterModel) Blur() { f.search.Blur() }

// Dirty implements logview.Filter.
func (f *FilterModel) Dirty() bool { return f.search.Dirty() }

// AckDirty implements logview.Filter.
func (f *FilterModel) AckDirty() { f.search.AckDirty() }

// HasActiveFilter implements logview.Filter.
func (f *FilterModel) HasActiveFilter() bool {
	return f.search.Search() != ""
}

// SetMeta updates the available services/components/hosts (loaded from API).
func (f *FilterModel) SetMeta(services, components, hosts []string) {
	f.services = services
	f.components = components
	f.hosts = hosts
}

// Params implements logview.Filter — returns SSE stream query params.
func (f *FilterModel) Params() url.Values {
	v := url.Values{}
	if s := f.search.Search(); s != "" {
		v.Set("search", s)
	}
	return v
}

// View implements logview.Filter — renders the filter bar.
func (f *FilterModel) View(width int) string {
	bar := f.search.Input.View()
	return theme.FilterBar.Width(width).Render(bar)
}

var _ logview.Filter = (*FilterModel)(nil)
