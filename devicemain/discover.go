package devicemain

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/mikefsq/goalpaca/registry"
)

// Discover writes hardware scan results as JSON. Custom launchers can use the
// same command as RunWith. It never constructs a driver or reads configuration.
// Scanners must honor the context and must not disturb devices already in use.
func Discover(ctx context.Context, drv registry.Driver, out io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	type found struct {
		Label  string         `json:"label"`
		Values map[string]any `json:"values"`
	}
	result := struct {
		Driver    string   `json:"driver"`
		Supported bool     `json:"supported"`
		Identity  []string `json:"identity"`
		Devices   []found  `json:"devices"`
		Error     string   `json:"error,omitempty"`
	}{Driver: drv.Name, Supported: drv.Scan != nil, Identity: append([]string{}, drv.Identity...), Devices: []found{}}
	var scanErr error
	if drv.Scan != nil {
		var devices []registry.Found
		scanErr = ctx.Err()
		if scanErr == nil {
			devices, scanErr = drv.Scan(ctx)
		}
		if scanErr == nil {
			scanErr = ctx.Err()
		}
		for _, device := range devices {
			values := device.Values
			if values == nil {
				values = map[string]any{}
			}
			result.Devices = append(result.Devices, found{device.Label, values})
		}
		if scanErr != nil {
			result.Error = scanErr.Error()
		}
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		return err
	}
	if scanErr != nil {
		return fmt.Errorf("hardware discovery: %w", scanErr)
	}
	return nil
}
