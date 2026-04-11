// Package netlog implements the netlog stream view for the TUI.
// Netlog events have the same structure as srvlog events.
package netlog

import (
	"net/url"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/lasseh/taillight/internal/tui/theme"
)

// FilterModel manages the netlog filter bar state.
type FilterModel struct {
	searchInput textinput.Model
	hosts       []string
	programs    []string
	hostIdx     int  // -1 = all
	progIdx     int  // -1 = all
	sevMax      int  // -1 = all, 0-7
	dirty       bool // true when filter changed
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

// Update handles input when the filter bar is focused.
func (f *FilterModel) Update(msg tea.Msg) (FilterModel, tea.Cmd) {
	var cmd tea.Cmd
	f.searchInput, cmd = f.searchInput.Update(msg)
	f.dirty = true
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

// SetMeta updates the available hosts and programs.
func (f *FilterModel) SetMeta(hosts, programs []string) {
	f.hosts = hosts
	f.programs = programs
}

// Params returns the current filter as URL query parameters.
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
	if s := f.searchInput.Value(); s != "" {
		v.Set("search", s)
	}
	return v
}

// Matches reports whether a netlog event passes the current filter.
func (f *FilterModel) Matches(hostname, programname string, severity int) bool {
	if h := f.Hostname(); h != "" && hostname != h {
		return false
	}
	if p := f.Programname(); p != "" && programname != p {
		return false
	}
	if f.sevMax >= 0 && severity > f.sevMax {
		return false
	}
	return true
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

	bar := lipgloss.JoinHorizontal(lipgloss.Center, joinWithSep(parts, " | ")...)
	return theme.FilterInput.Width(width).Render(bar)
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
