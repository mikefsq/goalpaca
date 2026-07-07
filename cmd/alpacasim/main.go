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

	"github.com/mikefsq/goalpaca/server"
	"github.com/mikefsq/goalpaca/sim"
)

func main() {
	port := flag.Int("port", 11111, "Alpaca HTTP port")
	discovery := flag.String("discovery", "direct", "discovery mode: direct (self-answer UDP 32227) | off")
	ipv6 := flag.Bool("ipv6", false, "also answer IPv6 multicast discovery (direct mode)")
	quiet := flag.Bool("quiet", false, "disable per-request logging")
	strictParamCasing := flag.Bool("strict-param-casing", false,
		"match request parameter names exactly instead of case-insensitively; "+
			"only needed to satisfy ConformU's stricter-than-spec \"Check Alpaca Protocol\" casing tests")
	flag.Parse()

	reqLog := log.Default()
	if *quiet {
		reqLog = nil
	}

	var mode server.DiscoveryMode
	switch *discovery {
	case "direct":
		mode = server.DiscoveryDirect
	case "off":
		mode = server.DiscoveryOff
	default:
		log.Fatalf("unknown -discovery mode %q (want direct or off)", *discovery)
	}

	srv := server.New(server.Config{
		AlpacaPort:          *port,
		Discovery:           server.DiscoveryConfig{Mode: mode, EnableIPv6: *ipv6},
		ServerName:          "goalpaca Alpaca Simulators",
		Manufacturer:        "goalpaca",
		ManufacturerVersion: "1.0",
		Location:            "Simulated",
		Logger:              reqLog,
		StrictParamCasing:   *strictParamCasing,
	})

	reg := func(t server.DeviceType, d server.Device) {
		if err := srv.Register(t, 0, d); err != nil {
			log.Fatalf("alpacasim: register %s: %v", t, err)
		}
	}
	reg(server.CameraType, sim.NewCamera())
	reg(server.CoverCalibratorType, sim.NewCoverCalibrator())
	reg(server.DomeType, sim.NewDome())
	reg(server.FilterWheelType, sim.NewFilterWheel())
	reg(server.FocuserType, sim.NewFocuser())
	reg(server.ObservingConditionsType, sim.NewObservingConditions())
	reg(server.RotatorType, sim.NewRotator())
	reg(server.SafetyMonitorType, sim.NewSafetyMonitor())
	reg(server.SwitchType, sim.NewSwitch())
	reg(server.TelescopeType, sim.NewTelescope())

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("alpacasim: serving 10 simulated devices on :%d (discovery=%s)", *port, *discovery)
	if err := srv.Run(ctx); err != nil {
		log.Fatalf("alpacasim: %v", err)
	}
}
