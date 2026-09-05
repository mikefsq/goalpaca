package client

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/mikefsq/goalpaca/alpaca"
)

// u16Sink is the consumer an imaging app actually wants: column-major wire bytes converted into
// one row-major []uint16, allocated once from the header and never reallocated.
//
// It carries a partial element across Write boundaries because the body arrives in arbitrary
// chunks, and it walks the destination in COLUMN RUNS so the strided write is hoisted out of the
// per-sample path — the source read stays sequential and each run touches h/32 destination lines
// that the next 31 columns reuse.
type u16Sink struct {
	w, h, es int
	pix      []uint16
	carry    [8]byte
	held     int
	x, y     int
}

func (s *u16Sink) Header(m alpaca.ImageBytesHeader) error {
	s.w, s.h = m.Width, m.Height
	s.es = alpaca.ElementSize(m.Transmit())
	if s.es != 2 {
		return fmt.Errorf("u16Sink: element size %d unsupported", s.es)
	}
	s.pix = make([]uint16, s.w*s.h)
	return nil
}

func (s *u16Sink) Write(p []byte) (int, error) {
	n := len(p)
	le := binary.LittleEndian
	if s.held > 0 {
		take := s.es - s.held
		if take > len(p) {
			take = len(p)
		}
		copy(s.carry[s.held:], p[:take])
		s.held += take
		p = p[take:]
		if s.held < s.es {
			return n, nil
		}
		s.put(le.Uint16(s.carry[:]))
		s.held = 0
	}
	for avail := len(p) / s.es; avail > 0; avail = len(p) / s.es {
		run := s.h - s.y
		if run > avail {
			run = avail
		}
		base := s.y*s.w + s.x
		for i := 0; i < run; i++ {
			s.pix[base+i*s.w] = le.Uint16(p[i*s.es:])
		}
		p = p[run*s.es:]
		s.y += run
		if s.y == s.h {
			s.y, s.x = 0, s.x+1
		}
	}
	if len(p) > 0 {
		copy(s.carry[:], p)
		s.held = len(p)
	}
	return n, nil
}

func (s *u16Sink) put(v uint16) {
	s.pix[s.y*s.w+s.x] = v
	s.y++
	if s.y == s.h {
		s.y, s.x = 0, s.x+1
	}
}

func (s *u16Sink) Close() error { return nil }

// widen is what a consumer of ImageArrayCtx must still do after the client returns: turn the
// (already transposed) row-major bytes into uint16 samples. Counted against the old path because
// it is work the streaming sink has already done by the time it returns.
func widen(pixels []byte) []uint16 {
	out := make([]uint16, len(pixels)/2)
	le := binary.LittleEndian
	for i := range out {
		out[i] = le.Uint16(pixels[i*2:])
	}
	return out
}

// imageServer serves one fixed ImageBytes frame, encoded once outside the timed region.
func imageServer(t testing.TB, w, h int) (*httptest.Server, []uint16) {
	t.Helper()
	// ROW-MAJOR source pixels: EncodeImageBytes performs the encode-time transpose itself, so what
	// reaches the wire is column-major and what a correct decoder returns is this buffer again.
	// Feeding it pre-transposed data instead makes the wire row-major and the whole fixture a lie
	// that the sink is then measured against — which is exactly what the first run of this test
	// caught.
	//
	// The value encodes its own (x,y), so a transpose that is wrong in either direction produces a
	// mismatch rather than a plausible image. w != h, so a swapped pair cannot coincidentally pass.
	pixels := make([]byte, w*h*2)
	want := make([]uint16, w*h)
	le := binary.LittleEndian
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint16((x*31 + y*17) & 0xffff)
			want[y*w+x] = v
			le.PutUint16(pixels[(y*w+x)*2:], v)
		}
	}
	body := alpaca.EncodeImageBytes(alpaca.ImageFrame{
		Rank: 2, Width: w, Height: h, ElementType: alpaca.ImgUInt16,
		TransmissionElementType: alpaca.ImgUInt16, Pixels: pixels,
	}, 0, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", alpaca.ImageBytesMIME)
		rw.WriteHeader(http.StatusOK)
		rw.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, want
}

func camAt(t testing.TB, srv *httptest.Server) *Camera {
	t.Helper()
	return NewCamera(srv.Listener.Addr().String(), 0)
}

