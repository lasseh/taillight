// Package applog implements the applog stream view for the TUI.
package applog

import (
	"net/url"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/lasseh/taillight/internal/tui/theme"
)

// FilterModel manages the applog filter bar state.
type FilterModel struct {
	searchInput textinput.Model
	services    []string
	components  []string
	hosts       []string
	svcIdx      int  // -1 = all
	compIdx     int  // -1 = all
	hostIdx     int  // -1 = all
	levelIdx    int  // -1 = all, index into levelOptions
	dirty       bool // true when filter changed, needs reapply
}

var levelOptions = []string{"", "DEBUG", "INFO", "WARN", "ERROR", "FATAL"}

// newFilter creates the filter bar with defaults.
func newFilter() FilterModel {
	ti := textinput.New()
	ti.Placeholder = "search..."
	ti.SetWidth(30)
	ti.Prompt = "/ "

	return FilterModel{
		searchInput: ti,
		svcIdx:      -1,
		compIdx:     -1,
		hostIdx:     -1,
		levelIdx:    -1,
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

// Search returns the current search text.
func (f *FilterModel) Search() string {
	return f.searchInput.Value()
}

// Service returns the selected service filter, or "" for all.
func (f *FilterModel) Service() string {
	if f.svcIdx < 0 || f.svcIdx >= len(f.services) {
		return ""
	}
	return f.services[f.svcIdx]
}

// Host returns the selected host filter, or "" for all.
func (f *FilterModel) Host() string {
	if f.hostIdx < 0 || f.hostIdx >= len(f.hosts) {
		return ""
	}
	return f.hosts[f.hostIdx]
}

// Level returns the selected minimum level filter, or "" for all.
func (f *FilterModel) Level() string {
	if f.levelIdx < 0 || f.levelIdx >= len(levelOptions) {
		return ""
	}
	return levelOptions[f.levelIdx]
}

// SetMeta updates the available services, components, and hosts.
func (f *FilterModel) SetMeta(services, components, hosts []string) {
	f.services = services
	f.components = components
	f.hosts = hosts
}

// Params returns the current filter as URL query parameters.
func (f *FilterModel) Params() url.Values {
	v := url.Values{}
	if svc := f.Service(); svc != "" {
		v.Set("service", svc)
	}
	if h := f.Host(); h != "" {
		v.Set("host", h)
	}
	if lvl := f.Level(); lvl != "" {
		v.Set("level", lvl)
	}
	if s := f.Search(); s != "" {
		v.Set("search", s)
	}
	return v
}

// Matches reports whether an applog event passes the current filter.
func (f *FilterModel) Matches(e applogLike) bool {
	if svc := f.Service(); svc != "" && e.service() != svc {
		return false
	}
	if h := f.Host(); h != "" && e.host() != h {
		return false
	}
	// Level filtering is server-side via SSE params, not client-side.
	return true
}

// applogLike is a minimal interface for filter matching.
type applogLike interface {
	service() string
	host() string
	level() string
}

// View renders the filter bar.
func (f *FilterModel) View(width int) string {
	search := f.searchInput.View()

	var svcLabel string
	if svc := f.Service(); svc != "" {
		svcLabel = theme.FilterLabel.Render("svc:") + theme.Program.Render(svc)
	}

	var lvlLabel string
	if lvl := f.Level(); lvl != "" {
		lvlLabel = theme.FilterLabel.Render("level:") + theme.AppLogLevelStyle(lvl).Render(lvl)
	}

	parts := []string{search}
	if svcLabel != "" {
		parts = append(parts, svcLabel)
	}
	if lvlLabel != "" {
		parts = append(parts, lvlLabel)
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
