// Command taillight-shipper reads log lines from stdin and/or tails log files,
// shipping them to a taillight ingest endpoint using pkg/logshipper.
//
// Usage:
//
//	./log-producing-api | taillight-shipper -c config.yml
//	taillight-shipper -c config.yml           # file-follow mode only
//	./log-producing-api | taillight-shipper -c config.yml -t  # both + tee
package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/lasseh/taillight/pkg/logshipper"
)

var (
	configPath string
	tee        bool
)

var rootCmd = &cobra.Command{
	Use:          "taillight-shipper",
	Short:        "Ship log lines from stdin and/or log files to a taillight endpoint",
	SilenceUsage: true,
	RunE:         run,
}

func init() {
	rootCmd.Flags().StringVarP(&configPath, "config", "c", "", "path to config file")
	rootCmd.Flags().BoolVarP(&tee, "tee", "t", false, "tee mode: echo each stdin line to stdout")
	cobra.CheckErr(rootCmd.MarkFlagRequired("config"))
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(_ *cobra.Command, _ []string) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	flushPeriod, err := parseFlushPeriod(cfg.FlushPeriod)
	if err != nil {
		return fmt.Errorf("parse flush_period: %w", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	piped := isStdinPiped()
	hasFiles := len(cfg.Files) > 0

	if !piped && !hasFiles {
		return fmt.Errorf("no input source — pipe stdin or configure files")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if cfg.TLSSkipVerify {
		logger.Warn("TLS certificate verification disabled")
	}

	base := baseConfig(cfg, flushPeriod)

	// Create every handler before any reader starts, so a config the library
	// rejects (no resolvable host, empty service, over-long field) stops the
	// command before it reads stdin or opens a file. An empty host is
	// resolved by the library.
	var specs []handlerSpec
	if piped {
		specs = append(specs, handlerSpec{service: cfg.Service, component: cfg.Component, host: cfg.Host})
	}
	for _, fc := range cfg.Files {
		specs = append(specs, handlerSpec{
			service:   fc.resolvedService(cfg.Service),
			component: fc.resolvedComponent(cfg.Component),
			host:      fc.resolvedHost(cfg.Host),
		})
	}
	handlers, err := newHandlers(base, specs, logger)
	if err != nil {
		return err
	}
	fileHandlers := handlers

	var wg sync.WaitGroup

	// Stdin reader.
	if piped {
		stdinHandler := handlers[0]
		fileHandlers = handlers[1:]
		wg.Add(1)
		go func() {
			<-ctx.Done()
			_ = os.Stdin.Close()
		}()
		go func() {
			defer wg.Done()
			readStdin(ctx, stdinHandler, tee)
		}()
		logger.Info("reading from stdin", "service", cfg.Service)
	}

	// File tailers.
	for i, fc := range cfg.Files {
		h := fileHandlers[i]
		logger.Info("tailing file", "path", fc.Path, "service", fc.resolvedService(cfg.Service))
		wg.Go(func() {
			tailFile(ctx, fc.Path, h, logger)
		})
	}

	wg.Wait()
	shutdown(handlers, logger)
	return nil
}

// handlerSpec holds the per-handler fields: one spec for the stdin reader and
// one per tailed file.
type handlerSpec struct {
	service, component, host string
}

// newShipper is logshipper.New, replaced in tests to observe created handlers.
var newShipper = logshipper.New

// baseConfig returns the library config every handler shares.
func baseConfig(cfg config, flushPeriod time.Duration) logshipper.Config {
	return logshipper.Config{
		Endpoint:           cfg.Endpoint,
		APIKey:             logshipper.Secret(cfg.APIKey),
		MinLevel:           parseMinLevel(cfg.MinLevel),
		BatchSize:          cfg.BatchSize,
		FlushPeriod:        flushPeriod,
		BufferSize:         cfg.BufferSize,
		InsecureSkipVerify: cfg.TLSSkipVerify,
	}
}

// newHandlers creates one handler per spec, in order. If one fails, the
// handlers already created are shut down before the error is returned, so no
// handler is left running without a shutdown.
func newHandlers(base logshipper.Config, specs []handlerSpec, logger *slog.Logger) ([]*logshipper.Handler, error) {
	handlers := make([]*logshipper.Handler, 0, len(specs))
	for _, s := range specs {
		cfg := base
		cfg.Service, cfg.Component, cfg.Host = s.service, s.component, s.host
		h, err := newShipper(cfg)
		if err != nil {
			shutdown(handlers, logger)
			return nil, fmt.Errorf("logshipper init (service %q): %w", s.service, err)
		}
		handlers = append(handlers, h)
	}
	return handlers, nil
}

// readStdin scans lines from stdin until EOF or ctx cancellation.
func readStdin(ctx context.Context, handler *logshipper.Handler, tee bool) {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1 MB line buffer.

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if tee {
			fmt.Println(line)
		}

		record := parseLine(line)
		if err := handler.Handle(ctx, record); err != nil {
			fmt.Fprintf(os.Stderr, "error: handle log entry: %v\n", err)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "error: reading stdin: %v\n", err)
	}
}

// shutdown flushes all handlers and reports any entries that were lost.
func shutdown(handlers []*logshipper.Handler, logger *slog.Logger) {
	for _, h := range handlers {
		hCtx, hCancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := h.Shutdown(hCtx); err != nil {
			logger.Error("shutdown error", "error", err)
		}
		hCancel()
		if dropped := h.Dropped(); dropped > 0 {
			logger.Warn("dropped log entries", "count", dropped)
		}
		if failed := h.SendFailed(); failed > 0 {
			logger.Warn("failed to ship log entries", "count", failed)
		}
	}
}
