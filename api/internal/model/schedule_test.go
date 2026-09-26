package model

import (
	"testing"
	"time"
)

func TestSchedulePeriod(t *testing.T) {
	tests := []struct {
		frequency string
		want      time.Duration
	}{
		{"daily", 24 * time.Hour},
		{"weekly", 7 * 24 * time.Hour},
		{"monthly", 30 * 24 * time.Hour},
		{"bogus", 24 * time.Hour},
	}
	for _, tt := range tests {
		if got := SchedulePeriod(tt.frequency); got != tt.want {
			t.Errorf("SchedulePeriod(%q) = %v, want %v", tt.frequency, got, tt.want)
		}
	}
}

func TestParseTimeOfDay(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		hour    int
		minute  int
		wantErr bool
	}{
		{name: "midnight", in: "00:00", hour: 0, minute: 0},
		{name: "morning", in: "09:30", hour: 9, minute: 30},
		{name: "end of day", in: "23:59", hour: 23, minute: 59},
		{name: "empty", in: "", wantErr: true},
		{name: "garbage", in: "banana", wantErr: true},
		{name: "hour out of range", in: "25:00", wantErr: true},
		{name: "minute out of range", in: "12:61", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hour, minute, err := ParseTimeOfDay(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseTimeOfDay(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if err == nil && (hour != tt.hour || minute != tt.minute) {
				t.Errorf("ParseTimeOfDay(%q) = %d:%d, want %d:%d", tt.in, hour, minute, tt.hour, tt.minute)
			}
		})
	}
}
