package main

import (
	"bufio"
	"errors"
	"io"
	"log/slog"
	"strings"
)

// maxLineBytes caps one input line at the 1 MiB the stdin reader has always
// accepted. The rest of a longer line is discarded up to its newline, so a
// line without a newline cannot grow memory past this bound, and the line
// still ships; pkg/logshipper then cuts the message to the ingest API's limit.
const maxLineBytes = 1 << 20

// lineReader reads newline-terminated lines for both stdin and file tailing,
// keeping at most maxLineBytes of each and logging every line it truncates.
// An incomplete line at the end of the input stays buffered across reads, so
// a tailed file can complete it later.
type lineReader struct {
	r         *bufio.Reader
	logger    *slog.Logger
	line      []byte // the current line so far, at most maxLineBytes
	cut       bool   // bytes of the current line were discarded
	truncated int    // lines returned truncated
}

func newLineReader(r io.Reader, logger *slog.Logger) *lineReader {
	return &lineReader{r: bufio.NewReaderSize(r, 64<<10), logger: logger}
}

// next returns the next complete line without its line ending. When the
// input runs out first it returns the reader's error, io.EOF included, and
// keeps the incomplete line for a later call or for take.
func (lr *lineReader) next() (string, error) {
	for {
		chunk, err := lr.r.ReadSlice('\n')
		if err == nil {
			chunk = chunk[:len(chunk)-1]
		}
		if room := maxLineBytes - len(lr.line); len(chunk) > room {
			chunk, lr.cut = chunk[:room], true
		}
		lr.line = append(lr.line, chunk...)
		if err == nil {
			return lr.take(), nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return "", err
		}
	}
}

// take returns the line buffered so far, without a trailing carriage return,
// and clears it.
func (lr *lineReader) take() string {
	line := strings.TrimSuffix(string(lr.line), "\r")
	if lr.cut {
		lr.truncated++
		lr.logger.Warn("line over max_bytes truncated", "max_bytes", maxLineBytes, "truncated_lines", lr.truncated)
	}
	lr.line, lr.cut = lr.line[:0], false
	return line
}

// reset switches to reading from r, as after log rotation.
func (lr *lineReader) reset(r io.Reader) {
	lr.r.Reset(r)
}
