// Command alpacadiscover runs Alpaca UDP discovery (IPv4 + IPv6) and prints the
// servers it finds and each server's configured devices.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/mikefsq/goalpaca/client"
)

func main() {
	timeout := flag.Duration("timeout", 2*time.Second, "how long to listen for discovery replies")
	flag.Parse()

	servers, err := client.Discover(*timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "discover: %v\n", err)
		os.Exit(1)
	}
	if len(servers) == 0 {
		fmt.Println("No Alpaca servers found.")
		return
	}

	fmt.Printf("Found %d Alpaca server(s):\n", len(servers))
	for _, s := range servers {
		fmt.Printf("\n%s  (AlpacaPort %d)\n", s.Address, s.AlpacaPort)
		devs, err := s.ConfiguredDevices()
		if err != nil {
			fmt.Printf("  (could not list devices: %v)\n", err)
			continue
		}
		if len(devs) == 0 {
			fmt.Println("  (no configured devices)")
			continue
		}
		for _, d := range devs {
			fmt.Printf("  - %-20s #%d  %-26s %s\n", d.DeviceType, d.DeviceNumber, d.DeviceName, d.UniqueID)
		}
	}
}
