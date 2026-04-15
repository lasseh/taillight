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
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
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

	// Resolve host: config value takes priority, fall back to os.Hostname().
	host := cfg.Host
	if host == "" {
		host, _ = os.Hostname()
	}

	piped := isStdinPiped()
	hasFiles := len(cfg.Files) > 0

	if !piped && !hasFiles {
		return fmt.Errorf("no input source — pipe stdin or configure files")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// handlers tracks every Handler so we can shut them all down.
	var handlers []*logshipper.Handler

	var httpClient *http.Client
	if cfg.TLSSkipVerify {
		httpClient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // user-requested skip
			},
		}
		logger.Warn("TLS certificate verification disabled")
	}

	minLevel := parseMinLevel(cfg.MinLevel)

	newHandler := func(service, component, hostOverride string) *logshipper.Handler {
		h, err := logshipper.New(logshipper.Config{
			Endpoint:    cfg.Endpoint,
			APIKey:      logshipper.Secret(cfg.APIKey),
			Service:     service,
			Component:   component,
			Host:        hostOverride,
			MinLevel:    minLevel,
			BatchSize:   cfg.BatchSize,
			FlushPeriod: flushPeriod,
			BufferSize:  cfg.BufferSize,
			Client:      httpClient,
		})
		if err != nil {
			logger.Error("logshipper init failed", "error", err)
			os.Exit(1)
		}
		handlers = append(handlers, h)
		return h
	}

	var wg sync.WaitGroup

	// Stdin reader.
	if piped {
		stdinHandler := newHandler(cfg.Service, cfg.Component, host)
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
	for _, fc := range cfg.Files {
		h := newHandler(fc.resolvedService(cfg.Service), fc.resolvedComponent(cfg.Component), fc.resolvedHost(host))
		logger.Info("tailing file", "path", fc.Path, "service", fc.resolvedService(cfg.Service))
		wg.Go(func() {
			tailFile(ctx, fc.Path, h, logger)
		})
	}

	wg.Wait()
	shutdown(handlers, logger)
	return nil
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

// shutdown flushes all handlers and reports any dropped entries.
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
	}
}
