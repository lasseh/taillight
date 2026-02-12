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
	apikeyUsername string
	apikeyName     string
	apikeyScopes   []string
)

var apikeyCmd = &cobra.Command{
	Use:   "apikey",
	Short: "Create a new API key for a user",
	Long:  `Generate a database-backed API key and print the full key to stdout (shown once).`,
	RunE:  runApikey,
}

func init() {
	apikeyCmd.Flags().StringVar(&apikeyUsername, "username", "", "username to create the key for (required)")
	apikeyCmd.Flags().StringVar(&apikeyName, "name", "", "descriptive name for the key (required)")
	apikeyCmd.Flags().StringSliceVar(&apikeyScopes, "scopes", []string{"admin"}, "comma-separated scopes: ingest, read, admin")
	_ = apikeyCmd.MarkFlagRequired("username")
	_ = apikeyCmd.MarkFlagRequired("name")
}

func runApikey(_ *cobra.Command, _ []string) error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
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

	store := postgres.NewAuthStore(pool)

	user, err := store.GetUserByUsername(ctx, apikeyUsername)
	if err != nil {
		return fmt.Errorf("lookup user %q: %w", apikeyUsername, err)
	}

	fullKey, keyHash, keyPrefix, err := auth.GenerateAPIKey()
	if err != nil {
		return fmt.Errorf("generate api key: %w", err)
	}

	_, err = store.CreateAPIKey(ctx, user.ID.Bytes, apikeyName, keyHash, keyPrefix, apikeyScopes, nil)
	if err != nil {
		return fmt.Errorf("create api key: %w", err)
	}

	logger.Info("api key created", "username", user.Username, "name", apikeyName, "prefix", keyPrefix)
	fmt.Println(fullKey)
	return nil
}
