package cmd

import (
	"github.com/mikegio27/go-input-remapper/internal/paths"
	"github.com/mikegio27/go-input-remapper/internal/tui"
	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Open the interactive TUI (default command)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return tui.Run(tui.Options{
			ConfigDir:  paths.ConfigDir(flagConfigDir),
			SocketPath: paths.SocketPath(flagSocket),
		})
	},
}

func init() {
	rootCmd.AddCommand(tuiCmd)
	// Running the bare binary opens the TUI.
	rootCmd.RunE = tuiCmd.RunE
}
