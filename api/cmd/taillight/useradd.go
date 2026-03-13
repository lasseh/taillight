package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/lasseh/taillight/internal/auth"
	"github.com/lasseh/taillight/internal/config"
	"github.com/lasseh/taillight/internal/postgres"
)

var (
	useraddUsername string
	useraddPassword string
	useraddAdmin    bool
)

var useraddCmd = &cobra.Command{
	Use:   "useradd",
	Short: "Create a new user account",
	Long:  `Create a new user account with a username and password.`,
	RunE:  runUseradd,
}

func init() {
	useraddCmd.Flags().StringVar(&useraddUsername, "username", "", "username for the new account (required)")
	useraddCmd.Flags().StringVar(&useraddPassword, "password", "", "password for the new account (required)")
	useraddCmd.Flags().BoolVar(&useraddAdmin, "admin", false, "grant admin privileges")
	_ = useraddCmd.MarkFlagRequired("username")
	_ = useraddCmd.MarkFlagRequired("password")
}

func runUseradd(_ *cobra.Command, _ []string) error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	if len(useraddPassword) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if len(useraddPassword) > 72 {
		return fmt.Errorf("password must be at most 72 characters (bcrypt limit)")
	}

	passwordHash, err := auth.HashPassword(useraddPassword)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	store := postgres.NewAuthStore(pool)
	user, err := store.CreateUser(ctx, useraddUsername, passwordHash, useraddAdmin)
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}

	logger.Info("user created", "username", user.Username, "id", fmt.Sprintf("%x", user.ID.Bytes))
	return nil
}
