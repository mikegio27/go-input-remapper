package cmd

import (
	"fmt"
	"os"

	"github.com/mikegio27/nereus/internal/config"
	"github.com/mikegio27/nereus/internal/control"
	"github.com/mikegio27/nereus/internal/paths"
	"github.com/spf13/cobra"
)

// dialDaemon connects to the control socket, mapping a connection failure to a
// friendly hint that the daemon may not be running.
func dialDaemon() (*control.Client, error) {
	c, err := control.Dial(paths.ClientSocketPath(flagSocket))
	if err != nil {
		return nil, fmt.Errorf("%w (is the daemon running?)", err)
	}
	return c, nil
}

var reloadCmd = &cobra.Command{
	Use:   "reload",
	Short: "Tell the running daemon to re-read its config now",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := dialDaemon()
		if err != nil {
			return err
		}
		defer c.Close()

		var res control.ReloadResult
		if err := c.Call(control.MethodReload, nil, &res); err != nil {
			return err
		}
		if !res.OK {
			fmt.Fprintln(os.Stderr, "reload rejected; daemon kept its current config:")
			for _, e := range res.Errors {
				fmt.Fprintf(os.Stderr, "  - %s\n", e)
			}
			return fmt.Errorf("config invalid")
		}
		fmt.Println("config reloaded")
		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the daemon's active profile and per-device engine state",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := dialDaemon()
		if err != nil {
			return err
		}
		defer c.Close()

		var res control.StatusResult
		if err := c.Call(control.MethodStatus, nil, &res); err != nil {
			return err
		}
		profile := res.ActiveProfile
		if profile == "" {
			profile = "(none)"
		}
		fmt.Printf("active profile: %s\n", profile)
		fmt.Printf("bound devices:  %d\n", len(res.Engines))
		for _, e := range res.Engines {
			fmt.Printf("  - %s  ->  %s\n", e.Path, e.Name)
		}
		return nil
	},
}

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the config files without applying them",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := paths.ConfigDir(flagConfigDir)
		cfg, err := config.Load(dir)
		if err != nil {
			return err
		}
		problems := config.Validate(cfg)
		if len(problems) == 0 {
			fmt.Printf("config in %s is valid (%d profile(s))\n", dir, len(cfg.Profiles))
			return nil
		}
		fmt.Fprintf(os.Stderr, "%d problem(s) in %s:\n", len(problems), dir)
		for _, p := range problems {
			fmt.Fprintf(os.Stderr, "  - %s\n", p)
		}
		return fmt.Errorf("config validation failed")
	},
}

func init() {
	rootCmd.AddCommand(reloadCmd, statusCmd, validateCmd)
}
