// Package client is a typed Go client for ASCOM Alpaca devices (HTTP/JSON REST
// + UDP discovery). It mirrors the goalpaca server library: a base Device plus
// one typed client per device type (Camera, Focuser, …), constructed with an
// address and device number. The request plumbing — ClientID, an
// auto-incrementing ClientTransactionID, URL building, and mapping the response
// to a value or a typed error — is handled here.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mikefsq/goalpaca/alpaca"
)

const defaultTimeout = 30 * time.Second

// maxEnvelopeBytes caps how much of a JSON-envelope response the client will
// read (Value payloads here are scalars, strings, and short lists). A buggy or
// hostile server can't stream gigabytes into memory. Image bodies use
// maxImageBytes instead.
const maxEnvelopeBytes = 8 << 20

// maxImageBytes caps an imagearray body: the binary ImageBytes form of a
// large frame is >100 MB and its JSON text form several times that.
const maxImageBytes = 2 << 30

// response is the Alpaca JSON envelope as seen by the client. Value is captured
// raw so typed getters decode it into the concrete Go type.
type response struct {
	Value               json.RawMessage `json:"Value"`
	ClientTransactionID uint32          `json:"ClientTransactionID"`
	ServerTransactionID uint32          `json:"ServerTransactionID"`
	ErrorNumber         int             `json:"ErrorNumber"`
	ErrorMessage        string          `json:"ErrorMessage"`
}

// RequestError is returned when the device rejects the request at the HTTP level
// (e.g. 400 for a malformed/missing parameter, or any non-200 status). A
// device-level ASCOM fault is returned as *alpaca.AlpacaError instead (carried
// in-band via the response ErrorNumber).
type RequestError struct {
	Status  int
	Message string
}

// Error implements the error interface.
func (e *RequestError) Error() string {
	return fmt.Sprintf("alpaca request failed: HTTP %d: %s", e.Status, e.Message)
}

// Option configures a device client.
type Option func(*Device)

// WithClientID sets the ClientID sent on every request (default: random 1–65535).
func WithClientID(id uint32) Option { return func(d *Device) { d.clientID = id } }

// WithHTTPClient supplies a custom *http.Client (transport, TLS, proxies, …).
// It is used for every request, including image downloads — set its Timeout
// with care (or leave it zero) so large ImageArray transfers aren't cut off.
// Composes with WithTimeout in either order.
func WithHTTPClient(c *http.Client) Option { return func(d *Device) { d.customHTTP = c } }

// WithTimeout sets the per-request timeout for JSON-envelope calls
// (default 30s). It does NOT bound image downloads: an ImageArray body can be
// >100 MB and outlive any fixed cap on a slow link — use ImageArrayCtx to
// bound or cancel those. Composes with WithHTTPClient in either order.
func WithTimeout(t time.Duration) Option {
	return func(d *Device) { d.timeout = t }
}

// Device is the common base embedded by every typed client. It carries the
// connection target and performs the Alpaca request/response transaction.
type Device struct {
	baseURL      string
	deviceType   alpaca.DeviceType
	deviceNumber int
	clientID     uint32
	txCounter    uint32

	// customHTTP/timeout capture the options; initHTTP resolves them into the
	// two clients actually used (so option order never matters).
	customHTTP *http.Client
	timeout    time.Duration
	http       *http.Client // JSON-envelope calls: overall per-request timeout
	imageHTTP  *http.Client // image downloads: connection-level limits only

	// noStream latches a server that ignores the ImageBytes Accept negotiation, so ImageArrayInto
	// stops attempting a streamed download against it.
	//
	// Without it the penalty recurs on EVERY FRAME: a request that is answered with JSON, up to a
	// megabyte of that JSON read to look for an error envelope, and then the whole image fetched a
	// second time through ImageArrayCtx. One wasted request per device is a discovery; one per
	// frame is a regression, and it would land on exactly the older servers least able to afford it.
	//
	// A pointer because Device is returned by value from newDevice and atomic.Bool must not be
	// copied. Never cleared: a server does not learn to speak ImageBytes mid-session, and re-probing
	// would reintroduce the per-frame cost this exists to remove.
	noStream *atomic.Bool
}

func newDevice(address string, dt alpaca.DeviceType, number int, opts ...Option) Device {
	d := Device{
		baseURL:      normalizeBaseURL(address),
		deviceType:   dt,
		deviceNumber: number,
		clientID:     rand.Uint32()%65535 + 1,
		noStream:     new(atomic.Bool),
	}
	for _, o := range opts {
		o(&d)
	}
	d.initHTTP()
	return d
}

