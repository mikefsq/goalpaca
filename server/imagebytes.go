package server

import (
	"os"
	"strings"
	"sync"
)

// The ImageBytes codec itself lives in the leaf alpaca package (shared with
// the client); see aliases.go for the re-exports. This file keeps the
// serving-side machinery: the output buffer pool and the debug switch.

// imageBufPool recycles the large ImageBytes output buffers (a 62 MP RAW16 frame is
// ~122 MB) so steady-state capture doesn't allocate and GC one per frame. Holds *[]byte
// to avoid the per-Put interface-boxing allocation. sync.Pool drops entries on GC, so an
// idle camera doesn't pin the memory.
var imageBufPool sync.Pool

// getImageBuf returns a buffer of exactly n bytes, reusing a pooled one when large enough.
func getImageBuf(n int) []byte {
	if v := imageBufPool.Get(); v != nil {
		if b := v.(*[]byte); cap(*b) >= n {
			return (*b)[:n]
		}
	}
	return make([]byte, n)
}

// putImageBuf returns a buffer to the pool. Safe to call after w.Write returns — the
// http stack has copied the body into the connection by then.
func putImageBuf(b []byte) { imageBufPool.Put(&b) }

// imageDebug enables per-frame ImageBytes timing on the console (encode vs write
// milliseconds). Off by default; set GOALPACA_IMAGE_DEBUG=1 (or true/yes/on).
var imageDebug = func() bool {
	switch strings.ToLower(os.Getenv("GOALPACA_IMAGE_DEBUG")) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}()
