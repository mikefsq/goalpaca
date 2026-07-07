package server

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"math"
	"net/http"
	"strconv"
)

// This file implements the plain-JSON ImageArray transport — the mandatory
// baseline every Alpaca client can consume. ImageBytes (imagebytes.go) is the
// negotiated optimization; a client that doesn't send
// Accept: application/imagebytes gets this form instead. The response is
// streamed (the Value of a 62 MP frame runs to hundreds of MB as JSON text),
// never marshalled in one buffer.

// jsonImageType is the ImageArray envelope's Type field
// (ImageArrayElementTypes): 0=Unknown, 1=Int16, 2=Int32, 3=Double. The richer
// ImageBytes element types collapse onto these: integer types up to 32 bits
// present as Int32, everything wider or floating-point as Double.
func jsonImageType(t ImageElementType) int {
	switch t {
	case ImgInt16:
		return 1
	case ImgByte, ImgUInt16, ImgInt32:
		return 2
	case ImgUInt32, ImgInt64, ImgUInt64, ImgSingle, ImgDouble:
		return 3
	}
	return 0
}

// appendJSONElem appends pixel element i of pix (little-endian, type t) as a
// JSON number.
func appendJSONElem(dst, pix []byte, i int, t ImageElementType) []byte {
	le := binary.LittleEndian
	switch t {
	case ImgByte:
		return strconv.AppendUint(dst, uint64(pix[i]), 10)
	case ImgInt16:
		return strconv.AppendInt(dst, int64(int16(le.Uint16(pix[i*2:]))), 10)
	case ImgUInt16:
		return strconv.AppendUint(dst, uint64(le.Uint16(pix[i*2:])), 10)
	case ImgInt32:
		return strconv.AppendInt(dst, int64(int32(le.Uint32(pix[i*4:]))), 10)
	case ImgUInt32:
		return strconv.AppendUint(dst, uint64(le.Uint32(pix[i*4:])), 10)
	case ImgInt64:
		return strconv.AppendInt(dst, int64(le.Uint64(pix[i*8:])), 10)
	case ImgUInt64:
		return strconv.AppendUint(dst, le.Uint64(pix[i*8:]), 10)
	case ImgSingle:
		f := float64(math.Float32frombits(le.Uint32(pix[i*4:])))
		return strconv.AppendFloat(dst, f, 'g', -1, 32)
	case ImgDouble:
		return strconv.AppendFloat(dst, math.Float64frombits(le.Uint64(pix[i*8:])), 'g', -1, 64)
	}
	return append(dst, '0')
}

// writeImageArrayJSON streams the standard JSON ImageArray envelope:
// {"Type":…,"Rank":…,"Value":[[…]],…}. Value follows the ASCOM wire
// convention shared with ImageBytes — a [Width][Height] array with the second
// (Y) index varying fastest — produced here by indexing the driver's
// row-major Pixels on the fly (Value[x][y] = Pixels[y*Width+x]), so no
// transposed copy of the frame is ever built. Rank-3 pixels are already in
// wire order [Width][Height][Planes].
func writeImageArrayJSON(w http.ResponseWriter, frame ImageFrame, clientTxID, serverTxID uint32) {
	transmit := frame.TransmissionElementType
	if transmit == 0 {
		transmit = frame.ElementType
	}
	es := elemBytes(transmit)
	planes := frame.Planes
	if frame.Rank == 2 {
		planes = 1
	}
	if es == 0 || frame.Width <= 0 || frame.Height <= 0 || planes <= 0 ||
		(frame.Rank != 2 && frame.Rank != 3) ||
		len(frame.Pixels) != frame.Width*frame.Height*planes*es {
		writeValue(w, nil, NewError(ErrNumUnspecified,
			fmt.Sprintf("malformed image frame: rank %d, %dx%dx%d, %d pixel bytes",
				frame.Rank, frame.Width, frame.Height, frame.Planes, len(frame.Pixels))),
			clientTxID, serverTxID)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	bw := bufio.NewWriterSize(w, 1<<16)
	// Per-element scratch: number text plus a separator; appended and flushed
	// through bw so peak memory stays at the buffer size, not the body size.
	scratch := make([]byte, 0, 32)

	fmt.Fprintf(bw, `{"Type":%d,"Rank":%d,"Value":[`, jsonImageType(frame.ElementType), frame.Rank)
	for x := 0; x < frame.Width; x++ {
		if x > 0 {
			bw.WriteByte(',')
		}
		bw.WriteByte('[')
		for y := 0; y < frame.Height; y++ {
			if y > 0 {
				bw.WriteByte(',')
			}
			if frame.Rank == 2 {
				scratch = appendJSONElem(scratch[:0], frame.Pixels, y*frame.Width+x, transmit)
				bw.Write(scratch)
				continue
			}
			bw.WriteByte('[')
			for p := 0; p < planes; p++ {
				if p > 0 {
					bw.WriteByte(',')
				}
				scratch = appendJSONElem(scratch[:0], frame.Pixels, (x*frame.Height+y)*planes+p, transmit)
				bw.Write(scratch)
			}
			bw.WriteByte(']')
		}
		bw.WriteByte(']')
	}
	fmt.Fprintf(bw, `],"ClientTransactionID":%d,"ServerTransactionID":%d,"ErrorNumber":0,"ErrorMessage":""}`,
		clientTxID, serverTxID)
	_ = bw.Flush()
}
