package devicemain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/mikefsq/goalpaca/registry"
)

func TestDiscoverResults(t *testing.T) {
	for _, tc := range []struct {
		name    string
		scan    func(context.Context) ([]registry.Found, error)
		wantErr bool
		count   int
	}{
		{"unsupported", nil, false, 0},
		{"empty", func(context.Context) ([]registry.Found, error) { return nil, nil }, false, 0},
		{"found", func(ctx context.Context) ([]registry.Found, error) {
			if _, ok := ctx.Deadline(); !ok {
				t.Error("missing scan deadline")
			}
			return []registry.Found{{Label: "Camera", Values: map[string]any{"serial": "abc", "index": 0}}, {Label: "Busy camera", Values: map[string]any{"index": 1}}}, nil
		}, false, 2},
		{"error", func(context.Context) ([]registry.Found, error) { return nil, errors.New("bus unavailable") }, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			err := Discover(context.Background(), registry.Driver{Name: "test", Identity: []string{"serial", "index"}, Scan: tc.scan}, &b)
			if (err != nil) != tc.wantErr {
				t.Fatal(err)
			}
			var result struct {
				Supported bool
				Devices   []struct{ Values map[string]any }
				Error     string
			}
			if err := json.Unmarshal(b.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Supported != (tc.scan != nil) || len(result.Devices) != tc.count {
				t.Fatal(b.String())
			}
			if tc.wantErr && result.Error == "" {
				t.Fatal("missing JSON diagnostic")
			}
			if tc.count == 2 {
				if _, ok := result.Devices[1].Values["serial"]; ok {
					t.Fatal("invented busy-device serial")
				}
			}
		})
	}
}
func TestDiscoverCLIExitsBeforeConfigAndConstruction(t *testing.T) {
	lastBuilt = nil
	var b bytes.Buffer
	err := RunWith("dm-focuser", Options{Args: []string{"-discover", "-config", "/does/not/exist"}, Stdout: &b, Stderr: io.Discard, BeforeRun: func() (bool, error) { t.Fatal("startup hook ran"); return true, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if lastBuilt != nil {
		t.Fatal("constructed device")
	}
	var result map[string]any
	if err := json.Unmarshal(b.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["supported"] != false {
		t.Fatal(b.String())
	}
}
func TestDiscoverCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var b bytes.Buffer
	err := Discover(ctx, registry.Driver{Name: "test", Scan: func(context.Context) ([]registry.Found, error) {
		t.Fatal("scan ran after cancellation")
		return nil, nil
	}}, &b)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
