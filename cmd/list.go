package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/mikegio27/nereus/internal/control"
	"github.com/mikegio27/nereus/internal/device"
	"github.com/spf13/cobra"
)

var flagListRecommend bool

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List input devices with classification and remap recommendations",
	Long: "Enumerate input devices, classify each (keyboard/mouse/gamepad/...), and\n" +
		"flag good remap candidates. If the daemon is running, its view is used so\n" +
		"bound devices are marked; otherwise devices are enumerated directly (and\n" +
		"unreadable ones are skipped — run with more privileges to see them all).",
	RunE: func(cmd *cobra.Command, args []string) error {
		devices, fromDaemon := gatherDevices()
		if len(devices) == 0 {
			fmt.Fprintln(os.Stderr, "no readable input devices (try running with more privileges, or start the daemon)")
			return nil
		}
		if flagListRecommend {
			printRecommendations(devices)
			return nil
		}
		printDeviceTable(devices, fromDaemon)
		return nil
	},
}

func init() {
	listCmd.Flags().BoolVarP(&flagListRecommend, "recommend", "r", false, "show remap recommendations and caveats")
	rootCmd.AddCommand(listCmd)
}

// gatherDevices returns the daemon's device view when it is reachable (so bound
// devices are marked), otherwise enumerates devices directly. The bool reports
// whether the daemon was the source.
func gatherDevices() ([]control.DeviceInfo, bool) {
	if c, err := dialDaemon(); err == nil {
		defer c.Close()
		var res control.ListDevicesResult
		if err := c.Call(control.MethodListDevices, nil, &res); err == nil {
			return res.Devices, true
		}
	}
	return enumerateDirect(), false
}

// enumerateDirect inspects devices without the daemon, mapping to the same shape.
func enumerateDirect() []control.DeviceInfo {
	infos, err := device.InspectAll(device.DefaultVirtualPrefix)
	if err != nil {
		return nil
	}
	recs := device.Recommend(infos)
	out := make([]control.DeviceInfo, 0, len(recs))
	for _, r := range recs {
		out = append(out, control.DeviceInfo{
			Path:        r.Info.Identity.Path,
			Name:        r.Info.Identity.Name,
			Kind:        r.Info.Kind.String(),
			Vendor:      r.Info.Identity.Vendor,
			Product:     r.Info.Identity.Product,
			Recommended: r.Remappable,
			Primary:     r.Primary,
			IsVirtual:   r.Info.IsVirtual,
			Reasons:     r.Reasons,
		})
	}
	return out
}

func printDeviceTable(devices []control.DeviceInfo, fromDaemon bool) {
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	header := "PATH\tKIND\tVENDOR:PRODUCT\tNAME"
	if fromDaemon {
		header = "PATH\tKIND\tVENDOR:PRODUCT\tBOUND\tNAME"
	}
	fmt.Fprintln(w, header)
	for _, d := range devices {
		name := d.Name
		if d.IsVirtual {
			name += "  (virtual — do not remap)"
		}
		if fromDaemon {
			bound := ""
			if d.Bound {
				bound = "yes"
			}
			fmt.Fprintf(w, "%s\t%s\t%04x:%04x\t%s\t%s\n", d.Path, d.Kind, d.Vendor, d.Product, bound, name)
		} else {
			fmt.Fprintf(w, "%s\t%s\t%04x:%04x\t%s\n", d.Path, d.Kind, d.Vendor, d.Product, name)
		}
	}
	w.Flush()
}

func printRecommendations(devices []control.DeviceInfo) {
	for _, d := range devices {
		verdict := "not recommended"
		if d.Recommended {
			verdict = "remappable"
		}
		if d.Primary {
			verdict += ", ★ likely node"
		}
		fmt.Printf("%s  [%s]  %s\n", d.Name, d.Kind, verdict)
		fmt.Printf("    %s  %04x:%04x", d.Path, d.Vendor, d.Product)
		if d.Bound {
			fmt.Print("  (bound by daemon)")
		}
		if d.IsVirtual {
			fmt.Print("  (virtual — do not remap)")
		}
		fmt.Println()
		for _, reason := range d.Reasons {
			fmt.Printf("    - %s\n", reason)
		}
		fmt.Println()
	}
}
