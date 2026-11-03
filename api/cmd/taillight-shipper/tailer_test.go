package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestTailFile_OversizedLineIsTruncatedAndShippingContinues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() }) //nolint:errcheck // test teardown
	write := func(s string) {
		t.Helper()
		if _, err := f.WriteString(s); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	var h recordingHandler
	done := make(chan struct{})
	go func() {
		defer close(done)
		tailFile(ctx, path, &h, slog.New(slog.DiscardHandler))
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	// The tailer starts at the end of the file; repeat a marker line until
	// one ships, so the lines that follow are read.
	waitFor := func(msg string) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); !slices.Contains(h.messages(), msg); time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("%q never shipped; got %d lines", msg, len(h.messages()))
			}
		}
	}
	for deadline := time.Now().Add(5 * time.Second); !slices.Contains(h.messages(), "ready"); {
		if time.Now().After(deadline) {
			t.Fatal("tailer never shipped a line")
		}
		write("ready\n")
		time.Sleep(2 * pollInterval)
	}

	// A 2 MiB line without its newline sits across polls, then completes.
	write(strings.Repeat("x", 2<<20))
	time.Sleep(2 * pollInterval)
	write("\nnext\n")
	waitFor("next")

	var got []string
	for _, m := range h.messages() {
		if m != "ready" {
			got = append(got, m)
		}
	}
	if len(got) != 2 || got[0] != strings.Repeat("x", maxLineBytes) || got[1] != "next" {
		t.Errorf("shipped lines of %v bytes, want the long line cut to %d bytes, then next", lineLens(got), maxLineBytes)
	}
}
