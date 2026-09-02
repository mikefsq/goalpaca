package alpaca

import (
	"encoding/binary"
	"runtime"
	"sync"
)

// ImageBytesMIME is the Accept/Content-Type value for the ASCOM ImageBytes
// binary image transport.
const ImageBytesMIME = "application/imagebytes"

// ImageBytesHeaderLen is the fixed metadata header size: 11 little-endian
// int32 fields (ASCOM ImageBytes metadata version 1).
const ImageBytesHeaderLen = 44

// ImageBytesVersion is the ImageBytes metadata version this codec speaks.
const ImageBytesVersion = 1

// EncodeImageBytes serializes a successful image as ASCOM ImageBytes: a 44-byte
// little-endian metadata header followed by the raw pixel bytes (already in the
// frame's TransmissionElementType). For a sensor like the ASI6200 (~120 MB
// 16-bit frame) this is the only practical transport — ImageArray-JSON is
// unusable at that size.
func EncodeImageBytes(frame ImageFrame, clientTxID, serverTxID uint32) []byte {
	buf := make([]byte, ImageBytesHeaderLen+len(frame.Pixels))
	EncodeImageBytesInto(buf, frame, clientTxID, serverTxID)
	return buf
}

// EncodeImageBytesInto writes the full ImageBytes message (44-byte header + pixels)
// into dst, which MUST be exactly ImageBytesHeaderLen+len(frame.Pixels) bytes — the
// allocation-free form of EncodeImageBytes for callers that pool buffers. A
// Rank-2 frame is transposed from the driver's row-major order to ASCOM's column-major
// wire order DIRECTLY into dst's pixel region — fused, with no intermediate buffer.
//
// ASCOM ImageBytes/ImageArray is a [Width,Height] array serialized with the SECOND
// dimension (Y/Height) varying fastest, i.e. column-major in image terms; sensors read
// out row-major (X fastest), so a non-square frame must be transposed or it renders as a
// diagonal shear in clients (e.g. N.I.N.A.). Rank-3 (colour planes) is copied untouched.
func EncodeImageBytesInto(dst []byte, frame ImageFrame, clientTxID, serverTxID uint32) {
	transmit := frame.TransmissionElementType
	if transmit == 0 {
		transmit = frame.ElementType
	}
	putImageBytesHeader(dst, 0, clientTxID, serverTxID, frame.ElementType, transmit,
		frame.Rank, frame.Width, frame.Height, frame.Planes)
	out := dst[ImageBytesHeaderLen:]
	if es := ElementSize(transmit); frame.Rank == 2 && es > 0 &&
		frame.Width > 0 && frame.Height > 0 && len(frame.Pixels) == frame.Width*frame.Height*es {
		transposeInto(out, frame.Pixels, frame.Height, frame.Width, es)
	} else {
		copy(out, frame.Pixels)
	}
}

// ElementSize is the wire size in bytes of one pixel element, or 0 if unknown.
func ElementSize(t ImageElementType) int {
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
	buf := make([]byte, ImageBytesHeaderLen+len(payload))
	putImageBytesHeader(buf, int32(errNum), clientTxID, serverTxID, ImgUnknown, ImgUnknown, 0, 0, 0, 0)
	copy(buf[ImageBytesHeaderLen:], payload)
	return buf
}

