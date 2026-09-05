package conformance

import (
	"net/http/httptest"
	"testing"

	"github.com/mikefsq/goalpaca/client"
	"github.com/mikefsq/goalpaca/server"
	"github.com/mikefsq/goalpaca/sim"
)

func TestRotatorConformance(t *testing.T) {
	srv := server.New(server.Config{Discovery: server.DiscoveryConfig{Mode: server.DiscoveryOff}})
	// Fast rate so moves resolve quickly in tests.
	if err := srv.Register(server.RotatorType, 0, sim.NewRotator(sim.WithRotationRate(720))); err != nil {
		t.Fatalf("register: %v", err)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	r := client.NewRotator(ts.URL, 0)
	CheckCommon(t, r)
	CheckRotator(t, r)
}
