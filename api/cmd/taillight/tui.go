package main

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/spf13/cobra"

	"github.com/lasseh/taillight/internal/tui"
)

var (
	tuiURL    string
	tuiAPIKey string
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch the terminal UI for real-time log viewing",
	Long:  `Connect to a Taillight server and view syslog and applog events in a terminal interface.`,
	RunE:  runTUI,
}

func init() {
	tuiCmd.Flags().StringVar(&tuiURL, "url", "http://localhost:8080", "Taillight server URL")
	tuiCmd.Flags().StringVar(&tuiAPIKey, "api-key", "", "API key for authentication")
}

func runTUI(_ *cobra.Command, _ []string) error {
	client := tui.NewSSEClient(tuiURL, tuiAPIKey)
	model := tui.New(client)

	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}
