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

// ImageMeta is the ImageBytes metadata header, handed to a sink before any pixels.
type ImageMeta = alpaca.ImageBytesHeader

// ImageSink receives a header followed by column-major bytes in the header's
// Transmit element type. Write chunks may split elements. Row-major consumers
// must transpose the pixels.
//
// Once Header is called, Close runs exactly once, including on Header or Write
// failure. Failures before Header do not call Close. ImageArrayInto returns the
// transfer error, or Close's error if the transfer succeeded.
type ImageSink interface {
	Header(ImageMeta) error
	Write(p []byte) (int, error)
	Close() error
}

// streamScratch is the default reusable transfer buffer size.
const streamScratch = 256 << 10

// ChunkSizer lets an ImageSink select the reusable transfer buffer size.
// Zero or negative values select the default size.
type ChunkSizer interface {
	ChunkSize() int
}

// ImageArrayInto streams ImageBytes into sink without buffering a full frame.
// On ErrUnstreamable, use ImageArrayCtx for the JSON fallback.
// Cancelling ctx ends the transfer.
func (c *Camera) ImageArrayInto(ctx context.Context, sink ImageSink) error {
	return c.getImageStream(ctx, "imagearray", sink)
}

func (d *Device) getImageStream(ctx context.Context, member string, sink ImageSink) (err error) {
	// Cache a refused ImageBytes negotiation to avoid a second download per frame.
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
		// JSON device errors do not imply that ImageBytes is unsupported.
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
	// Skip extension bytes between the fixed header and pixel data.
	if skip := hdr.DataStart - alpaca.ImageBytesHeaderLen; skip > 0 {
		if _, err := io.CopyN(io.Discard, resp.Body, int64(skip)); err != nil {
			return fmt.Errorf("alpaca: imagebytes padding: %w", err)
		}
	}

	if err := sink.Header(hdr); err != nil {
		sink.Close()
		return err
	}
	// Preserve the transfer error when Close also fails.
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

// streamRefusedJSON preserves device errors; otherwise it returns ErrUnstreamable.
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

// ErrUnstreamable means the server did not return ImageBytes.
// The caller can retry with ImageArrayCtx to use the JSON transport.
var ErrUnstreamable = errors.New("alpaca: response cannot be streamed")

// IsUnstreamable reports whether err means "re-fetch with ImageArrayCtx".
func IsUnstreamable(err error) bool { return errors.Is(err, ErrUnstreamable) }
