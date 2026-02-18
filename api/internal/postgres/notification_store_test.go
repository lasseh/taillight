package postgres

import "testing"

func TestDeref(t *testing.T) {
	tests := []struct {
		name  string
		input *string
		want  string
	}{
		{name: "nil returns empty", input: nil, want: ""},
		{name: "non-nil returns value", input: strPtr("hello"), want: "hello"},
		{name: "empty string pointer", input: strPtr(""), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deref(tt.input)
			if got != tt.want {
				t.Errorf("deref() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNullIfEmpty(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantNil bool
	}{
		{name: "empty returns nil", input: "", wantNil: true},
		{name: "non-empty returns pointer", input: "value"},
		{name: "whitespace is non-empty", input: " "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nullIfEmpty(tt.input)
			if tt.wantNil {
				if got != nil {
					t.Errorf("nullIfEmpty(%q) = %v, want nil", tt.input, got)
				}
			} else {
				if got == nil {
					t.Fatalf("nullIfEmpty(%q) = nil, want non-nil", tt.input)
				}
				if *got != tt.input {
					t.Errorf("nullIfEmpty(%q) = %q, want %q", tt.input, *got, tt.input)
				}
			}
		})
	}
}

func strPtr(s string) *string { return &s }
