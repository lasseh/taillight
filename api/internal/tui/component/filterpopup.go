package component

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/lasseh/taillight/internal/tui/theme"
)

// FieldKind distinguishes free-form text fields (with optional typeahead
// suggestions) from dropdowns over a fixed option set.
type FieldKind int

const (
	// FieldText is a free-form string field (hostname, program, service, ...).
	FieldText FieldKind = iota
	// FieldDropdown picks from a fixed list of options (severity, level, ...).
	FieldDropdown
)

// Option is a single entry in a FieldDropdown. Value is the API value written
// to Values(); Label is the human-readable display string.
type Option struct {
	Value string
	Label string
}

// Field describes one row in the filter popup.
type Field struct {
	Key         string // machine key: "hostname", "severity_max", ...
	Label       string // display label: "Hostname"
	Kind        FieldKind
	Options     []Option // FieldDropdown only
	Suggestions []string // FieldText only — typeahead pool
	Value       string   // current value (Option.Value for dropdowns)
}

// FilterPopupAppliedMsg is emitted when the user applies filters. The parent
// dispatches this back to the active view so it can rebuild its filter and
// restart the SSE stream.
type FilterPopupAppliedMsg struct {
	Values map[string]string
}

// FilterPopup is a floating filter editor rendered on top of the main view.
// It is a standalone bubbletea-style model: Update returns a new FilterPopup
// plus optional tea.Cmd. Use OverlayFilterPopup to composite View() onto the
// base screen.
type FilterPopup struct {
	fields      []Field
	selected    int
	editing     bool // text-input editing or dropdown inline-list mode
	input       textinput.Model
	optionIndex int // highlighted option when editing a dropdown
	width       int // screen width (for centering)
	height      int // screen height
}

// filterPopupKeys enumerates the bindings the popup recognizes. Defined once
// and reused across Update so the matches don't allocate per-keypress.
var filterPopupKeys = struct {
	up, down, next, prev        key.Binding
	enter, clear, apply, cancel key.Binding
	optionUp, optionDown        key.Binding
}{
	up:         key.NewBinding(key.WithKeys("up", "k")),
	down:       key.NewBinding(key.WithKeys("down", "j")),
	next:       key.NewBinding(key.WithKeys("tab")),
	prev:       key.NewBinding(key.WithKeys("shift+tab")),
	enter:      key.NewBinding(key.WithKeys("enter")),
	clear:      key.NewBinding(key.WithKeys("c")),
	apply:      key.NewBinding(key.WithKeys("ctrl+s")),
	cancel:     key.NewBinding(key.WithKeys("esc")),
	optionUp:   key.NewBinding(key.WithKeys("up", "k", "shift+tab")),
	optionDown: key.NewBinding(key.WithKeys("down", "j", "tab")),
}

// NewFilterPopup builds a popup over the given fields. Field order is
// preserved; the first field starts selected.
func NewFilterPopup(fields []Field) FilterPopup {
	ti := textinput.New()
	ti.Prompt = "› "
	ti.SetWidth(32)
	return FilterPopup{
		fields: fields,
		input:  ti,
	}
}

// SetSize records the screen dimensions so OverlayFilterPopup can center the
// popup. Safe to call on every WindowSizeMsg.
func (p *FilterPopup) SetSize(w, h int) {
	p.width = w
	p.height = h
}

// Values returns the popup's current values keyed by Field.Key. Empty strings
// are preserved so callers can distinguish cleared-vs-untouched fields.
func (p FilterPopup) Values() map[string]string {
	out := make(map[string]string, len(p.fields))
	for _, f := range p.fields {
		out[f.Key] = f.Value
	}
	return out
}

// Editing reports whether a field is currently in edit mode. Callers use this
// to decide whether keys like Esc should be absorbed by the popup (cancel
// edit) or by the parent app (close popup).
func (p FilterPopup) Editing() bool {
	return p.editing
}

// Update handles a single message and returns the new popup state. Keys that
// don't apply (e.g. tab navigation while editing a text input) fall through
// to the textinput so typing works normally.
func (p FilterPopup) Update(msg tea.Msg) (FilterPopup, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return p, nil
	}

	// Text-input editing mode: every key except enter/esc goes to the input.
	if p.editing && p.fields[p.selected].Kind == FieldText {
		switch {
		case key.Matches(keyMsg, filterPopupKeys.enter):
			p.fields[p.selected].Value = strings.TrimSpace(p.input.Value())
			p.editing = false
			p.input.Blur()
			return p, nil
		case key.Matches(keyMsg, filterPopupKeys.cancel):
			// Abort the in-flight edit; keep the previous value.
			p.editing = false
			p.input.Blur()
			return p, nil
		}
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		return p, cmd
	}

	// Dropdown inline-list mode: arrows cycle options, enter commits.
	if p.editing && p.fields[p.selected].Kind == FieldDropdown {
		switch {
		case key.Matches(keyMsg, filterPopupKeys.enter):
			if p.optionIndex >= 0 && p.optionIndex < len(p.fields[p.selected].Options) {
				p.fields[p.selected].Value = p.fields[p.selected].Options[p.optionIndex].Value
			}
			p.editing = false
			return p, nil
		case key.Matches(keyMsg, filterPopupKeys.cancel):
			p.editing = false
			return p, nil
		case key.Matches(keyMsg, filterPopupKeys.optionUp):
			if p.optionIndex > 0 {
				p.optionIndex--
			}
			return p, nil
		case key.Matches(keyMsg, filterPopupKeys.optionDown):
			if p.optionIndex < len(p.fields[p.selected].Options)-1 {
				p.optionIndex++
			}
			return p, nil
		}
		return p, nil
	}

	// Navigation mode: move selection, enter/edit, clear, apply.
	switch {
	case key.Matches(keyMsg, filterPopupKeys.up), key.Matches(keyMsg, filterPopupKeys.prev):
		if p.selected > 0 {
			p.selected--
		}
	case key.Matches(keyMsg, filterPopupKeys.down), key.Matches(keyMsg, filterPopupKeys.next):
		if p.selected < len(p.fields)-1 {
			p.selected++
		}
	case key.Matches(keyMsg, filterPopupKeys.clear):
		p.fields[p.selected].Value = ""
	case key.Matches(keyMsg, filterPopupKeys.apply):
		return p, func() tea.Msg { return FilterPopupAppliedMsg{Values: p.Values()} }
	case key.Matches(keyMsg, filterPopupKeys.enter):
		return p.enterEdit()
	}
	return p, nil
}

// enterEdit opens the selected field for editing. For text fields the
// textinput is seeded with the current value and focused; for dropdowns the
// inline list is primed at the current selection.
func (p FilterPopup) enterEdit() (FilterPopup, tea.Cmd) {
	field := p.fields[p.selected]
	switch field.Kind {
	case FieldText:
		p.input.SetValue(field.Value)
		cmd := p.input.Focus()
		p.editing = true
		return p, cmd
	case FieldDropdown:
		p.optionIndex = 0
		for i, opt := range field.Options {
			if opt.Value == field.Value {
				p.optionIndex = i
				break
			}
		}
		p.editing = true
		return p, nil
	}
	return p, nil
}

// View renders the popup card (no centering — caller composites it). Returns
// an empty string if there are no fields.
func (p FilterPopup) View() string {
	if len(p.fields) == 0 {
		return ""
	}

	const cardWidth = 48
	bg := theme.ColorBGDark

	header := lipgloss.NewStyle().
		Foreground(theme.ColorTeal).
		Background(bg).
		Bold(true).
		Render("Filters")

	var rows []string
	rows = append(rows, header, "")

	for i, f := range p.fields {
		rows = append(rows, p.renderField(i, f, cardWidth-4))
	}

	footer := lipgloss.NewStyle().
		Foreground(theme.ColorComment).
		Background(bg).
		Render("↑/↓ field   enter select   c clear   ^s apply   esc close")
	rows = append(rows, "", footer)

	body := lipgloss.NewStyle().
		Background(bg).
		Width(cardWidth - 4).
		Render(strings.Join(rows, "\n"))

	return theme.Card.Width(cardWidth).Render(body)
}

// renderField renders a single row (label + value, plus inline editor when
// the row is in edit mode).
func (p FilterPopup) renderField(i int, f Field, width int) string {
	bg := theme.ColorBGDark
	rowBG := bg
	if i == p.selected {
		rowBG = theme.ColorBGHighlight
	}

	labelStyle := lipgloss.NewStyle().
		Foreground(theme.ColorComment).
		Background(rowBG).
		Width(12)
	valueStyle := lipgloss.NewStyle().
		Foreground(theme.ColorFG).
		Background(rowBG)

	// Value display — dropdowns show the current option's label.
	display := f.Value
	if f.Kind == FieldDropdown {
		display = ""
		for _, opt := range f.Options {
			if opt.Value == f.Value {
				display = opt.Label
				break
			}
		}
	}
	if display == "" {
		display = lipgloss.NewStyle().
			Foreground(theme.ColorComment).
			Background(rowBG).
			Italic(true).
			Render("<any>")
	} else {
		display = valueStyle.Render(display)
	}

	row := labelStyle.Render(f.Label+":") + " " + display
	rowLine := lipgloss.NewStyle().
		Background(rowBG).
		Width(width).
		Render(row)

	// Inline editor beneath the selected row when editing.
	if i != p.selected || !p.editing {
		return rowLine
	}

	switch f.Kind {
	case FieldText:
		editor := lipgloss.NewStyle().
			Foreground(theme.ColorFG).
			Background(bg).
			Render(p.input.View())
		// Typeahead: show up to 5 matching suggestions.
		hint := p.suggestions(f, p.input.Value(), 5, width)
		if hint != "" {
			return rowLine + "\n" + editor + "\n" + hint
		}
		return rowLine + "\n" + editor
	case FieldDropdown:
		return rowLine + "\n" + p.renderOptionList(f, width)
	}
	return rowLine
}

// suggestions renders up to max matching Suggestions for a text field, filtered
// by case-insensitive substring against the current input.
func (p FilterPopup) suggestions(f Field, input string, maxHits, width int) string {
	if len(f.Suggestions) == 0 {
		return ""
	}
	bg := theme.ColorBGDark
	needle := strings.ToLower(strings.TrimSpace(input))
	var hits []string
	for _, s := range f.Suggestions {
		if needle == "" || strings.Contains(strings.ToLower(s), needle) {
			hits = append(hits, s)
		}
		if len(hits) >= maxHits {
			break
		}
	}
	if len(hits) == 0 {
		return ""
	}
	style := lipgloss.NewStyle().
		Foreground(theme.ColorComment).
		Background(bg).
		Width(width)
	return style.Render("  " + strings.Join(hits, "  "))
}

// renderOptionList renders the inline dropdown list with the current option
// highlighted. Length is bounded to avoid overwhelming the popup.
func (p FilterPopup) renderOptionList(f Field, width int) string {
	const visible = 9
	if len(f.Options) == 0 {
		return ""
	}

	// Sliding window so the current option stays visible.
	start := max(0, p.optionIndex-visible/2)
	end := min(len(f.Options), start+visible)
	if end-start < visible {
		start = max(0, end-visible)
	}

	bg := theme.ColorBGDark
	var lines []string
	for i := start; i < end; i++ {
		opt := f.Options[i]
		label := opt.Label
		if label == "" {
			label = opt.Value
		}
		style := lipgloss.NewStyle().
			Foreground(theme.ColorFG).
			Background(bg).
			Width(width)
		if i == p.optionIndex {
			style = style.
				Foreground(theme.ColorTeal).
				Background(theme.ColorBGHighlight).
				Bold(true)
		}
		lines = append(lines, style.Render(" "+label))
	}
	return strings.Join(lines, "\n")
}

// OverlayFilterPopup composites the rendered popup onto the screen, centered
// over the content area. screenW/screenH must match the base screen size so
// the centering math is correct.
func OverlayFilterPopup(screen, popup string, screenW, screenH int) string {
	if popup == "" {
		return screen
	}
	pw := lipgloss.Width(popup)
	ph := lipgloss.Height(popup)
	x := max(0, (screenW-pw)/2)
	y := max(0, (screenH-ph)/2)
	base := lipgloss.NewLayer(screen)
	overlay := lipgloss.NewLayer(popup).X(x).Y(y).Z(2)
	return lipgloss.NewCompositor(base, overlay).Render()
}
