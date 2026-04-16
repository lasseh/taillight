package netlog

import (
	"net/url"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/lasseh/taillight/internal/tui/component"
	"github.com/lasseh/taillight/internal/tui/theme"
	"github.com/lasseh/taillight/internal/tui/view/logview"
)

// FilterModel is the netlog filter. Same shape as srvlog's filter (netlogs
// share the syslog schema) but lives in its own package so the labels and
// pill colors can diverge later if needed.
type FilterModel struct {
	search      logview.SearchFilter
	hostname    string
	program     string
	severityMax int // -1 = unset
	facility    int // -1 = unset
	hosts       []string
	programs    []string
}

func newFilter() *FilterModel {
	return &FilterModel{
		search:      logview.NewSearchFilter(),
		severityMax: -1,
		facility:    -1,
	}
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
	return f.search.Search() != "" || f.hostname != "" || f.program != "" ||
		f.severityMax >= 0 || f.facility >= 0
}

// SetMeta updates the available hosts/programs (loaded from the API).
func (f *FilterModel) SetMeta(hosts, programs []string) {
	f.hosts = hosts
	f.programs = programs
}

// Params implements logview.Filter — returns SSE stream query params.
func (f *FilterModel) Params() url.Values {
	v := url.Values{}
	if s := f.search.Search(); s != "" {
		v.Set("search", s)
	}
	if f.hostname != "" {
		v.Set("hostname", f.hostname)
	}
	if f.program != "" {
		v.Set("programname", f.program)
	}
	if f.severityMax >= 0 {
		v.Set("severity_max", strconv.Itoa(f.severityMax))
	}
	if f.facility >= 0 {
		v.Set("facility", strconv.Itoa(f.facility))
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
	f.hostname = ""
	f.program = ""
	f.severityMax = -1
	f.facility = -1
	f.search.MarkDirty()
}

// ApplyPopupValues copies values emitted by a FilterPopup into this filter.
func (f *FilterModel) ApplyPopupValues(values map[string]string) {
	if s, ok := values["search"]; ok {
		f.search.Input.SetValue(s)
		f.search.MarkDirty()
	}
	if v, ok := values["hostname"]; ok {
		f.hostname = v
	}
	if v, ok := values["program"]; ok {
		f.program = v
	}
	if v, ok := values["severity_max"]; ok {
		if v == "" {
			f.severityMax = -1
		} else if n, err := strconv.Atoi(v); err == nil {
			f.severityMax = n
		}
	}
	if v, ok := values["facility"]; ok {
		if v == "" {
			f.facility = -1
		} else if n, err := strconv.Atoi(v); err == nil {
			f.facility = n
		}
	}
}

// PopupFields builds the list of popup fields with current values and
// metadata-driven suggestions, ready for component.NewFilterPopup.
func (f *FilterModel) PopupFields() []component.Field {
	sevVal := ""
	if f.severityMax >= 0 {
		sevVal = strconv.Itoa(f.severityMax)
	}
	facVal := ""
	if f.facility >= 0 {
		facVal = strconv.Itoa(f.facility)
	}
	return []component.Field{
		{
			Key:         "hostname",
			Label:       "Hostname",
			Kind:        component.FieldText,
			Suggestions: f.hosts,
			Value:       f.hostname,
		},
		{
			Key:         "program",
			Label:       "Program",
			Kind:        component.FieldText,
			Suggestions: f.programs,
			Value:       f.program,
		},
		{
			Key:     "severity_max",
			Label:   "Severity",
			Kind:    component.FieldDropdown,
			Options: severityOptions(),
			Value:   sevVal,
		},
		{
			Key:     "facility",
			Label:   "Facility",
			Kind:    component.FieldDropdown,
			Options: facilityOptions(),
			Value:   facVal,
		},
		{
			Key:   "search",
			Label: "Search",
			Kind:  component.FieldText,
			Value: f.search.Search(),
		},
	}
}

// severityOptions mirrors srvlog's severity set; duplicated intentionally so
// each package stays self-contained and labels can diverge.
func severityOptions() []component.Option {
	return []component.Option{
		{Value: "", Label: "any"},
		{Value: "0", Label: "EMERG (0)"},
		{Value: "1", Label: "ALERT (1)"},
		{Value: "2", Label: "CRIT (2)"},
		{Value: "3", Label: "ERR (3)"},
		{Value: "4", Label: "WARNING (4)"},
		{Value: "5", Label: "NOTICE (5)"},
		{Value: "6", Label: "INFO (6)"},
		{Value: "7", Label: "DEBUG (7)"},
	}
}

// facilityOptions mirrors the srvlog facility list.
func facilityOptions() []component.Option {
	names := []string{
		"kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news",
		"uucp", "cron", "authpriv", "ftp", "ntp", "security", "console", "clock",
		"local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7",
	}
	opts := make([]component.Option, 0, len(names)+1)
	opts = append(opts, component.Option{Value: "", Label: "any"})
	for i, n := range names {
		opts = append(opts, component.Option{
			Value: strconv.Itoa(i),
			Label: n + " (" + strconv.Itoa(i) + ")",
		})
	}
	return opts
}

var _ logview.Filter = (*FilterModel)(nil)
