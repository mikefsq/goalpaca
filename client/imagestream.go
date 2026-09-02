package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/mikefsq/goalpaca/alpaca"
)

// Streaming image download — the ImageBytes body decoded as it arrives, into ONE destination.
//
// # Why this exists beside ImageArrayCtx
//
// ImageArrayCtx has to hold the whole payload before it can return anything, because
// alpaca.DecodeImageBytes takes it by value. Counting the large buffers alive at once on that
// path, for a 122 MB frame off a 60 MP sensor:
//
//	io.ReadAll's growing buffer   ~2x transient, doubling from 512 bytes
//	the transpose's output        122 MB
//	the caller's own widening     122 MB (uint16 samples, in the consumer)
//
// The peak is well over 350 MB to deliver 122 MB of pixels, and the growth churn alone copies the
// body twice. On a Raspberry Pi that is the difference between a working capture and an OOM.
//
// This path reads the 44-byte metadata header first, hands it to the sink so the sink can allocate
// its final buffer ONCE, and then streams the body through a small reusable scratch. Nothing
// full-size is allocated here at all: the only large allocation in the whole transfer belongs to
// the sink, and it is the buffer the caller wanted anyway.
//
// It is the same shape goindi's BLOB path already uses for INDI (client.FITSWriter), which is what
// makes the two transports comparable rather than one being structurally worse than the other.
//
// # It is not zero-copy, and it cannot be
//
// The bytes cross a socket, so they are copied into user space once no matter what. And ImageBytes
// is COLUMN-MAJOR on the wire, so a row-major consumer must transpose, which is a second pass by
// definition. Reinterpreting the body as []uint16 would satisfy alignment and endianness on a
// little-endian host and still be wrong for that reason. "One destination, filled once" is the
// achievable target, and it is what this does.

// ImageMeta is the ImageBytes metadata header, handed to a sink before any pixels.
type ImageMeta = alpaca.ImageBytesHeader

// ImageSink receives a streamed image. Header is called exactly once, before any Write; Write is
// called with the raw wire bytes in arbitrary chunk sizes, so a sink must carry a partial element
// across calls; Close is called once at the end, including when the transfer failed part-way
// (with the error reported through ImageArrayInto's return, not through Close).
//
// The bytes handed to Write are COLUMN-MAJOR, in Header's Transmit() element type. That is the wire
// order, not the presentation order — a sink that wants row-major pixels transposes as it writes.
type ImageSink interface {
	Header(ImageMeta) error
	Write(p []byte) (int, error)
	Close() error
}

// streamScratch is the default staging buffer for the body copy. Big enough that the per-read
// overhead is amortized, small enough to stay resident in L2 so the sink's conversion reads bytes
// that are already in cache — the same reasoning that makes goindi's row-at-a-time FITS decode
// cheap.
const streamScratch = 256 << 10

// ChunkSizer is an optional ImageSink that chooses its own staging-buffer size.
//
// It exists because the right size is the SINK's business, not the transport's, and the two want
// opposite things. A sink converting on the calling goroutine wants small chunks that stay in L2.
// A sink that fans its conversion out across cores wants chunks big enough to be worth the
// dispatch — measured against a 61 MP frame, a 256 KB chunk leaves a parallel sink with 4 KB per
// worker, which costs more in scheduling than it saves in arithmetic.
//
// Returning zero or less selects streamScratch. The buffer is reused for the whole transfer, so a
// large value here is one allocation, not one per read.
type ChunkSizer interface {
	ChunkSize() int
}

// ImageArrayInto downloads the latest frame and streams it into sink, holding no full-size buffer
// of its own. It requires the server to honour the ImageBytes Accept negotiation: the JSON
// ImageArray fallback cannot be streamed (its samples are a JSON array, not a pixel buffer), so a
// server that answers with JSON is reported as an error rather than silently buffered — a caller
// that reached for this asked not to hold the frame twice.
//
// Cancelling ctx tears down the in-flight transfer, as with ImageArrayCtx.
func (c *Camera) ImageArrayInto(ctx context.Context, sink ImageSink) error {
	return c.getImageStream(ctx, "imagearray", sink)
}

