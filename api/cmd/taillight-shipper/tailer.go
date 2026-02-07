package main

import (
	"context"
	"io"
	"log/slog"
	"os"

	"github.com/nxadm/tail"

	"github.com/lasseh/taillight/pkg/logshipper"
)

// tailFile tails path and sends each line through handler until ctx is
// cancelled. It handles log rotation via ReOpen and waits for the file to
// appear if it doesn't exist at startup.
func tailFile(ctx context.Context, path string, handler *logshipper.Handler, logger *slog.Logger) {
	t, err := tail.TailFile(path, tail.Config{
		Follow:    true,
		ReOpen:    true,
		MustExist: false,
		Location:  &tail.SeekInfo{Offset: 0, Whence: io.SeekEnd},
		Logger:    tail.DiscardingLogger,
	})
	if err != nil {
		logger.Error("failed to tail file", "path", path, "error", err)
		return
	}

	logger.Info("tailing file", "path", path)

	for {
		select {
		case <-ctx.Done():
			t.Cleanup()
			return
		case line, ok := <-t.Lines:
			if !ok {
				return
			}
			if line.Err != nil {
				logger.Warn("tail read error", "path", path, "error", line.Err)
				continue
			}
			record := parseLine(line.Text)
			if err := handler.Handle(context.Background(), record); err != nil {
				logger.Warn("handle log entry failed", "path", path, "error", err)
			}
		}
	}
}

// isStdinPiped returns true when stdin is connected to a pipe or file rather
// than a terminal.
func isStdinPiped() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice == 0
}