// initHTTP resolves the HTTP options into the envelope and image clients. The
// default envelope client bounds whole requests at the configured timeout;
// the default image client has no overall cap (a >100 MB frame on a slow link
// legitimately outlives any fixed timeout) but inherits the transport's
// connect/TLS limits plus a response-header bound, so a dead server still
// fails fast — only an in-progress body transfer is unbounded, and
// ImageArrayCtx lets the caller bound that.
func (d *Device) initHTTP() {
	timeout := d.timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	if d.customHTTP != nil {
		d.http = d.customHTTP
		if d.timeout != 0 && d.customHTTP.Timeout != d.timeout {
			c := *d.customHTTP // shallow copy: same transport, different timeout
			c.Timeout = d.timeout
			d.http = &c
		}
		d.imageHTTP = d.customHTTP // the user's client governs image transfers as supplied
		return
	}
	tr := http.RoundTripper(http.DefaultTransport)
	if t, ok := tr.(*http.Transport); ok {
		t2 := t.Clone()
		t2.ResponseHeaderTimeout = timeout
		tr = t2
	}
	d.http = &http.Client{Timeout: timeout, Transport: tr}
	d.imageHTTP = &http.Client{Transport: tr}
}

// BaseURL returns the server base URL this client targets.
func (d *Device) BaseURL() string { return d.baseURL }

// DeviceType returns the target's Alpaca device type.
func (d *Device) DeviceType() alpaca.DeviceType { return d.deviceType }

// DeviceNumber returns the target's per-type device number.
func (d *Device) DeviceNumber() int { return d.deviceNumber }

// ClientID returns the ClientID sent on every request.
func (d *Device) ClientID() uint32 { return d.clientID }

func normalizeBaseURL(address string) string {
	a := strings.TrimSpace(address)
	if !strings.Contains(a, "://") {
		a = "http://" + urlAuthority(a)
	}
	return strings.TrimRight(a, "/")
}

// urlAuthority converts a dialable host:port — which may carry a raw IPv6 zone such as
// "[fe80::1%eth0]:11111" — into an authority safe to embed in a URL. RFC 6874 requires the
// zone delimiter '%' to be written as "%25"; a raw "%zone" is otherwise rejected by the URL
// parser as an invalid escape, so link-local IPv6 servers (the ones Alpaca IPv6 discovery
// finds) can't be reached. Addresses without a zone pass through unchanged.
//
// The zone body is percent-encoded too, not just the delimiter: RFC 6874 admits only
// unreserved characters and pct-encoded octets in a ZoneID, and a zone is an interface
// identifier whose form is platform-dependent. On Linux/BSD it is a short name ("eth0")
// that needs no encoding, but on Windows it is the interface name — routinely with spaces,
// e.g. "Ethernet Instance 0" — which the URL parser rejects outright ('invalid character
// " " in host name'). Encoding the body keeps link-local discovery working there.
func urlAuthority(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr // not host:port — leave as-is
	}
	if i := strings.IndexByte(host, '%'); i >= 0 {
		host = host[:i] + "%25" + escapeZoneID(host[i+1:])
	}
	return net.JoinHostPort(host, port)
}

// URLAuthority is the exported form of urlAuthority, for callers outside this package that
// build their own URLs from a dialable address — notably a discovered server's Address, which
// carries an IPv6 zone verbatim from the OS. Embedding such an address in a URL without this
// yields "invalid URL escape" (or, for a Windows zone, "invalid character in host name").
func URLAuthority(addr string) string { return urlAuthority(addr) }