func (d *Device) getImageStream(ctx context.Context, member string, sink ImageSink) (err error) {
	// A server already known not to speak ImageBytes is refused WITHOUT a request, so the caller's
	// fallback costs nothing after the first frame. See Device.noStream.
	if d.noStream != nil && d.noStream.Load() {
		return ErrUnstreamable
	}
	req, err := d.prepare(http.MethodGet, member, nil)
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)
	req.Header.Set("Accept", alpaca.ImageBytesMIME)
	resp, err := d.imageHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Small by construction (an error body), so reading it whole is safe.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return &RequestError{Status: resp.StatusCode, Message: strings.TrimSpace(string(body))}
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), alpaca.ImageBytesMIME) {
		err := d.streamRefusedJSON(resp.Body)
		// Latch only the "this server speaks JSON" answer. A device error that happened to arrive
		// in a JSON envelope says nothing about the transport, and latching on it would demote a
		// working ImageBytes server for the rest of the session over one disconnected camera.
		if d.noStream != nil && errors.Is(err, ErrUnstreamable) {
			d.noStream.Store(true)
		}
		return err
	}

	hdrBuf := make([]byte, alpaca.ImageBytesHeaderLen)
	if _, err := io.ReadFull(resp.Body, hdrBuf); err != nil {
		return fmt.Errorf("alpaca: imagebytes header: %w", err)
	}
	hdr, err := alpaca.ParseImageBytesHeader(hdrBuf)
	if err != nil {
		return err
	}
	if hdr.ErrorNumber != 0 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return &alpaca.AlpacaError{Number: hdr.ErrorNumber, Message: string(msg)}
	}
	// A DataStart beyond the fixed header means the server inserted padding this codec does not
	// know about. Skipping it is right and cheap; treating it as pixels would shift every sample.
	if skip := hdr.DataStart - alpaca.ImageBytesHeaderLen; skip > 0 {
		if _, err := io.CopyN(io.Discard, resp.Body, int64(skip)); err != nil {
			return fmt.Errorf("alpaca: imagebytes padding: %w", err)
		}
	}

	if err := sink.Header(hdr); err != nil {
		sink.Close()
		return err
	}
	// Close runs on every exit, so a sink always learns the transfer ended. Its error is reported
	// only when the transfer itself succeeded — a truncation should be blamed on the truncation,
	// not on the incomplete-payload complaint it provokes downstream.
	defer func() {
		cerr := sink.Close()
		if err == nil {
			err = cerr
		}
	}()

	chunk := streamScratch
	if cs, ok := sink.(ChunkSizer); ok {
		if n := cs.ChunkSize(); n > 0 {
			chunk = n
		}
	}
	buf := make([]byte, chunk)
	if _, err := io.CopyBuffer(writerOnly{sink}, resp.Body, buf); err != nil {
		return fmt.Errorf("alpaca: imagebytes body: %w", err)
	}
	return nil
}

// writerOnly hides any ReadFrom/WriteTo the sink may have, so io.CopyBuffer is guaranteed to use
// the scratch buffer rather than quietly choosing a path that allocates its own.
type writerOnly struct{ w io.Writer }

func (w writerOnly) Write(p []byte) (int, error) { return w.w.Write(p) }

// streamRefusedJSON turns a server that ignored the Accept negotiation into a useful error. The
// device's own error envelope is preferred when there is one, because "the server sent JSON" is
// not the operator's problem when the real answer is "camera is not connected".
func (d *Device) streamRefusedJSON(body io.Reader) error {
	raw, _ := io.ReadAll(io.LimitReader(body, 1<<20))
	var env struct {
		response
	}
	if err := json.Unmarshal(raw, &env); err == nil && env.ErrorNumber != 0 {
		return &alpaca.AlpacaError{Number: env.ErrorNumber, Message: env.ErrorMessage}
	}
	return fmt.Errorf("%w: server answered imagearray as JSON, streaming needs %s",
		ErrUnstreamable, alpaca.ImageBytesMIME)
}

// ErrUnstreamable reports a response that cannot be streamed and must be fetched with
// ImageArrayCtx instead — today, a server that ignored the ImageBytes Accept negotiation and
// answered with the JSON ImageArray form, whose samples are a JSON array rather than a pixel
// buffer.
//
// It is a distinct error because it is not a failure: the frame is there and is retrievable, just
// not this way. A caller that treats every error from ImageArrayInto as a lost exposure would drop
// frames from a perfectly working server that simply speaks the older form.
var ErrUnstreamable = errors.New("alpaca: response cannot be streamed")

// IsUnstreamable reports whether err means "re-fetch with ImageArrayCtx".
func IsUnstreamable(err error) bool { return errors.Is(err, ErrUnstreamable) }