func TestImageArrayIntoMatchesImageArrayCtx(t *testing.T) {
	// Deliberately not a multiple of the scratch size, and odd in both axes, so elements straddle
	// Write boundaries and the carry path is exercised rather than skipped.
	const w, h = 337, 199
	srv, want := imageServer(t, w, h)
	cam := camAt(t, srv)

	var sink u16Sink
	if err := cam.ImageArrayInto(context.Background(), &sink); err != nil {
		t.Fatalf("ImageArrayInto: %v", err)
	}
	if len(sink.pix) != len(want) {
		t.Fatalf("streamed %d samples, want %d", len(sink.pix), len(want))
	}
	for i := range want {
		if sink.pix[i] != want[i] {
			t.Fatalf("streamed sample %d (x=%d y=%d) = %d, want %d",
				i, i%w, i/w, sink.pix[i], want[i])
		}
	}
	// And the existing path agrees, so the fixture is not merely self-consistent.
	fr, err := cam.ImageArrayCtx(context.Background())
	if err != nil {
		t.Fatalf("ImageArrayCtx: %v", err)
	}
	got := widen(fr.Pixels)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("buffered sample %d = %d, want %d", i, got[i], want[i])
		}
	}
}

func benchBoth(b *testing.B, w, h int) {
	srv, _ := imageServer(b, w, h)
	cam := camAt(b, srv)
	bytes := int64(w) * int64(h) * 2

	b.Run("buffered_ImageArrayCtx", func(b *testing.B) {
		b.SetBytes(bytes)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			fr, err := cam.ImageArrayCtx(context.Background())
			if err != nil {
				b.Fatal(err)
			}
			if pix := widen(fr.Pixels); len(pix) == 0 {
				b.Fatal("empty")
			}
		}
	})
	b.Run("streamed_ImageArrayInto", func(b *testing.B) {
		b.SetBytes(bytes)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var sink u16Sink
			if err := cam.ImageArrayInto(context.Background(), &sink); err != nil {
				b.Fatal(err)
			}
			if len(sink.pix) == 0 {
				b.Fatal("empty")
			}
		}
	})
}

// 1936x1096 — the ASI462MC on the bench.
func BenchmarkImage4MB(b *testing.B) { benchBoth(b, 1936, 1096) }

// 9576x6388 — a 61 MP full-frame sensor, where the buffer count actually hurts.
func BenchmarkImage122MB(b *testing.B) { benchBoth(b, 9576, 6388) }

// parSink is u16Sink with the conversion fanned out across cores, and a chunk big enough for that
// to pay. The destination indices a chunk touches are disjoint per column run, so workers never
// share a write and no locking is needed — the same property that lets transposeElems parallelize.
type parSink struct {
	u16Sink
	chunk int
}

func (s *parSink) ChunkSize() int { return s.chunk }

func (s *parSink) Write(p []byte) (int, error) {
	n := len(p)
	le := binary.LittleEndian
	// Finish an element split across the previous Write FIRST. io.CopyBuffer is free to deliver an
	// odd byte count, and dispatching before draining the carry would place every remaining sample
	// in the chunk one byte out — a whole-frame corruption that still decodes to a plausible image.
	if s.held > 0 {
		take := s.es - s.held
		if take > len(p) {
			take = len(p)
		}
		copy(s.carry[s.held:], p[:take])
		s.held += take
		p = p[take:]
		if s.held < s.es {
			return n, nil
		}
		s.put(le.Uint16(s.carry[:]))
		s.held = 0
	}
	full := (len(p) / s.es) * s.es
	if full > 0 {
		s.convertParallel(p[:full])
		p = p[full:]
	}
	if len(p) > 0 {
		copy(s.carry[:], p)
		s.held = len(p)
	}
	return n, nil
}

// convertParallel splits the chunk on COLUMN BOUNDARIES so each worker owns whole column runs and
// can compute its own destination offsets from the element index alone.
func (s *parSink) convertParallel(p []byte) {
	elems := len(p) / s.es
	workers := runtime.GOMAXPROCS(0)
	// Below a few elements per worker the dispatch costs more than the work.
	if workers > elems/8192 {
		workers = elems / 8192
	}
	if workers < 2 {
		s.convertRange(p, 0, elems)
		s.advance(elems)
		return
	}
	band := (elems + workers - 1) / workers
	var wg sync.WaitGroup
	for lo := 0; lo < elems; lo += band {
		hi := lo + band
		if hi > elems {
			hi = elems
		}
		wg.Add(1)
		go func(lo, hi int) {
			defer wg.Done()
			s.convertRange(p, lo, hi)
		}(lo, hi)
	}
	wg.Wait()
	s.advance(elems)
}

