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
	"charm.land/wish/v2/logging"
	gossh "golang.org/x/crypto/ssh"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/ssh"
	"github.com/lasseh/taillight/internal/tui"
	"github.com/lasseh/taillight/internal/tui/client"
	"github.com/lasseh/taillight/pkg/logshipper"
	"github.com/spf13/cobra"
)

var (
	listenAddr      string
	hostKeyPath     string
	authorizedKeys  string
	allowAnyKey     bool
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
	rootCmd.Flags().BoolVar(&allowAnyKey, "allow-any-key", false, "DEMO ONLY: accept any SSH public key without an authorized_keys file")
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

	// Configure the auth option: either an authorized_keys file (the
	// production default) or allow-any-key for public demo servers.
	authOption, err := buildAuthOption(logger)
	if err != nil {
		return err
	}

	// Create the SSH server. We use a custom session middleware (instead
	// of bubbletea.MiddlewareWithProgramHandler) so we can defer
	// app.Cleanup() per session. Wish's built-in handler doesn't expose
	// a hook for after-program teardown, which would leak SSE goroutines
	// every time a client disconnects.
	//
	// Password auth is intentionally not enabled in either mode.
	srv, err := wish.NewServer(
		wish.WithAddress(listenAddr),
		wish.WithHostKeyPath(hostKeyPath),
		authOption,
		wish.WithMiddleware(
			sessionMiddleware(serverURL, apiKey, fps, logger),
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

// buildAuthOption returns the wish ssh.Option for public-key auth, either
// from an authorized_keys file (production) or an accept-any-key handler
// (public demo servers). Password auth is intentionally never enabled.
func buildAuthOption(logger *slog.Logger) (ssh.Option, error) {
	if allowAnyKey {
		// Loud warning — this must never go unnoticed in production logs.
		logger.Warn("DEMO MODE: accepting any SSH public key — do not use in production")
		fmt.Fprintln(os.Stderr, "┌─────────────────────────────────────────────────────────────┐")
		fmt.Fprintln(os.Stderr, "│  WARNING: --allow-any-key is set — this server accepts      │")
		fmt.Fprintln(os.Stderr, "│  ANY SSH public key. Do NOT use this mode in production.    │")
		fmt.Fprintln(os.Stderr, "└─────────────────────────────────────────────────────────────┘")
		return wish.WithPublicKeyAuth(func(_ ssh.Context, _ ssh.PublicKey) bool {
			return true
		}), nil
	}

	// Production mode: require an authorized_keys file. Fail fast if it's
	// missing — never silently accept all connections.
	if _, err := os.Stat(authorizedKeys); err != nil {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Public-key auth is required. To set it up:")
		fmt.Fprintln(os.Stderr, "  1. On the client machine, copy your public key:")
		fmt.Fprintln(os.Stderr, "     cat ~/.ssh/id_ed25519.pub")
		fmt.Fprintf(os.Stderr, "  2. On this server, paste it into %s (one key per line)\n", authorizedKeys)
		fmt.Fprintln(os.Stderr, "  3. Restart taillight-wish")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "For a public demo server, use --allow-any-key instead (INSECURE).")
		return nil, fmt.Errorf("authorized_keys not found at %q: %w", authorizedKeys, err)
	}
	logger.Info("loaded authorized_keys", "path", authorizedKeys)
	return wish.WithAuthorizedKeys(authorizedKeys), nil
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
	shipper, err := logshipper.New(logshipper.Config{
		Endpoint:  serverURL + "/api/v1/applog/ingest",
		APIKey:    logshipper.Secret(apiKey),
		Service:   "taillight-wish",
		Component: "ssh-server",
		Host:      host,
		AddSource: true,
		MinLevel:  slog.LevelInfo,
	})
	if err != nil {
		slog.New(consoleHandler).Error("logshipper init failed", "error", err)
		return slog.New(consoleHandler), nil
	}
	return slog.New(logshipper.MultiHandler(consoleHandler, shipper)), shipper
}

// sessionMiddleware is a wish.Middleware that creates and runs a tea.Program
// per SSH session with full control over the lifecycle. We own the entire
// session here (instead of using bubbletea.MiddlewareWithProgramHandler)
// so we can defer app.Cleanup() — wish's built-in handler doesn't expose
// any after-program hook, which would leak SSE goroutines per session.
func sessionMiddleware(srvURL, key string, fpsRate int, logger *slog.Logger) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(s ssh.Session) {
			pty, windowChanges, active := s.Pty()
			if !active {
				fmt.Fprintln(s, "Error: no PTY requested. Use: ssh -t ...") //nolint:errcheck // best-effort
				logger.Warn("session rejected: no PTY",
					"user", s.User(),
					"remote_addr", s.RemoteAddr().String())
				return
			}

			started := time.Now()
			fingerprint := ""
			if pk := s.PublicKey(); pk != nil {
				fingerprint = gossh.FingerprintSHA256(pk)
			}
			logger.Info("session started",
				"user", s.User(),
				"remote_addr", s.RemoteAddr().String(),
				"key_fingerprint", fingerprint,
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
			// Critical: release SSE goroutines and TCP connections when
			// the session ends. Without this, every SSH disconnect leaks
			// the stream goroutines and connections to the API.
			defer app.Cleanup()

			// Build environment with COLORTERM=truecolor so bubbletea and
			// lipgloss detect TrueColor support over SSH.
			envs := append(s.Environ(), "TERM="+pty.Term, "COLORTERM=truecolor")

			// Determine I/O: use pty.Slave when available (real PTY),
			// fall back to the session itself.
			var input io.Reader = s
			var output io.Writer = s
			if !s.EmulatedPty() && pty.Slave != nil {
				input = pty.Slave
				output = pty.Slave
			}

			program := tea.NewProgram(app,
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

			// Forward window resize events to the program.
			ctx, cancel := context.WithCancel(s.Context())
			defer cancel()
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					case w := <-windowChanges:
						program.Send(tea.WindowSizeMsg{Width: w.Width, Height: w.Height})
					}
				}
			}()

			if _, err := program.Run(); err != nil {
				logger.Error("program exit error",
					"err", err,
					"user", s.User(),
					"remote_addr", s.RemoteAddr().String())
			}

			logger.Info("session ended",
				"user", s.User(),
				"remote_addr", s.RemoteAddr().String(),
				"duration", time.Since(started).Round(time.Second).String())

			next(s)
		}
	}
}
