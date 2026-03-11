package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/spf13/cobra"

	"github.com/lasseh/taillight/internal/config"
)

var (
	migrationsPath string
	migrateSteps   int
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Run database migrations",
	Long: `Run database migrations using golang-migrate.

Examples:
  taillight migrate up              # Apply all pending migrations
  taillight migrate down            # Roll back all migrations
  taillight migrate down --steps 1  # Roll back one migration
  taillight migrate version         # Show current migration version
  taillight migrate force 1         # Force set version (use with caution)`,
}

var migrateUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Apply all pending migrations",
	RunE:  runMigrateUp,
}

var migrateDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Roll back migrations",
	RunE:  runMigrateDown,
}

var migrateVersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show current migration version",
	RunE:  runMigrateVersion,
}

var migrateForceCmd = &cobra.Command{
	Use:   "force [version]",
	Short: "Force set migration version (use with caution)",
	Args:  cobra.ExactArgs(1),
	RunE:  runMigrateForce,
}

func init() {
	migrateCmd.PersistentFlags().StringVar(&migrationsPath, "path", "migrations", "path to migrations directory")
	migrateDownCmd.Flags().IntVar(&migrateSteps, "steps", 0, "number of migrations to roll back (0 = all)")

	migrateCmd.AddCommand(migrateUpCmd)
	migrateCmd.AddCommand(migrateDownCmd)
	migrateCmd.AddCommand(migrateVersionCmd)
	migrateCmd.AddCommand(migrateForceCmd)
}

func newMigrate() (*migrate.Migrate, error) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	sourcePath := fmt.Sprintf("file://%s", migrationsPath)
	m, err := migrate.New(sourcePath, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("create migrate instance: %w", err)
	}

	return m, nil
}

func runMigrateUp(_ *cobra.Command, _ []string) error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	m, err := newMigrate()
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }()

	logger.Info("applying migrations", "path", migrationsPath)

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			logger.Info("no migrations to apply")
			return nil
		}
		return fmt.Errorf("migrate up: %w", err)
	}

	version, dirty, _ := m.Version()
	logger.Info("migrations applied", "version", version, "dirty", dirty)
	return nil
}

func runMigrateDown(_ *cobra.Command, _ []string) error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	m, err := newMigrate()
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }()

	if migrateSteps > 0 {
		logger.Info("rolling back migrations", "steps", migrateSteps)
		if err := m.Steps(-migrateSteps); err != nil {
			if errors.Is(err, migrate.ErrNoChange) {
				logger.Info("no migrations to roll back")
				return nil
			}
			return fmt.Errorf("migrate down: %w", err)
		}
	} else {
		logger.Info("rolling back all migrations")
		if err := m.Down(); err != nil {
			if errors.Is(err, migrate.ErrNoChange) {
				logger.Info("no migrations to roll back")
				return nil
			}
			return fmt.Errorf("migrate down: %w", err)
		}
	}

	version, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return fmt.Errorf("get version: %w", err)
	}
	logger.Info("rollback complete", "version", version, "dirty", dirty)
	return nil
}

func runMigrateVersion(_ *cobra.Command, _ []string) error {
	m, err := newMigrate()
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }()

	version, dirty, err := m.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			fmt.Println("No migrations applied yet")
			return nil
		}
		return fmt.Errorf("get version: %w", err)
	}

	fmt.Printf("Version: %d\n", version)
	if dirty {
		fmt.Println("Status: dirty (migration failed, manual intervention required)")
	} else {
		fmt.Println("Status: clean")
	}
	return nil
}

func runMigrateForce(_ *cobra.Command, args []string) error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	var version int
	if _, err := fmt.Sscanf(args[0], "%d", &version); err != nil {
		return fmt.Errorf("invalid version: %s", args[0])
	}

	m, err := newMigrate()
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }()

	logger.Warn("forcing migration version", "version", version)
	if err := m.Force(version); err != nil {
		return fmt.Errorf("force version: %w", err)
	}

	logger.Info("version forced", "version", version)
	return nil
}