// convertRange writes elements [lo,hi) of this chunk, resolving each one's (x,y) from the sink's
// starting position plus the element's offset. Reads are sequential; writes stride by w.
func (s *parSink) convertRange(p []byte, lo, hi int) {
	le := binary.LittleEndian
	start := s.y + lo // element index measured from (x, y=0) of the current column
	x := s.x + start/s.h
	y := start % s.h
	for i := lo; i < hi; {
		run := s.h - y
		if run > hi-i {
			run = hi - i
		}
		base := y*s.w + x
		for k := 0; k < run; k++ {
			s.pix[base+k*s.w] = le.Uint16(p[(i+k)*s.es:])
		}
		i += run
		y += run
		if y == s.h {
			y, x = 0, x+1
		}
	}
}

func (s *parSink) advance(elems int) {
	total := s.y + elems
	s.x += total / s.h
	s.y = total % s.h
}

func BenchmarkImage122MBParallel(b *testing.B) {
	const w, h = 9576, 6388
	srv, _ := imageServer(b, w, h)
	cam := camAt(b, srv)
	for _, chunk := range []int{256 << 10, 4 << 20, 16 << 20} {
		b.Run(fmt.Sprintf("chunk_%dKB", chunk>>10), func(b *testing.B) {
			b.SetBytes(int64(w) * int64(h) * 2)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sink := &parSink{chunk: chunk}
				if err := cam.ImageArrayInto(context.Background(), sink); err != nil {
					b.Fatal(err)
				}
				if len(sink.pix) == 0 {
					b.Fatal("empty")
				}
			}
		})
	}
}

func TestParallelSinkMatchesScalar(t *testing.T) {
	const w, h = 337, 199
	srv, want := imageServer(t, w, h)
	cam := camAt(t, srv)

	// A chunk that is not a multiple of the element size, so a sample straddles every boundary.
	for _, chunk := range []int{1, 3, 4095, 1 << 20} {
		sink := &parSink{chunk: chunk}
		if err := cam.ImageArrayInto(context.Background(), sink); err != nil {
			t.Fatalf("chunk %d: %v", chunk, err)
		}
		for i := range want {
			if sink.pix[i] != want[i] {
				t.Fatalf("chunk %d: sample %d (x=%d y=%d) = %d, want %d",
					chunk, i, i%w, i/w, sink.pix[i], want[i])
			}
		}
	}
}

func BenchmarkImage4MBParallel(b *testing.B) {
	const w, h = 1936, 1096
	srv, _ := imageServer(b, w, h)
	cam := camAt(b, srv)
	for _, chunk := range []int{256 << 10, 4 << 20} {
		b.Run(fmt.Sprintf("chunk_%dKB", chunk>>10), func(b *testing.B) {
			b.SetBytes(int64(w) * int64(h) * 2)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sink := &parSink{chunk: chunk}
				if err := cam.ImageArrayInto(context.Background(), sink); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestJSONServerFallsBackOnceNotEveryFrame(t *testing.T) {
	var streamAttempts int
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		// A server that ignores Accept: always JSON, never imagebytes.
		if strings.Contains(r.Header.Get("Accept"), alpaca.ImageBytesMIME) {
			streamAttempts++
		}
		rw.Header().Set("Content-Type", "application/json")
		rw.Write([]byte(`{"Value":[[1,2],[3,4]],"Type":2,"Rank":2,"ErrorNumber":0,"ErrorMessage":""}`))
	}))
	defer srv.Close()
	cam := NewCamera(srv.Listener.Addr().String(), 0)

	for i := 0; i < 5; i++ {
		var sink u16Sink
		err := cam.ImageArrayInto(context.Background(), &sink)
		if !IsUnstreamable(err) {
			t.Fatalf("frame %d: err = %v, want ErrUnstreamable so the caller falls back", i, err)
		}
	}
	if streamAttempts != 1 {
		t.Errorf("%d streaming attempts against a JSON-only server, want 1 — the refusal must "+
			"latch, or every frame pays for the discovery", streamAttempts)
	}
	// And the buffering path still works against that same server, which is the whole point of
	// falling back rather than failing.
	if _, err := cam.ImageArrayCtx(context.Background()); err != nil {
		t.Errorf("ImageArrayCtx against a JSON server: %v", err)
	}
}
