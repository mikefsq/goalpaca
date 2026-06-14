package alpacadev

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestImageBytesTranspose locks the ASCOM ImageBytes ordering: a Rank-2 frame is
// supplied row-major (X fastest) and must go onto the wire column-major (Y fastest),
// and DecodeImageBytes must restore the original row-major buffer. A non-square frame
// (W != H) is used so a transpose error can't hide.
func TestImageBytesTranspose(t *testing.T) {
	w, h := 3, 2 // row-major values 0..5: row0=[0,1,2], row1=[3,4,5]
	src := make([]byte, w*h*2)
	for i := 0; i < w*h; i++ {
		binary.LittleEndian.PutUint16(src[i*2:], uint16(i))
	}
	frame := ImageFrame{
		Rank: 2, Width: w, Height: h,
		ElementType:             ImgInt32,
		TransmissionElementType: ImgUInt16,
		Pixels:                  src,
	}

	buf := EncodeImageBytes(frame, 0, 0)
	pix := buf[imageBytesMetadataLen:]

	// Column-major wire order over (x,y), y fastest: x0y0,x0y1,x1y0,x1y1,x2y0,x2y1
	// = src(0,0),src(1,0),src(0,1),src(1,1),src(0,2),src(1,2) = 0,3,1,4,2,5
	want := []uint16{0, 3, 1, 4, 2, 5}
	for i, wv := range want {
		if got := binary.LittleEndian.Uint16(pix[i*2:]); got != wv {
			t.Errorf("wire element %d = %d, want %d", i, got, wv)
		}
	}

	// Round-trip restores row-major.
	got, err := DecodeImageBytes(buf)
	if err != nil {
		t.Fatalf("DecodeImageBytes: %v", err)
	}
	if got.Width != w || got.Height != h || got.Rank != 2 {
		t.Fatalf("decoded dims = %dx%d rank %d; want %dx%d rank 2", got.Width, got.Height, got.Rank, w, h)
	}
	if !bytes.Equal(got.Pixels, src) {
		t.Errorf("round-trip pixels = % d; want row-major % d", got.Pixels, src)
	}
}

// BenchmarkTranspose62MP measures the wire transpose on a full ASI6200 RAW16 frame.
func BenchmarkTranspose62MP(b *testing.B) {
	const w, h = 9576, 6388
	src := make([]byte, w*h*2)
	b.SetBytes(int64(len(src)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = transposeElems(src, h, w, 2)
	}
}

// TestTransposeParallelCorrect exercises the parallel/tiled path (frame larger than
// parallelMinElems, non-square so a band-split bug can't hide): exact orientation vs a
// naive reference, plus the round-trip identity.
func TestTransposeParallelCorrect(t *testing.T) {
	const w, h = 1500, 900 // 1.35M elems > parallelMinElems → fans out across cores
	if w*h < parallelMinElems {
		t.Fatalf("test frame too small to hit the parallel path")
	}
	src := make([]byte, w*h*2)
	for i := 0; i < w*h; i++ {
		binary.LittleEndian.PutUint16(src[i*2:], uint16(i*2654435761>>11)) // scrambled, position-dependent
	}

	// Encode order is transposeInto(out, pixels, rows=h, cols=w): src is h rows × w cols.
	got := make([]byte, len(src))
	transposeInto(got, src, h, w, 2)
	for r := 0; r < h; r++ {
		for c := 0; c < w; c++ {
			s := binary.LittleEndian.Uint16(src[(r*w+c)*2:])
			d := binary.LittleEndian.Uint16(got[(c*h+r)*2:]) // dst[c*h + r] = src[r*w + c]
			if s != d {
				t.Fatalf("transpose mismatch at (r=%d,c=%d): got %d want %d", r, c, d, s)
			}
		}
	}

	// Round-trip (rows/cols swapped) restores the original.
	back := make([]byte, len(src))
	transposeInto(back, got, w, h, 2)
	if !bytes.Equal(back, src) {
		t.Fatal("double transpose did not restore the original")
	}
}

// BenchmarkEncode62MP measures the full fused encode (transpose + header) into a
// reused buffer for a 62 MP ASI6200 RAW16 frame — the production hot path.
func BenchmarkEncode62MP(b *testing.B) {
	const w, h = 9576, 6388
	frame := ImageFrame{
		Rank: 2, Width: w, Height: h,
		ElementType:             ImgInt32,
		TransmissionElementType: ImgUInt16,
		Pixels:                  make([]byte, w*h*2),
	}
	dst := make([]byte, imageBytesMetadataLen+len(frame.Pixels))
	b.SetBytes(int64(len(frame.Pixels)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encodeImageBytesInto(dst, frame, 0, 0)
	}
}
