package applog

import (
	"net/url"

	tea "charm.land/bubbletea/v2"

	"github.com/lasseh/taillight/internal/tui/component"
	"github.com/lasseh/taillight/internal/tui/theme"
	"github.com/lasseh/taillight/internal/tui/view/logview"
)

// FilterModel is the applog filter. It wraps the shared search input plus the
// extra fields accepted by the applog SSE endpoint (service, component, host,
// level). Metadata for typeahead is loaded from the API.
type FilterModel struct {
	search     logview.SearchFilter
	service    string
	component  string
	host       string
	level      string // minimum level: WARN → WARN + ERROR + FATAL
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
	return f.search.Search() != "" || f.service != "" || f.component != "" ||
		f.host != "" || f.level != ""
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
	if f.service != "" {
		v.Set("service", f.service)
	}
	if f.component != "" {
		v.Set("component", f.component)
	}
	if f.host != "" {
		v.Set("host", f.host)
	}
	if f.level != "" {
		v.Set("level", f.level)
	}
	return v
}

// View implements logview.Filter — renders the filter bar.
func (f *FilterModel) View(width int) string {
	bar := f.search.Input.View()
	return theme.FilterBar.Width(width).Render(bar)
}

// Clear resets every field to its empty state.
func (f *FilterModel) Clear() {
	f.search.Input.SetValue("")
	f.service = ""
	f.component = ""
	f.host = ""
	f.level = ""
	f.search.MarkDirty()
}

// ApplyPopupValues copies values emitted by a FilterPopup into this filter.
func (f *FilterModel) ApplyPopupValues(values map[string]string) {
	if s, ok := values["search"]; ok {
		f.search.Input.SetValue(s)
		f.search.MarkDirty()
	}
	if v, ok := values["service"]; ok {
		f.service = v
	}
	if v, ok := values["component"]; ok {
		f.component = v
	}
	if v, ok := values["host"]; ok {
		f.host = v
	}
	if v, ok := values["level"]; ok {
		f.level = v
	}
}

// PopupFields builds the list of popup fields with current values and
// metadata-driven suggestions, ready for component.NewFilterPopup.
func (f *FilterModel) PopupFields() []component.Field {
	return []component.Field{
		{
			Key:         "service",
			Label:       "Service",
			Kind:        component.FieldText,
			Suggestions: f.services,
			Value:       f.service,
		},
		{
			Key:         "component",
			Label:       "Component",
			Kind:        component.FieldText,
			Suggestions: f.components,
			Value:       f.component,
		},
		{
			Key:         "host",
			Label:       "Host",
			Kind:        component.FieldText,
			Suggestions: f.hosts,
			Value:       f.host,
		},
		{
			Key:     "level",
			Label:   "Level",
			Kind:    component.FieldDropdown,
			Options: levelOptions(),
			Value:   f.level,
		},
		{
			Key:   "search",
			Label: "Search",
			Kind:  component.FieldText,
			Value: f.search.Search(),
		},
	}
}

// levelOptions lists applog levels from most-severe to least-severe. Selecting
// "WARN" matches WARN + ERROR + FATAL (the API uses min-level semantics).
func levelOptions() []component.Option {
	return []component.Option{
		{Value: "", Label: "any"},
		{Value: "FATAL", Label: "FATAL"},
		{Value: "ERROR", Label: "ERROR"},
		{Value: "WARN", Label: "WARN"},
		{Value: "INFO", Label: "INFO"},
		{Value: "DEBUG", Label: "DEBUG"},
	}
}

var _ logview.Filter = (*FilterModel)(nil)
