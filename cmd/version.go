package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// version is the build version. It is "dev" for a plain `go build`/`go install`
// and is overridden at release time via the linker:
//
//	go build -ldflags "-X github.com/mikegio27/go-input-remapper/cmd.version=v1.2.3"
//
// (GoReleaser sets this automatically — see .goreleaser.yaml.)
var version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version and build info",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("go-input-remapper %s (%s/%s, %s)\n",
			version, runtime.GOOS, runtime.GOARCH, runtime.Version())
		return nil
	},
}

func init() {
	// Also exposes a top-level --version flag via Cobra.
	rootCmd.Version = version
	rootCmd.AddCommand(versionCmd)
}
