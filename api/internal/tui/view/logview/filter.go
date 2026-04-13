package logview

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// SearchFilter is a reusable filter that wraps a single search text input
// with dirty tracking. Per-feed filter types embed this and add their own
// metadata fields (hostnames, services, etc.).
//
// Server-side filtering does the heavy lifting via SSE query params, so the
// search input is the only interactive filter.
type SearchFilter struct {
	Input textinput.Model
	dirty bool
}

// NewSearchFilter creates a new search filter with the standard placeholder
// and prompt.
func NewSearchFilter() SearchFilter {
	ti := textinput.New()
	ti.Placeholder = "search..."
	ti.SetWidth(30)
	ti.Prompt = "/ "
	return SearchFilter{Input: ti}
}

// UpdateInput delegates to the embedded textinput. Sets the dirty flag only
// when the value actually changed (so arrow keys, Home/End, etc. don't
// trigger wasted table rebuilds).
func (f *SearchFilter) UpdateInput(msg tea.Msg) tea.Cmd {
	before := f.Input.Value()
	var cmd tea.Cmd
	f.Input, cmd = f.Input.Update(msg)
	if f.Input.Value() != before {
		f.dirty = true
	}
	return cmd
}

// Focus activates the input.
func (f *SearchFilter) Focus() tea.Cmd {
	return f.Input.Focus()
}

// Blur deactivates the input.
func (f *SearchFilter) Blur() {
	f.Input.Blur()
}

// Dirty reports whether the search value changed since the last AckDirty.
func (f *SearchFilter) Dirty() bool {
	return f.dirty
}

// AckDirty clears the dirty flag.
func (f *SearchFilter) AckDirty() {
	f.dirty = false
}

// Search returns the current search query.
func (f *SearchFilter) Search() string {
	return f.Input.Value()
}
