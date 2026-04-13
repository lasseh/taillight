package srvlog

import (
	"net/url"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/lasseh/taillight/internal/tui/theme"
)

// FilterModel manages the filter bar state.
type FilterModel struct {
	searchInput textinput.Model
	hosts       []string
	programs    []string
	hostIdx     int  // -1 = all
	progIdx     int  // -1 = all
	sevMax      int  // -1 = all, 0-7
	dirty       bool // true when filter changed, needs reapply
}

// newFilter creates the filter bar with defaults.
func newFilter() FilterModel {
	ti := textinput.New()
	ti.Placeholder = "search..."
	ti.SetWidth(30)
	ti.Prompt = "/ "

	return FilterModel{
		searchInput: ti,
		hostIdx:     -1,
		progIdx:     -1,
		sevMax:      -1,
	}
}

// Focus activates the search text input.
func (f *FilterModel) Focus() tea.Cmd {
	return f.searchInput.Focus()
}

// Blur deactivates the search text input.
func (f *FilterModel) Blur() {
	f.searchInput.Blur()
}

// Update handles input when the filter bar is focused. Only sets dirty
// when the search value actually changed — avoids rebuilding the table
// on arrow keys, Home/End, Ctrl+A, etc.
func (f *FilterModel) Update(msg tea.Msg) (FilterModel, tea.Cmd) {
	before := f.searchInput.Value()
	var cmd tea.Cmd
	f.searchInput, cmd = f.searchInput.Update(msg)
	if f.searchInput.Value() != before {
		f.dirty = true
	}
	return *f, cmd
}

// Dirty reports whether the filter changed since last ack.
func (f *FilterModel) Dirty() bool {
	return f.dirty
}

// AckDirty clears the dirty flag.
func (f *FilterModel) AckDirty() {
	f.dirty = false
}

// HasActiveFilter reports whether any filter narrows the result set.
func (f *FilterModel) HasActiveFilter() bool {
	return f.searchInput.Value() != "" ||
		f.Hostname() != "" || f.Programname() != "" || f.sevMax >= 0
}

// Search returns the current search text.
func (f *FilterModel) Search() string {
	return f.searchInput.Value()
}

// Hostname returns the selected hostname filter, or "" for all.
func (f *FilterModel) Hostname() string {
	if f.hostIdx < 0 || f.hostIdx >= len(f.hosts) {
		return ""
	}
	return f.hosts[f.hostIdx]
}

// Programname returns the selected program filter, or "" for all.
func (f *FilterModel) Programname() string {
	if f.progIdx < 0 || f.progIdx >= len(f.programs) {
		return ""
	}
	return f.programs[f.progIdx]
}

// SeverityMax returns the severity filter, or -1 for all.
func (f *FilterModel) SeverityMax() int {
	return f.sevMax
}

// SetMeta updates the available hosts and programs for the pickers.
func (f *FilterModel) SetMeta(hosts, programs []string) {
	f.hosts = hosts
	f.programs = programs
}

// Params returns the current filter as URL query parameters for the SSE stream.
func (f *FilterModel) Params() url.Values {
	v := url.Values{}
	if h := f.Hostname(); h != "" {
		v.Set("hostname", h)
	}
	if p := f.Programname(); p != "" {
		v.Set("programname", p)
	}
	if f.sevMax >= 0 {
		v.Set("severity_max", url.QueryEscape(string(rune('0'+f.sevMax))))
	}
	if s := f.Search(); s != "" {
		v.Set("search", s)
	}
	return v
}

// Matches reports whether an event passes the current filter.
func (f *FilterModel) Matches(e eventLike) bool {
	if h := f.Hostname(); h != "" && e.hostname() != h {
		return false
	}
	if p := f.Programname(); p != "" && e.programname() != p {
		return false
	}
	if f.sevMax >= 0 && e.severity() > f.sevMax {
		return false
	}
	// Search is not filtered client-side — it's sent to the SSE stream query.
	return true
}

// eventLike is a minimal interface for filter matching.
type eventLike interface {
	hostname() string
	programname() string
	severity() int
}

// View renders the filter bar.
func (f *FilterModel) View(width int) string {
	search := f.searchInput.View()

	var hostLabel string
	if h := f.Hostname(); h != "" {
		hostLabel = theme.FilterLabel.Render("host:") + theme.Hostname.Render(h)
	}

	var progLabel string
	if p := f.Programname(); p != "" {
		progLabel = theme.FilterLabel.Render("prog:") + theme.Program.Render(p)
	}

	parts := []string{search}
	if hostLabel != "" {
		parts = append(parts, hostLabel)
	}
	if progLabel != "" {
		parts = append(parts, progLabel)
	}

	bar := lipgloss.JoinHorizontal(lipgloss.Center, joinWithSep(parts, "  ")...)
	return theme.FilterBar.Width(width).Render(bar)
}

func joinWithSep(parts []string, sep string) []string {
	if len(parts) <= 1 {
		return parts
	}
	var out []string
	for i, p := range parts {
		out = append(out, p)
		if i < len(parts)-1 {
			out = append(out, sep)
		}
	}
	return out
}
