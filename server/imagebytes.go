package alpacadev

import "encoding/binary"

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
	transmit := frame.TransmissionElementType
	if transmit == 0 {
		transmit = frame.ElementType
	}

	buf := make([]byte, imageBytesMetadataLen+len(frame.Pixels))
	putImageBytesHeader(buf, 0, clientTxID, serverTxID, frame.ElementType, transmit,
		frame.Rank, frame.Width, frame.Height, frame.Planes)
	copy(buf[imageBytesMetadataLen:], frame.Pixels)
	return buf
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
	return ImageFrame{
		ElementType:             ImageElementType(le.Uint32(data[20:])),
		TransmissionElementType: ImageElementType(le.Uint32(data[24:])),
		Rank:                    int(le.Uint32(data[28:])),
		Width:                   int(le.Uint32(data[32:])),
		Height:                  int(le.Uint32(data[36:])),
		Planes:                  int(le.Uint32(data[40:])),
		Pixels:                  append([]byte(nil), data[dataStart:]...),
	}, nil
}