func putImageBytesHeader(buf []byte, errNum int32, clientTxID, serverTxID uint32,
	elemType, transmitType ImageElementType, rank, dim1, dim2, dim3 int) {
	le := binary.LittleEndian
	le.PutUint32(buf[0:], uint32(ImageBytesVersion))
	le.PutUint32(buf[4:], uint32(errNum))
	le.PutUint32(buf[8:], clientTxID)
	le.PutUint32(buf[12:], serverTxID)
	le.PutUint32(buf[16:], uint32(ImageBytesHeaderLen)) // DataStart
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
	if len(data) < ImageBytesHeaderLen {
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
	}
	// Reverse the encode-time transpose (ASCOM column-major wire → sensor row-major),
	// so callers of the Go client get natural row-major pixels. See EncodeImageBytes.
	transmit := frame.TransmissionElementType
	if transmit == 0 {
		transmit = frame.ElementType
	}
	pixels := data[dataStart:]
	// The DETACH COPY is only made when nothing else is going to allocate.
	//
	// Pixels must not alias data, because the caller owns that buffer and is free to reuse it. But
	// transposeElems ALREADY returns a fresh allocation, so on the ordinary path — a rank-2 frame
	// from any camera — copying first and transposing second allocated and walked the whole frame
	// twice to produce one result. At 122 MB a frame that is a wasted allocation and a wasted pass.
	//
	// So the transpose consumes the caller's buffer directly (it only reads it) and the copy is
	// kept for the paths where no transpose runs: rank 3, an unknown element width, or a payload
	// whose length disagrees with its dimensions.
	if es := ElementSize(transmit); frame.Rank == 2 && es > 0 &&
		frame.Width > 0 && frame.Height > 0 && len(pixels) == frame.Width*frame.Height*es {
		frame.Pixels = transposeElems(pixels, frame.Width, frame.Height, es)
	} else {
		frame.Pixels = append([]byte(nil), pixels...)
	}
	return frame, nil
}

// ImageBytesHeader is the fixed 44-byte metadata prefix, parsed on its own.
//
// It exists so a client can size and shape its destination BEFORE the pixels arrive, which is what
// makes a streaming decode possible: everything needed to allocate the final buffer — the element
// type actually on the wire, the rank, and the three dimensions — is in the first 44 bytes.
// DecodeImageBytes cannot serve that purpose because it takes the whole payload by value.
type ImageBytesHeader struct {
	ErrorNumber             int
	DataStart               int
	ElementType             ImageElementType
	TransmissionElementType ImageElementType
	Rank                    int
	Width                   int
	Height                  int
	Planes                  int
}

// Transmit is the element type the PIXELS are in, which is not always the logical one: a server may
// ship a 16-bit sensor's frame as UInt16 while presenting it as Int32. Reading the buffer with the
// logical type would misalign every sample after the first.
func (h ImageBytesHeader) Transmit() ImageElementType {
	if h.TransmissionElementType != 0 {
		return h.TransmissionElementType
	}
	return h.ElementType
}

// PixelBytes is how many pixel bytes the header says will follow, or 0 when the dimensions do not
// describe a frame this codec can size (an error payload, or an element type of unknown width).
func (h ImageBytesHeader) PixelBytes() int {
	es := ElementSize(h.Transmit())
	if es <= 0 || h.Width <= 0 || h.Height <= 0 {
		return 0
	}
	planes := h.Planes
	if h.Rank != 3 || planes <= 0 {
		planes = 1
	}
	return h.Width * h.Height * planes * es
}

// ParseImageBytesHeader reads the metadata prefix. data must be at least ImageBytesHeaderLen long;
// the pixels need not be present, which is the point.
func ParseImageBytesHeader(data []byte) (ImageBytesHeader, error) {
	if len(data) < ImageBytesHeaderLen {
		return ImageBytesHeader{}, NewError(ErrNumUnspecified, "imagebytes response shorter than metadata header")
	}
	le := binary.LittleEndian
	return ImageBytesHeader{
		ErrorNumber:             int(int32(le.Uint32(data[4:]))),
		DataStart:               int(le.Uint32(data[16:])),
		ElementType:             ImageElementType(le.Uint32(data[20:])),
		TransmissionElementType: ImageElementType(le.Uint32(data[24:])),
		Rank:                    int(le.Uint32(data[28:])),
		Width:                   int(le.Uint32(data[32:])),
		Height:                  int(le.Uint32(data[36:])),
		Planes:                  int(le.Uint32(data[40:])),
	}, nil
}
