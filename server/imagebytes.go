package alpacadev

import (
	"encoding/binary"
	"os"
	"runtime"
	"strings"
	"sync"
)

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

// ImageBytesMIME is the Accept/Content-Type value for the ASCOM ImageBytes
// binary image transport.
const ImageBytesMIME = "application/imagebytes"

// imageBytesMetadataLen is the fixed metadata header size: 11 little-endian
// int32 fields (ASCOM ImageBytes metadata version 1).
const imageBytesMetadataLen = 44

const imageBytesMetadataVersion = 1

// EncodeImageBytes serializes a successful image as ASCOM ImageBytes: a 44-byte
// little-endian metadata header followed by the raw pixel bytes (already in the
// frame's TransmissionElementType). For a sensor like the ASI6200 (~120 MB
// 16-bit frame) this is the only practical transport — ImageArray-JSON is
// unusable at that size.
func EncodeImageBytes(frame ImageFrame, clientTxID, serverTxID uint32) []byte {
	buf := make([]byte, imageBytesMetadataLen+len(frame.Pixels))
	encodeImageBytesInto(buf, frame, clientTxID, serverTxID)
	return buf
}

// encodeImageBytesInto writes the full ImageBytes message (44-byte header + pixels)
// into dst, which MUST be exactly imageBytesMetadataLen+len(frame.Pixels) bytes. A
// Rank-2 frame is transposed from the driver's row-major order to ASCOM's column-major
// wire order DIRECTLY into dst's pixel region — fused, with no intermediate buffer.
//
// ASCOM ImageBytes/ImageArray is a [Width,Height] array serialized with the SECOND
// dimension (Y/Height) varying fastest, i.e. column-major in image terms; sensors read
// out row-major (X fastest), so a non-square frame must be transposed or it renders as a
// diagonal shear in clients (e.g. N.I.N.A.). Rank-3 (colour planes) is copied untouched.
func encodeImageBytesInto(dst []byte, frame ImageFrame, clientTxID, serverTxID uint32) {
	transmit := frame.TransmissionElementType
	if transmit == 0 {
		transmit = frame.ElementType
	}
	putImageBytesHeader(dst, 0, clientTxID, serverTxID, frame.ElementType, transmit,
		frame.Rank, frame.Width, frame.Height, frame.Planes)
	out := dst[imageBytesMetadataLen:]
	if es := elemBytes(transmit); frame.Rank == 2 && es > 0 &&
		frame.Width > 0 && frame.Height > 0 && len(frame.Pixels) == frame.Width*frame.Height*es {
		transposeInto(out, frame.Pixels, frame.Height, frame.Width, es)
	} else {
		copy(out, frame.Pixels)
	}
}

// elemBytes is the wire size in bytes of one pixel element, or 0 if unknown.
func elemBytes(t ImageElementType) int {
	switch t {
	case ImgByte:
		return 1
	case ImgInt16, ImgUInt16:
		return 2
	case ImgInt32, ImgUInt32, ImgSingle:
		return 4
	case ImgInt64, ImgUInt64, ImgDouble:
		return 8
	}
	return 0
}

// transposeTile is the block size (elements) for the cache-blocked transpose. A column
// write strides by rows*es bytes, so processing a tile keeps both src and dst footprints
// within cache. parallelMinElems is the element count above which the transpose fans out
// across cores (a 62 MP frame moves ~244 MB read+write; below this the goroutine setup
// outweighs the gain).
const (
	transposeTile    = 64
	parallelMinElems = 1 << 20
)

// transposeElems returns the element-wise transpose of a rows×cols grid stored row-major
// as a cols×rows grid stored row-major; es is the per-element size in bytes. Allocating
// wrapper around transposeInto, used by DecodeImageBytes (the cold Go-client path).
func transposeElems(src []byte, rows, cols, es int) []byte {
	dst := make([]byte, len(src))
	transposeInto(dst, src, rows, cols, es)
	return dst
}

// transposeInto writes the transpose of src (rows×cols, row-major, es-byte elements)
// into dst (cols×rows, row-major). Cache-blocked and, for large frames, parallelized
// across row-bands — disjoint row ranges write disjoint dst columns, so no locking.
func transposeInto(dst, src []byte, rows, cols, es int) {
	if rows*cols >= parallelMinElems {
		workers := runtime.GOMAXPROCS(0)
		if workers > rows {
			workers = rows
		}
		if workers > 1 {
			band := (rows + workers - 1) / workers
			var wg sync.WaitGroup
			for r0 := 0; r0 < rows; r0 += band {
				r1 := r0 + band
				if r1 > rows {
					r1 = rows
				}
				wg.Add(1)
				go func(r0, r1 int) {
					defer wg.Done()
					transposeBand(dst, src, rows, cols, es, r0, r1)
				}(r0, r1)
			}
			wg.Wait()
			return
		}
	}
	transposeBand(dst, src, rows, cols, es, 0, rows)
}

