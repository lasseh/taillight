package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is set at build time via ldflags.
var Version = "dev"

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
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(loadgenCmd)
	rootCmd.AddCommand(applogLoadgenCmd)
	rootCmd.AddCommand(importCmd)
	rootCmd.AddCommand(useraddCmd)
	rootCmd.AddCommand(apikeyCmd)
}
