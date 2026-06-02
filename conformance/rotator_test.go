package conformance

import (
	"net/http/httptest"
	"testing"

	"github.com/mikefsq/goalpaca/client"
	alpacadev "github.com/mikefsq/goalpaca/server"
	"github.com/mikefsq/goalpaca/sim"
)

// TestRotatorConformance runs the ported ConformU Rotator checks through the
// client against the in-process simulator (sim -> server -> client).
func TestRotatorConformance(t *testing.T) {
	srv := alpacadev.New(alpacadev.Config{Discovery: alpacadev.DiscoveryConfig{Mode: alpacadev.DiscoveryOff}})
	// Fast rate so moves resolve quickly in tests.
	if err := srv.Register(alpacadev.RotatorType, 0, sim.NewRotator(sim.WithRotationRate(720))); err != nil {
		t.Fatalf("register: %v", err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	r := client.NewRotator(ts.URL, 0)
	CheckCommon(t, r)
	CheckRotator(t, r)
}
