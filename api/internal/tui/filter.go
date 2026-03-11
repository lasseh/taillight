package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// FilterBar is an interactive text input that parses key:value filter pairs.
type FilterBar struct {
	input   textinput.Model
	active  bool
	stream  Stream
	applied map[string]string // currently applied filter params
}

// NewFilterBar creates a new filter bar.
func NewFilterBar() FilterBar {
	ti := textinput.New()
	ti.Placeholder = "hostname:web01 search:error"
	ti.CharLimit = 256
	return FilterBar{
		input:   ti,
		applied: make(map[string]string),
	}
}

// FilterAppliedMsg is sent when the user submits a filter.
type FilterAppliedMsg struct {
	Stream Stream
	Params map[string]string
}

// FilterClearedMsg is sent when the user clears/closes the filter.
type FilterClearedMsg struct {
	Stream Stream
}

// IsActive returns whether the filter bar is focused.
func (f FilterBar) IsActive() bool {
	return f.active
}

// Open activates the filter bar for the given stream.
func (f *FilterBar) Open(stream Stream) tea.Cmd {
	f.active = true
	f.stream = stream
	f.input.Focus()
	return textinput.Blink
}

// Close deactivates the filter bar.
func (f *FilterBar) Close() {
	f.active = false
	f.input.Blur()
}

// Update handles messages for the filter bar.
func (f FilterBar) Update(msg tea.Msg) (FilterBar, tea.Cmd) {
	if !f.active {
		return f, nil
	}

	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case keyEnter:
			params := parseFilter(f.input.Value(), f.stream)
			f.applied = params
			f.Close()
			return f, func() tea.Msg {
				return FilterAppliedMsg{Stream: f.stream, Params: params}
			}
		case "esc":
			f.input.SetValue("")
			f.applied = make(map[string]string)
			f.Close()
			return f, func() tea.Msg {
				return FilterClearedMsg{Stream: f.stream}
			}
		}
	}

	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	return f, cmd
}

// View renders the filter bar.
func (f FilterBar) View(width int) string {
	if !f.active {
		if len(f.applied) > 0 {
			return filterBarStyle.Width(width).Render("filter: " + formatFilterTags(f.applied))
		}
		return ""
	}
	return filterBarStyle.Width(width).Render("filter: " + f.input.View())
}

// HasFilter returns whether a filter is currently applied.
func (f FilterBar) HasFilter() bool {
	return len(f.applied) > 0
}

// syslog filter keys.
var syslogFilterKeys = map[string]string{
	"hostname":    "hostname",
	"programname": "programname",
	"syslogtag":   "syslogtag",
	"severity":    "severity",
	"facility":    "facility",
	"search":      "search",
	"host":        "hostname",
	"program":     "programname",
	"tag":         "syslogtag",
	"sev":         "severity",
}

// applog filter keys.
var applogFilterKeys = map[string]string{
	"service":   "service",
	"component": "component",
	"host":      "host",
	"level":     "level",
	"search":    "search",
	"svc":       "service",
	"comp":      "component",
	"lvl":       "level",
}

// parseFilter converts "key:value key2:value2 bare text" into URL query params.
func parseFilter(input string, stream Stream) map[string]string {
	params := make(map[string]string)
	keys := syslogFilterKeys
	if stream == StreamAppLog {
		keys = applogFilterKeys
	}

	var searchParts []string
	for token := range strings.FieldsSeq(input) {
		if idx := strings.IndexByte(token, ':'); idx > 0 {
			key := strings.ToLower(token[:idx])
			value := token[idx+1:]
			if paramKey, ok := keys[key]; ok {
				params[paramKey] = value
				continue
			}
		}
		searchParts = append(searchParts, token)
	}

	if len(searchParts) > 0 {
		params["search"] = strings.Join(searchParts, " ")
	}

	return params
}

// formatFilterTags renders applied filters as colored tag badges.
func formatFilterTags(params map[string]string) string {
	parts := make([]string, 0, len(params))
	for k, v := range params {
		tag := filterTagKeyStyle.Render(k+":") + filterTagValueStyle.Render(v)
		parts = append(parts, tag)
	}
	return strings.Join(parts, "  ")
}
