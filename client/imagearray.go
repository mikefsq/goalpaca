package client

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"

	"github.com/mikefsq/goalpaca/alpaca"
)

// decodeImageArrayJSON decodes the standard JSON ImageArray Value — the
// mandatory baseline transport a server may answer with when it ignores the
// Accept: application/imagebytes negotiation — into an ImageFrame matching
// what DecodeImageBytes produces: rank-2 pixels in row-major sensor order
// (the wire's [Width][Height] Value is un-transposed here), rank-3 pixels in
// wire order [Width][Height][Planes].
//
// jsonType is the envelope's Type field (ImageArrayElementTypes: 1=Int16,
// 2=Int32, 3=Double); rank is the envelope's Rank, or 0 to infer from the
// Value nesting.
func decodeImageArrayJSON(value json.RawMessage, jsonType, rank int) (alpaca.ImageFrame, error) {
	if rank == 0 {
		rank = sniffRank(value)
	}

	var elemType alpaca.ImageElementType
	var es int
	switch jsonType {
	case 1:
		elemType, es = alpaca.ImgInt16, 2
	case 3:
		elemType, es = alpaca.ImgDouble, 8
	default: // 2, or 0/absent from a lax server: Int32 is the spec's default
		elemType, es = alpaca.ImgInt32, 4
	}

	le := binary.LittleEndian
	putElem := func(pix []byte, i int, v float64) {
		switch elemType {
		case alpaca.ImgInt16:
			le.PutUint16(pix[i*2:], uint16(int16(v)))
		case alpaca.ImgDouble:
			le.PutUint64(pix[i*8:], math.Float64bits(v))
		default:
			le.PutUint32(pix[i*4:], uint32(int32(v)))
		}
	}

	switch rank {
	case 2:
		// float64 is exact for every JSON ImageArray element type (Int16,
		// Int32, Double), so a numeric round-trip through it is lossless.
		var v [][]float64
		if err := json.Unmarshal(value, &v); err != nil {
			return alpaca.ImageFrame{}, fmt.Errorf("alpaca: imagearray JSON rank-2 decode: %w", err)
		}
		width := len(v)
		if width == 0 || len(v[0]) == 0 {
			return alpaca.ImageFrame{}, fmt.Errorf("alpaca: imagearray JSON has empty dimensions")
		}
		height := len(v[0])
		pix := make([]byte, width*height*es)
		for x, col := range v {
			if len(col) != height {
				return alpaca.ImageFrame{}, fmt.Errorf("alpaca: imagearray JSON is ragged: column %d has %d rows, want %d", x, len(col), height)
			}
			for y, val := range col {
				putElem(pix, y*width+x, val) // wire [x][y] → row-major
			}
		}
		return alpaca.ImageFrame{
			Rank: 2, Width: width, Height: height,
			ElementType:             elemType,
			TransmissionElementType: elemType,
			Pixels:                  pix,
		}, nil

	case 3:
		var v [][][]float64
		if err := json.Unmarshal(value, &v); err != nil {
			return alpaca.ImageFrame{}, fmt.Errorf("alpaca: imagearray JSON rank-3 decode: %w", err)
		}
		width := len(v)
		if width == 0 || len(v[0]) == 0 || len(v[0][0]) == 0 {
			return alpaca.ImageFrame{}, fmt.Errorf("alpaca: imagearray JSON has empty dimensions")
		}
		height, planes := len(v[0]), len(v[0][0])
		pix := make([]byte, width*height*planes*es)
		for x, col := range v {
			if len(col) != height {
				return alpaca.ImageFrame{}, fmt.Errorf("alpaca: imagearray JSON is ragged: column %d has %d rows, want %d", x, len(col), height)
			}
			for y, cell := range col {
				if len(cell) != planes {
					return alpaca.ImageFrame{}, fmt.Errorf("alpaca: imagearray JSON is ragged: cell [%d][%d] has %d planes, want %d", x, y, len(cell), planes)
				}
				for p, val := range cell {
					putElem(pix, (x*height+y)*planes+p, val)
				}
			}
		}
		return alpaca.ImageFrame{
			Rank: 3, Width: width, Height: height, Planes: planes,
			ElementType:             elemType,
			TransmissionElementType: elemType,
			Pixels:                  pix,
		}, nil
	}
	return alpaca.ImageFrame{}, fmt.Errorf("alpaca: imagearray JSON rank %d unsupported", rank)
}

// sniffRank counts the leading '[' nesting depth of a JSON array value,
// skipping whitespace — enough to tell rank 2 from rank 3 when a lax server
// omits the envelope's Rank field.
func sniffRank(value json.RawMessage) int {
	depth := 0
	for _, b := range value {
		switch b {
		case ' ', '\t', '\n', '\r':
		case '[':
			depth++
			if depth == 3 {
				return 3
			}
		default:
			return depth
		}
	}
	return depth
}
