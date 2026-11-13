package main

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestLineReader(t *testing.T) {
	long := strings.Repeat("x", maxLineBytes)
	tests := []struct {
		name          string
		in            string
		want          []string // lines returned before io.EOF
		wantRest      string   // incomplete line left at io.EOF
		wantTruncated int
	}{
		{name: "lines", in: "a\nb\n", want: []string{"a", "b"}},
		{name: "empty lines", in: "\n\n", want: []string{"", ""}},
		{name: "carriage return dropped", in: "a\r\nb\r\n", want: []string{"a", "b"}},
		{name: "incomplete last line kept", in: "a\nb", want: []string{"a"}, wantRest: "b"},
		{name: "line at the bound", in: long + "\n", want: []string{long}},
		{name: "line one byte over", in: long + "y\nnext\n", want: []string{long, "next"}, wantTruncated: 1},
		{name: "2 MiB line then a normal one", in: long + long + "\nnext\n", want: []string{long, "next"}, wantTruncated: 1},
		{name: "incomplete line over the bound", in: long + long, wantRest: long, wantTruncated: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lr := newLineReader(strings.NewReader(tt.in), slog.New(slog.DiscardHandler))
			var got []string
			for {
				line, err := lr.next()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatalf("next: %v", err)
				}
				got = append(got, line)
				if lr.lastCut != (len(line) == len(long) && tt.wantTruncated > 0) {
					t.Errorf("line of %d bytes: lastCut = %v", len(line), lr.lastCut)
				}
			}
			if rest := lr.take(); rest != tt.wantRest {
				t.Errorf("rest = %d bytes, want %d", len(rest), len(tt.wantRest))
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %d lines %v, want %d lines %v", len(got), lineLens(got), len(tt.want), lineLens(tt.want))
			}
			if lr.truncated != tt.wantTruncated {
				t.Errorf("truncated = %d, want %d", lr.truncated, tt.wantTruncated)
			}
		})
	}
}

// A tailed file can end mid-line. The reader keeps the partial line across
// io.EOF and completes it once the rest arrives, still within the bound.
func TestLineReader_LineCompletedAfterEOF(t *testing.T) {
	var file bytes.Buffer // returns io.EOF when drained, like a file at its end
	lr := newLineReader(&file, slog.New(slog.DiscardHandler))

	file.WriteString("first ha")
	if _, err := lr.next(); !errors.Is(err, io.EOF) {
		t.Fatalf("next = %v, want io.EOF before the newline", err)
	}
	file.WriteString("lf\n")
	if line, err := lr.next(); err != nil || line != "first half" {
		t.Fatalf("next = %q, %v, want %q", line, err, "first half")
	}

	file.WriteString(strings.Repeat("x", maxLineBytes-1))
	if _, err := lr.next(); !errors.Is(err, io.EOF) {
		t.Fatalf("next = %v, want io.EOF before the newline", err)
	}
	file.WriteString("yz\n")
	line, err := lr.next()
	if err != nil || line != strings.Repeat("x", maxLineBytes-1)+"y" {
		t.Fatalf("next = %d bytes, %v, want %d bytes", len(line), err, maxLineBytes)
	}
	if lr.truncated != 1 {
		t.Errorf("truncated = %d, want 1", lr.truncated)
	}
}

// growingFile serves size bytes with no newline, returning io.EOF after every
// step bytes as a tailed file does between polls.
type growingFile struct {
	size, step, sent int
	pause            bool
}

func (g *growingFile) Read(p []byte) (int, error) {
	if g.pause || g.sent == g.size {
		g.pause = false
		return 0, io.EOF
	}
	n := min(len(p), g.step-g.sent%g.step, g.size-g.sent)
	for i := range n {
		p[i] = 'x'
	}
	g.sent += n
	g.pause = g.sent%g.step == 0
	return n, nil
}

func TestLineReader_LineWithoutNewlineKeepsHeapNearBound(t *testing.T) {
	src := &growingFile{size: 32 << 20, step: 1 << 20}
	lr := newLineReader(src, slog.New(slog.DiscardHandler))

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for src.sent < src.size {
		if _, err := lr.next(); !errors.Is(err, io.EOF) {
			t.Fatalf("next = %v, want io.EOF while the line has no newline", err)
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&after)

	if len(lr.line) != maxLineBytes {
		t.Errorf("buffered %d bytes, want %d", len(lr.line), maxLineBytes)
	}
	if grew := int64(after.HeapAlloc) - int64(before.HeapAlloc); grew > 2*maxLineBytes {
		t.Errorf("heap grew %d bytes over a %d-byte line without a newline, want at most %d", grew, src.size, 2*maxLineBytes)
	}
	runtime.KeepAlive(lr)
}

func lineLens(lines []string) []int {
	lens := make([]int, len(lines))
	for i, l := range lines {
		lens[i] = len(l)
	}
	return lens
}
