// Command alpacasim serves one of every goalpaca simulated device behind a
// single Alpaca HTTP port, for testing client software and as a ConformU target
// with no hardware.
package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"

	alpacadev "github.com/mikefsq/goalpaca/server"
	"github.com/mikefsq/goalpaca/sim"
)

func main() {
	port := flag.Int("port", 11111, "Alpaca HTTP port")
	discovery := flag.String("discovery", "direct", "discovery mode: direct (self-answer UDP 32227) | off")
	ipv6 := flag.Bool("ipv6", false, "also answer IPv6 multicast discovery (direct mode)")
	quiet := flag.Bool("quiet", false, "disable per-request logging")
	flag.Parse()

	reqLog := log.Default()
	if *quiet {
		reqLog = nil
	}

	mode := alpacadev.DiscoveryDirect
	if *discovery == "off" {
		mode = alpacadev.DiscoveryOff
	}

	srv := alpacadev.New(alpacadev.Config{
		AlpacaPort:          *port,
		Discovery:           alpacadev.DiscoveryConfig{Mode: mode, EnableIPv6: *ipv6},
		ServerName:          "goalpaca Alpaca Simulators",
		Manufacturer:        "goalpaca",
		ManufacturerVersion: "1.0",
		Location:            "Simulated",
		Logger:              reqLog,
	})

	reg := func(t alpacadev.DeviceType, d alpacadev.Device) {
		if err := srv.Register(t, 0, d); err != nil {
			log.Fatalf("alpacasim: register %s: %v", t, err)
		}
	}
	reg(alpacadev.CameraType, sim.NewCamera())
	reg(alpacadev.CoverCalibratorType, sim.NewCoverCalibrator())
	reg(alpacadev.DomeType, sim.NewDome())
	reg(alpacadev.FilterWheelType, sim.NewFilterWheel())
	reg(alpacadev.FocuserType, sim.NewFocuser())
	reg(alpacadev.ObservingConditionsType, sim.NewObservingConditions())
	reg(alpacadev.RotatorType, sim.NewRotator())
	reg(alpacadev.SafetyMonitorType, sim.NewSafetyMonitor())
	reg(alpacadev.SwitchType, sim.NewSwitch())
	reg(alpacadev.TelescopeType, sim.NewTelescope())

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("alpacasim: serving 10 simulated devices on :%d (discovery=%s)", *port, *discovery)
	if err := srv.Run(ctx); err != nil {
		log.Fatalf("alpacasim: %v", err)
	}
}