// transposeBand transposes source rows [r0,r1) into dst, cache-blocked over tiles.
func transposeBand(dst, src []byte, rows, cols, es, r0, r1 int) {
	for rt := r0; rt < r1; rt += transposeTile {
		rEnd := min(rt+transposeTile, r1)
		for ct := 0; ct < cols; ct += transposeTile {
			cEnd := min(ct+transposeTile, cols)
			switch es {
			case 1:
				for r := rt; r < rEnd; r++ {
					sBase := r * cols
					for c := ct; c < cEnd; c++ {
						dst[c*rows+r] = src[sBase+c]
					}
				}
			case 2:
				for r := rt; r < rEnd; r++ {
					sBase := r * cols * 2
					for c := ct; c < cEnd; c++ {
						d := (c*rows + r) * 2
						s := sBase + c*2
						dst[d] = src[s]
						dst[d+1] = src[s+1]
					}
				}
			default:
				for r := rt; r < rEnd; r++ {
					for c := ct; c < cEnd; c++ {
						copy(dst[(c*rows+r)*es:(c*rows+r)*es+es], src[(r*cols+c)*es:(r*cols+c)*es+es])
					}
				}
			}
		}
	}
}

// EncodeImageBytesError serializes an error in the ImageBytes envelope: the
// metadata header with a non-zero ErrorNumber, followed by the UTF-8 error
// message as the payload (rank 0). Used when the client requested ImageBytes
// but the call failed.
func EncodeImageBytesError(errNum int, msg string, clientTxID, serverTxID uint32) []byte {
	payload := []byte(msg)
	buf := make([]byte, imageBytesMetadataLen+len(payload))
	putImageBytesHeader(buf, int32(errNum), clientTxID, serverTxID, ImgUnknown, ImgUnknown, 0, 0, 0, 0)
	copy(buf[imageBytesMetadataLen:], payload)
	return buf
}

func putImageBytesHeader(buf []byte, errNum int32, clientTxID, serverTxID uint32,
	elemType, transmitType ImageElementType, rank, dim1, dim2, dim3 int) {
	le := binary.LittleEndian
	le.PutUint32(buf[0:], uint32(imageBytesMetadataVersion))
	le.PutUint32(buf[4:], uint32(errNum))
	le.PutUint32(buf[8:], clientTxID)
	le.PutUint32(buf[12:], serverTxID)
	le.PutUint32(buf[16:], uint32(imageBytesMetadataLen)) // DataStart
	le.PutUint32(buf[20:], uint32(elemType))
	le.PutUint32(buf[24:], uint32(transmitType))
	le.PutUint32(buf[28:], uint32(rank))
	le.PutUint32(buf[32:], uint32(dim1))
	le.PutUint32(buf[36:], uint32(dim2))
	le.PutUint32(buf[40:], uint32(dim3))
}

// DecodeImageBytes parses an ASCOM ImageBytes response (the inverse of
// EncodeImageBytes). If the metadata carries a non-zero ErrorNumber it returns
// an *AlpacaError with the payload as the message; otherwise it returns the
// decoded frame (pixels copied out of data).
func DecodeImageBytes(data []byte) (ImageFrame, error) {
	if len(data) < imageBytesMetadataLen {
		return ImageFrame{}, NewError(ErrNumUnspecified, "imagebytes response shorter than metadata header")
	}
	le := binary.LittleEndian
	errNum := int32(le.Uint32(data[4:]))
	dataStart := le.Uint32(data[16:])
	if int(dataStart) > len(data) {
		dataStart = uint32(len(data))
	}
	if errNum != 0 {
		return ImageFrame{}, &AlpacaError{Number: int(errNum), Message: string(data[dataStart:])}
	}
	frame := ImageFrame{
		ElementType:             ImageElementType(le.Uint32(data[20:])),
		TransmissionElementType: ImageElementType(le.Uint32(data[24:])),
		Rank:                    int(le.Uint32(data[28:])),
		Width:                   int(le.Uint32(data[32:])),
		Height:                  int(le.Uint32(data[36:])),
		Planes:                  int(le.Uint32(data[40:])),
		Pixels:                  append([]byte(nil), data[dataStart:]...),
	}
	// Reverse the encode-time transpose (ASCOM column-major wire → sensor row-major),
	// so callers of the Go client get natural row-major pixels. See EncodeImageBytes.
	transmit := frame.TransmissionElementType
	if transmit == 0 {
		transmit = frame.ElementType
	}
	if es := elemBytes(transmit); frame.Rank == 2 && es > 0 &&
		frame.Width > 0 && frame.Height > 0 && len(frame.Pixels) == frame.Width*frame.Height*es {
		frame.Pixels = transposeElems(frame.Pixels, frame.Width, frame.Height, es)
	}
	return frame, nil
}
