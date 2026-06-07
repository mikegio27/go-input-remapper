// Command go-input-remapper is a Linux input remapper: a persistent daemon that
// executes remaps and macros from TOML config files, plus a TUI front-end that
// edits those files and drives the daemon over a control socket.
package main

import (
	"os"

	"github.com/mikegio27/go-input-remapper/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
