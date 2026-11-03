package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"syscall"
	"time"
)

const pollInterval = 250 * time.Millisecond

// tailFile follows path from the current end-of-file and sends each new
// complete line through handler until ctx is cancelled. It detects log rotation
// (file replacement or truncation) and reopens automatically.
func tailFile(ctx context.Context, path string, handler slog.Handler, logger *slog.Logger) {
	f, err := os.Open(path)
	if err != nil {
		logger.Error("cannot open file", "path", path, "error", err)
		return
	}
	defer func() {
		f.Close() //nolint:errcheck // best-effort close on exit
	}()

	// Seek to end — only ship lines appended after startup.
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		logger.Error("seek to end failed", "path", path, "error", err)
		return
	}

	reader := newLineReader(f, logger.With("path", path))
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	inode := fileInode(f)

	for {
		line, readErr := reader.next()
		if readErr == nil {
			if line == "" {
				continue
			}
			record := parseLine(line)
			if err := handler.Handle(ctx, record); err != nil {
				logger.Warn("handle log entry failed", "path", path, "error", err)
			}
			continue
		}

		if !errors.Is(readErr, io.EOF) {
			logger.Warn("read error", "path", path, "error", readErr)
			return
		}

		// EOF — the reader keeps any partial line until more data arrives.

		// Check for file replacement or truncation.
		if newF, newInode, rotated := checkRotation(path, f, inode, logger); rotated {
			// Ship any buffered partial line from the old file.
			if partial := reader.take(); partial != "" {
				record := parseLine(partial)
				if err := handler.Handle(ctx, record); err != nil {
					logger.Warn("handle log entry failed", "path", path, "error", err)
				}
			}
			if newF != nil {
				f.Close() //nolint:errcheck // closing rotated-away file
				f = newF
			}
			reader.reset(f)
			inode = newInode
			continue
		}

		// Wait for new data or shutdown.
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// checkRotation detects whether the file at path has been replaced (different
// inode) or truncated (same inode, smaller size than current read position).
//
// On replacement it returns the newly opened file. On truncation it seeks the
// existing fd to the start and returns nil. In both cases rotated is true so
// the caller knows to reset the bufio.Reader.
func checkRotation(path string, f *os.File, origInode uint64, logger *slog.Logger) (newF *os.File, newInode uint64, rotated bool) {
	pathInfo, err := os.Stat(path)
	if err != nil {
		return nil, 0, false
	}

	pathInode := statInode(pathInfo)

	// File replaced: new file at the same path (logrotate rename+create).
	if pathInode != 0 && pathInode != origInode {
		logger.Info("file replaced (new inode), reopening", "path", path)
		nf, err := os.Open(path)
		if err != nil {
			logger.Warn("reopen rotated file failed", "path", path, "error", err)
			return nil, 0, false
		}
		return nf, pathInode, true
	}

	// File truncated: same inode but current position is past the end.
	pos, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, 0, false
	}
	if pathInfo.Size() < pos {
		logger.Info("file truncated, seeking to beginning", "path", path)
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			logger.Warn("seek to start failed after truncation", "path", path, "error", err)
		}
		return nil, origInode, true
	}

	return nil, 0, false
}

// fileInode returns the inode number of the open file, or 0 if unavailable.
func fileInode(f *os.File) uint64 {
	info, err := f.Stat()
	if err != nil {
		return 0
	}
	return statInode(info)
}

// statInode extracts the inode number from an os.FileInfo.
func statInode(info os.FileInfo) uint64 {
	if sys, ok := info.Sys().(*syscall.Stat_t); ok {
		return sys.Ino
	}
	return 0
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
