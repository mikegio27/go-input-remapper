// Package cmd wires the Cobra command tree for the single go-input-remapper
// binary. Subcommands share the global --config-dir and --socket flags so the
// daemon, TUI, and CLI all resolve the same locations.
package cmd

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

// Global flag values, populated by Cobra before any subcommand runs.
var (
	flagConfigDir string
	flagSocket    string
	flagVerbose   bool
)

// rootCmd is the base command. With no subcommand it launches the TUI (the
// expected interactive entry point); other subcommands are headless.
var rootCmd = &cobra.Command{
	Use:   "go-input-remapper",
	Short: "Remap inputs, define macros, and switch profiles on Linux",
	Long: "go-input-remapper is a Linux input remapper: a persistent daemon executes\n" +
		"remaps and macros from TOML config files, and a TUI front-end edits those\n" +
		"files and talks to the daemon. Run with no subcommand to open the TUI.",
	SilenceUsage:  true,
	SilenceErrors: true,
	// Default action (no subcommand) is the TUI; tui.go sets RunE.
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		setupLogging(flagVerbose)
	},
}

// Execute runs the command tree and returns the process exit code.
func Execute() int {
	if err := rootCmd.Execute(); err != nil {
		slog.Error("command failed", "err", err)
		return 1
	}
	return 0
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&flagConfigDir, "config-dir", "", "config directory (default: $XDG_CONFIG_HOME/go-input-remapper)")
	pf.StringVar(&flagSocket, "socket", "", "control socket path (default: $XDG_RUNTIME_DIR/go-input-remapper.sock)")
	pf.BoolVarP(&flagVerbose, "verbose", "v", false, "enable debug logging")
}

// setupLogging configures the default slog logger to write human-readable text
// to stderr, at debug level when verbose.
func setupLogging(verbose bool) {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h))
}