// escapeZoneID percent-encodes an IPv6 zone identifier per RFC 6874, which allows only
// unreserved characters (ALPHA / DIGIT / "-" / "." / "_" / "~") to appear literally.
// Everything else — notably the spaces in a Windows interface name — becomes %XX.
func escapeZoneID(zone string) string {
	var b strings.Builder
	for i := 0; i < len(zone); i++ { // byte-wise: a multi-byte rune encodes as its octets
		switch c := zone[i]; {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '.', c == '_', c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// prepare builds an Alpaca request with ClientID + an incrementing
// ClientTransactionID injected (GET as query string, PUT as a form body).
func (d *Device) prepare(method, member string, params url.Values) (*http.Request, error) {
	if params == nil {
		params = url.Values{}
	}
	params.Set("ClientID", strconv.FormatUint(uint64(d.clientID), 10))
	params.Set("ClientTransactionID", strconv.FormatUint(uint64(atomic.AddUint32(&d.txCounter, 1)), 10))

	endpoint := fmt.Sprintf("%s/api/v1/%s/%d/%s", d.baseURL, d.deviceType, d.deviceNumber, strings.ToLower(member))
	if method == http.MethodGet {
		return http.NewRequest(method, endpoint+"?"+params.Encode(), nil)
	}
	req, err := http.NewRequest(method, endpoint, strings.NewReader(params.Encode()))
	if err == nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return req, err
}

// call performs one Alpaca transaction and decodes the JSON response: a non-200
// status becomes *RequestError; a non-zero ErrorNumber becomes
// *alpaca.AlpacaError; otherwise Value is decoded into out.
func (d *Device) call(method, member string, params url.Values, out any) error {
	req, err := d.prepare(method, member, params)
	if err != nil {
		return err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := readCapped(resp.Body, maxEnvelopeBytes)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return &RequestError{Status: resp.StatusCode, Message: strings.TrimSpace(string(body))}
	}

	var env response
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("alpaca: decode response: %w", err)
	}
	if env.ErrorNumber != 0 {
		return &alpaca.AlpacaError{Number: env.ErrorNumber, Message: env.ErrorMessage}
	}
	if out != nil {
		// A successful GET must carry a Value; decoding a missing/null Value
		// into Go zero values would silently fabricate data (Connected()
		// confidently false, Gain() 0) from a noncompliant response.
		if len(env.Value) == 0 || string(env.Value) == "null" {
			return fmt.Errorf("alpaca: response for %s has no Value", member)
		}
		if err := json.Unmarshal(env.Value, out); err != nil {
			return fmt.Errorf("alpaca: decode value: %w", err)
		}
	}
	return nil
}

// Typed GET helpers. Each calls then returns, so the decoded value is observed
// after call mutates it.
func (d *Device) getBool(member string) (bool, error) {
	var v bool
	err := d.call(http.MethodGet, member, nil, &v)
	return v, err
}

func (d *Device) getInt(member string) (int, error) {
	var v int
	err := d.call(http.MethodGet, member, nil, &v)
	return v, err
}

func (d *Device) getFloat(member string) (float64, error) {
	var v float64
	err := d.call(http.MethodGet, member, nil, &v)
	return v, err
}

func (d *Device) getString(member string) (string, error) {
	var v string
	err := d.call(http.MethodGet, member, nil, &v)
	return v, err
}

func (d *Device) getStringList(member string) ([]string, error) {
	var v []string
	err := d.call(http.MethodGet, member, nil, &v)
	return v, err
}

// getInto runs a GET with parameters, decoding Value into out.
func (d *Device) getInto(member string, params url.Values, out any) error {
	return d.call(http.MethodGet, member, params, out)
}

// put runs a PUT (method / property-set) with no returned value.
func (d *Device) put(member string, params url.Values) error {
	return d.call(http.MethodPut, member, params, nil)
}

// getImageBytes performs a GET requesting the binary ImageBytes transport and
// decodes the result into an ImageFrame. A device error carried in the
// ImageBytes envelope (or a JSON error envelope) becomes *alpaca.AlpacaError.
func (d *Device) getImageBytes(member string) (alpaca.ImageFrame, error) {
	return d.getImageBytesCtx(context.Background(), member)
}

// getImageBytesCtx is getImageBytes with a caller context: cancelling ctx aborts the in-flight
// HTTP transfer (the large ImageBytes body read), so an aborted capture tears the download down
// instead of orphaning it until the client timeout.
func (d *Device) getImageBytesCtx(ctx context.Context, member string) (alpaca.ImageFrame, error) {
	req, err := d.prepare(http.MethodGet, member, nil)
	if err != nil {
		return alpaca.ImageFrame{}, err
	}
	req = req.WithContext(ctx)
	req.Header.Set("Accept", alpaca.ImageBytesMIME)
	resp, err := d.imageHTTP.Do(req)
	if err != nil {
		return alpaca.ImageFrame{}, err
	}
	defer resp.Body.Close()
	body, err := readCapped(resp.Body, maxImageBytes)
	if err != nil {
		return alpaca.ImageFrame{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return alpaca.ImageFrame{}, &RequestError{Status: resp.StatusCode, Message: strings.TrimSpace(string(body))}
	}
	if strings.Contains(resp.Header.Get("Content-Type"), alpaca.ImageBytesMIME) {
		return alpaca.DecodeImageBytes(body)
	}
	// A server is free to ignore the Accept negotiation and answer with the
	// standard JSON form instead: either an error envelope or the baseline
	// JSON ImageArray (Type/Rank/Value).
	var env struct {
		response
		Type int `json:"Type"`
		Rank int `json:"Rank"`
	}
	if err := json.Unmarshal(body, &env); err == nil {
		if env.ErrorNumber != 0 {
			return alpaca.ImageFrame{}, &alpaca.AlpacaError{Number: env.ErrorNumber, Message: env.ErrorMessage}
		}
		if len(env.Value) > 0 && env.Value[0] == '[' {
			return decodeImageArrayJSON(env.Value, env.Type, env.Rank)
		}
	}
	return alpaca.ImageFrame{}, fmt.Errorf("alpaca: unexpected imagearray content-type %q", resp.Header.Get("Content-Type"))
}

// readCapped reads r to EOF up to max bytes, erroring (instead of truncating)
// on an oversized body.
func readCapped(r io.Reader, max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("alpaca: response body exceeds %d bytes", max)
	}
	return b, nil
}

// boolParam renders a Go bool as the Alpaca form value.
func boolParam(v bool) string {
	if v {
		return "True"
	}
	return "False"
}

func intParam(n int) string       { return strconv.Itoa(n) }
func floatParam(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }
