package component_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/lasseh/taillight/internal/tui/component"
)

const seedHostname = "router"

func sampleFields() []component.Field {
	return []component.Field{
		{Key: "hostname", Label: "Hostname", Kind: component.FieldText, Value: seedHostname},
		{Key: "severity_max", Label: "Severity", Kind: component.FieldDropdown, Options: []component.Option{
			{Value: "", Label: "any"},
			{Value: "3", Label: "ERR"},
			{Value: "4", Label: "WARN"},
		}, Value: "3"},
	}
}

func TestNewFilterPopupValuesPreservesSeed(t *testing.T) {
	p := component.NewFilterPopup(sampleFields())
	got := p.Values()
	if got["hostname"] != seedHostname {
		t.Errorf("hostname = %q, want %q", got["hostname"], seedHostname)
	}
	if got["severity_max"] != "3" {
		t.Errorf("severity_max = %q, want %q", got["severity_max"], "3")
	}
}

func TestFilterPopupNavigationDown(t *testing.T) {
	p := component.NewFilterPopup(sampleFields())
	// Move selection down.
	p, _ = p.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	// Clear current field (severity_max).
	p, _ = p.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if got := p.Values()["severity_max"]; got != "" {
		t.Errorf("severity_max after clear = %q, want empty", got)
	}
}

func TestFilterPopupApplyEmitsMsg(t *testing.T) {
	p := component.NewFilterPopup(sampleFields())
	_, cmd := p.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+s should emit an apply command")
	}
	msg := cmd()
	applied, ok := msg.(component.FilterPopupAppliedMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want FilterPopupAppliedMsg", msg)
	}
	if applied.Values["hostname"] != seedHostname {
		t.Errorf("apply values[hostname] = %q, want %q", applied.Values["hostname"], seedHostname)
	}
}

func TestFilterPopupEscCancelsEdit(t *testing.T) {
	p := component.NewFilterPopup(sampleFields())
	// Enter text-edit mode on hostname.
	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// Type a letter — should be routed to the textinput now.
	p, _ = p.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	// Esc should abandon the edit without committing.
	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if got := p.Values()["hostname"]; got != seedHostname {
		t.Errorf("hostname after cancelled edit = %q, want %q", got, seedHostname)
	}
}

func TestFilterPopupDropdownCycleAndCommit(t *testing.T) {
	p := component.NewFilterPopup(sampleFields())
	// Move to severity field.
	p, _ = p.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	// Open inline list.
	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// Move cursor down in the option list.
	p, _ = p.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	// Commit.
	p, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// Seed was "3" (index 1); j moves to index 2 → "4".
	if got := p.Values()["severity_max"]; got != "4" {
		t.Errorf("severity_max after dropdown commit = %q, want %q", got, "4")
	}
}

func TestFilterPopupViewRenders(t *testing.T) {
	p := component.NewFilterPopup(sampleFields())
	p.SetSize(120, 40)
	view := p.View()
	if view == "" {
		t.Error("popup view should not be empty for non-empty fields")
	}
}

func TestFilterPopupEmptyFields(t *testing.T) {
	p := component.NewFilterPopup(nil)
	if view := p.View(); view != "" {
		t.Errorf("empty popup should render empty, got %q", view)
	}
}

func TestOverlayFilterPopupNoPopupReturnsScreen(t *testing.T) {
	screen := "hello"
	out := component.OverlayFilterPopup(screen, "", 10, 10)
	if out != screen {
		t.Errorf("OverlayFilterPopup with empty popup = %q, want %q", out, screen)
	}
}
