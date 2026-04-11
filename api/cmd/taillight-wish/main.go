// Command taillight-wish serves the Taillight TUI over SSH using charm's wish
// framework. Each SSH session gets its own bubbletea program connected to the
// Taillight API server.
//
// Usage:
//
//	taillight-wish -s https://taillight.example.com -k tl_xxxxx
//	taillight-wish --listen :2222 --host-key ~/.ssh/id_ed25519
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/wish/v2"
	"charm.land/wish/v2/activeterm"
	"charm.land/wish/v2/bubbletea"
	"charm.land/wish/v2/logging"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/ssh"
	"github.com/spf13/cobra"

	"github.com/lasseh/taillight/internal/tui"
	"github.com/lasseh/taillight/internal/tui/client"
)

var (
	listenAddr  string
	hostKeyPath string
	serverURL   string
	apiKey      string
)

var rootCmd = &cobra.Command{
	Use:          "taillight-wish",
	Short:        "Serve Taillight TUI over SSH",
	SilenceUsage: true,
	RunE:         run,
}

func init() {
	rootCmd.Flags().StringVar(&listenAddr, "listen", ":2222", "SSH listen address")
	rootCmd.Flags().StringVar(&hostKeyPath, "host-key", ".ssh/id_ed25519", "path to SSH host key")
	rootCmd.Flags().StringVarP(&serverURL, "server", "s", "", "Taillight API server URL (required)")
	rootCmd.Flags().StringVarP(&apiKey, "key", "k", "", "API key for the Taillight API")
	cobra.CheckErr(rootCmd.MarkFlagRequired("server"))
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(_ *cobra.Command, _ []string) error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Validate API connectivity before starting the SSH server.
	c := client.New(client.Config{
		BaseURL: serverURL,
		APIKey:  apiKey,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Health(ctx); err != nil {
		return fmt.Errorf("cannot reach Taillight API at %s: %w", serverURL, err)
	}
	logger.Info("taillight API reachable", "url", serverURL)

	// Create the SSH server.
	srv, err := wish.NewServer(
		wish.WithAddress(listenAddr),
		wish.WithHostKeyPath(hostKeyPath),
		wish.WithMiddleware(
			bubbletea.Middleware(newTeaHandler(serverURL, apiKey)),
			activeterm.Middleware(),
			logging.Middleware(),
		),
	)
	if err != nil {
		return fmt.Errorf("create SSH server: %w", err)
	}

	// Start listening.
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	logger.Info("starting SSH server", "addr", listenAddr)
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			logger.Error("SSH server error", "err", err)
		}
	}()

	<-done
	logger.Info("shutting down SSH server")

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	return srv.Shutdown(shutCtx)
}

// newTeaHandler returns a wish bubbletea handler that creates a fresh App
// instance for each SSH session.
func newTeaHandler(srvURL, key string) func(ssh.Session) (tea.Model, []tea.ProgramOption) {
	return func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
		_, _, active := s.Pty()
		if !active {
			fmt.Fprintln(s, "Error: no PTY requested. Use: ssh -t ...") //nolint:errcheck // best-effort message to SSH client
			return nil, nil
		}

		// Each session gets its own API client and App instance.
		c := client.New(client.Config{
			BaseURL: srvURL,
			APIKey:  key,
		})

		cfg := tui.Config{
			BufferSize:    10000,
			BatchInterval: 50 * time.Millisecond,
			AutoScroll:    true,
			TimeFormat:    "15:04:05",
		}

		app := tui.NewApp(cfg, c)

		return app, []tea.ProgramOption{
			tea.WithFPS(30),
			// Force TrueColor — SSH doesn't forward $COLORTERM so
			// lipgloss would downgrade our hex colors to 256-color.
			// All modern terminals support TrueColor even when they
			// report xterm-256color.
			tea.WithColorProfile(colorprofile.TrueColor),
		}
	}
}
