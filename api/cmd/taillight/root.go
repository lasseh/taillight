package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is set at build time via ldflags.
var Version = "dev"

// cfgFile is the optional path to the config file, set via --config.
var cfgFile string

var rootCmd = &cobra.Command{
	Use:          "taillight",
	Short:        "Taillight — real-time syslog and application log viewer",
	SilenceUsage: true,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version",
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Println("taillight", Version)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "path to config file (default: config.yml in . or /etc/taillight)")

	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(loadgenCmd)
	rootCmd.AddCommand(applogLoadgenCmd)
	rootCmd.AddCommand(importCmd)
	rootCmd.AddCommand(useraddCmd)
	rootCmd.AddCommand(apikeyCmd)
}
