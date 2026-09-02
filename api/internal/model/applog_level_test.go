package model

import (
	"slices"
	"testing"
)

func TestNormalizeLevel(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"ERROR", "ERROR", true},
		{"error", "ERROR", true},
		{"Warn", "WARN", true},
		// Aliases.
		{"trace", "DEBUG", true},
		{"d", "DEBUG", true},
		{"notice", "INFO", true},
		{"i", "INFO", true},
		{"warning", "WARN", true},
		{"w", "WARN", true},
		{"err", "ERROR", true},
		{"e", "ERROR", true},
		{"critical", "FATAL", true},
		{"crit", "FATAL", true},
		{"emerg", "FATAL", true},
		{"alert", "FATAL", true},
		{"severe", "FATAL", true},
		{"panic", "FATAL", true},
		// Unknown.
		{"verbose", "", false},
		{"", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := NormalizeLevel(tc.in)
			if got != tc.want || ok != tc.ok {
				t.Errorf("NormalizeLevel(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestAppLogLevelAliasNames(t *testing.T) {
	names := AppLogLevelAliasNames()
	if !slices.IsSorted(names) {
		t.Errorf("alias names not sorted: %v", names)
	}
	if len(names) != len(levelAliases) {
		t.Errorf("got %d names, want %d", len(names), len(levelAliases))
	}
	for _, name := range names {
		if _, ok := ValidAppLogLevels[name]; ok {
			t.Errorf("alias %q shadows a canonical level", name)
		}
	}
}
