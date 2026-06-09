// Package main is the entrypoint for the taillight CLI.
package main

import (
	"os"

	// Embed the IANA timezone database into the binary so time.LoadLocation
	// works on minimal base images (alpine/scratch/distroless) that don't ship
	// /usr/share/zoneinfo. Without this, any non-UTC schedule timezone fails
	// validation with "invalid timezone".
	_ "time/tzdata"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
