package sim

import (
	"sync"
	"time"

	alpacadev "github.com/mikefsq/goalpaca/server"
)

// FilterWheel is a simulated ASCOM FilterWheel. A move takes a short, fixed time
// during which Position reads -1 (the ASCOM "in motion" sentinel).
type FilterWheel struct {
	alpacadev.BaseFilterWheel

	mu        sync.Mutex
	names     []string
	offsets   []int
	position  int
	moveUntil time.Time
}

// FilterWheelOption configures a simulated FilterWheel.
type FilterWheelOption func(*FilterWheel)

// WithFilters sets the filter names and their focus offsets (must be equal length).
func WithFilters(names []string, offsets []int) FilterWheelOption {
	return func(fw *FilterWheel) {
		fw.names = append([]string(nil), names...)
		fw.offsets = append([]int(nil), offsets...)
	}
}

// NewFilterWheel creates a simulated 4-slot FilterWheel.
func NewFilterWheel(opts ...FilterWheelOption) *FilterWheel {
	fw := &FilterWheel{
		names:   []string{"Red", "Green", "Blue", "Luminance"},
		offsets: []int{0, 0, 0, 0},
	}
	fw.ID = "goalpaca-sim-filterwheel-1"
	fw.DevName = "Alpaca FilterWheel Simulator"
	fw.Desc = "goalpaca simulated filter wheel"
	fw.Info = "goalpaca sim"
	fw.Version = "1.0"
	fw.IfaceVer = 3
	for _, o := range opts {
		o(fw)
	}
	return fw
}

func (fw *FilterWheel) Names() []string {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	return append([]string(nil), fw.names...)
}

func (fw *FilterWheel) FocusOffsets() []int {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	return append([]int(nil), fw.offsets...)
}

func (fw *FilterWheel) Position() int {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if time.Now().Before(fw.moveUntil) {
		return -1 // moving
	}
	return fw.position
}

func (fw *FilterWheel) SetPosition(slot int) error {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if slot < 0 || slot >= len(fw.names) {
		return alpacadev.ErrInvalidValue
	}
	fw.position = slot
	fw.moveUntil = time.Now().Add(300 * time.Millisecond)
	return nil
}
