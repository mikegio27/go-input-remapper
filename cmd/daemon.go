package cmd

import (
	"os/signal"
	"syscall"

	"github.com/mikegio27/nereus/internal/daemon"
	"github.com/mikegio27/nereus/internal/paths"
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run the remapper daemon (reads config, grabs devices, injects events)",
	Long: "The daemon is the persistent worker: it loads the TOML config, resolves\n" +
		"device matchers to real devices, grabs them, and re-emits remapped events\n" +
		"through virtual devices. It hot-reloads config and serves the control socket.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		return daemon.Run(ctx, daemon.Options{
			ConfigDir:  paths.ConfigDir(flagConfigDir),
			SocketPath: paths.SocketPath(flagSocket),
		})
	},
}

func init() {
	rootCmd.AddCommand(daemonCmd)
}
