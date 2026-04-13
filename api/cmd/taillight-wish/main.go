// Command taillight-wish serves the Taillight TUI over SSH using charm's wish
// framework. Each SSH session gets its own bubbletea program connected to the
// Taillight API server.
//
// Authentication is public-key only via an authorized_keys file. Password
// auth is intentionally not enabled. The server fails to start if the
// authorized_keys file is missing — never silently accepts all connections.
//
// Logs are shipped to the Taillight API's applog ingest endpoint so SSH
// session lifecycle, auth events, and errors are queryable alongside other
// system logs. See the --logshipper-* flags.
//
// Usage:
//
//	taillight-wish -s https://taillight.example.com -k tl_xxxxx
//	taillight-wish --listen :2222 --authorized-keys ~/.ssh/taillight_authorized_keys
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
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
	"github.com/lasseh/taillight/pkg/logshipper"
)

var (
	listenAddr      string
	hostKeyPath     string
	authorizedKeys  string
	serverURL       string
	apiKey          string
	fps             int
	shipLogsEnabled bool
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
	rootCmd.Flags().StringVar(&authorizedKeys, "authorized-keys", defaultAuthorizedKeysPath(), "path to authorized_keys file (public-key auth)")
	rootCmd.Flags().StringVarP(&serverURL, "server", "s", "", "Taillight API server URL (required)")
	rootCmd.Flags().StringVarP(&apiKey, "key", "k", "", "API key for the Taillight API")
	rootCmd.Flags().IntVar(&fps, "fps", 30, "render frame rate per session (1-60)")
	rootCmd.Flags().BoolVar(&shipLogsEnabled, "logshipper-enabled", true, "ship wish logs to the Taillight applog ingest endpoint")
	cobra.CheckErr(rootCmd.MarkFlagRequired("server"))
}

// defaultAuthorizedKeysPath returns the default path for the authorized_keys
// file, falling back to a relative path if HOME is unset.
func defaultAuthorizedKeysPath() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".ssh", "taillight_authorized_keys")
	}
	return "taillight_authorized_keys"
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(_ *cobra.Command, _ []string) error {
	logger, shipper := setupLogger()
	defer func() {
		if shipper != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := shipper.Shutdown(ctx); err != nil {
				logger.Warn("logshipper shutdown error", "err", err)
			}
		}
	}()

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

	// Validate authorized_keys exists before starting the server. Fail
	// fast — never silently accept all connections.
	if _, err := os.Stat(authorizedKeys); err != nil {
		return fmt.Errorf("authorized_keys file %q: %w (create one with 'ssh-keygen -y -f ~/.ssh/id_ed25519 > %s')", authorizedKeys, err, authorizedKeys)
	}
	logger.Info("loaded authorized_keys", "path", authorizedKeys)

	// Create the SSH server. Use MiddlewareWithProgramHandler for full
	// control over tea.ProgramOption ordering — the default Middleware
	// appends MakeOptions which overrides our WithEnvironment.
	// Public-key auth via WithAuthorizedKeys; password auth is intentionally
	// not enabled.
	srv, err := wish.NewServer(
		wish.WithAddress(listenAddr),
		wish.WithHostKeyPath(hostKeyPath),
		wish.WithAuthorizedKeys(authorizedKeys),
		wish.WithMiddleware(
			bubbletea.MiddlewareWithProgramHandler(newProgramHandler(serverURL, apiKey, fps, logger)),
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

// setupLogger creates the wish logger, optionally shipping logs to the
// Taillight API's applog ingest endpoint. Mirrors the pattern used by the
// main API server in cmd/taillight/serve.go:setupLogger.
func setupLogger() (*slog.Logger, *logshipper.Handler) {
	consoleHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})

	if !shipLogsEnabled {
		return slog.New(consoleHandler), nil
	}

	host, _ := os.Hostname()
	shipper := logshipper.New(logshipper.Config{
		Endpoint:  serverURL + "/api/v1/applog/ingest",
		APIKey:    apiKey,
		Service:   "taillight-wish",
		Component: "ssh-server",
		Host:      host,
		AddSource: true,
		MinLevel:  slog.LevelInfo,
	})
	return slog.New(logshipper.MultiHandler(consoleHandler, shipper)), shipper
}

// newProgramHandler returns a wish ProgramHandler that creates a fresh
// tea.Program for each SSH session with full control over option ordering.
// This bypasses the default handler's MakeOptions which would override
// our environment and color profile settings.
func newProgramHandler(srvURL, key string, fpsRate int, logger *slog.Logger) bubbletea.ProgramHandler {
	return func(s ssh.Session) *tea.Program {
		pty, _, active := s.Pty()
		if !active {
			fmt.Fprintln(s, "Error: no PTY requested. Use: ssh -t ...") //nolint:errcheck // best-effort message to SSH client
			logger.Warn("session rejected: no PTY",
				"user", s.User(),
				"remote_addr", s.RemoteAddr().String())
			return nil
		}

		logger.Info("session started",
			"user", s.User(),
			"remote_addr", s.RemoteAddr().String(),
			"term", pty.Term,
			"window", fmt.Sprintf("%dx%d", pty.Window.Width, pty.Window.Height))

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

		// Build environment with COLORTERM=truecolor so both bubbletea
		// and lipgloss detect TrueColor support. SSH doesn't forward
		// $COLORTERM, causing 256-color downgrading without this.
		envs := append(s.Environ(), "TERM="+pty.Term, "COLORTERM=truecolor")

		// Determine I/O: use pty.Slave when available (real PTY),
		// fall back to the session itself (emulated PTY or no slave FD).
		var input io.Reader = s
		var output io.Writer = s
		if !s.EmulatedPty() && pty.Slave != nil {
			input = pty.Slave
			output = pty.Slave
		}

		return tea.NewProgram(app,
			tea.WithInput(input),
			tea.WithOutput(output),
			tea.WithEnvironment(envs),
			tea.WithColorProfile(colorprofile.TrueColor),
			tea.WithFPS(fpsRate),
			tea.WithWindowSize(pty.Window.Width, pty.Window.Height),
			// Suppress suspend (ctrl+z) — not meaningful over SSH.
			tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg {
				if _, ok := msg.(tea.SuspendMsg); ok {
					return tea.ResumeMsg{}
				}
				return msg
			}),
		)
	}
}
