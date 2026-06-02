// Package client is a typed Go client for ASCOM Alpaca devices (HTTP/JSON REST
// + UDP discovery). It mirrors the goalpaca server library: a base Device plus
// one typed client per device type (Camera, Focuser, …), constructed with an
// address and device number. The request plumbing — ClientID, an
// auto-incrementing ClientTransactionID, URL building, and mapping the response
// to a value or a typed error — is handled here.
package client

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	alpaca "github.com/mikefsq/goalpaca/server"
)

const defaultTimeout = 30 * time.Second

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

func (e *RequestError) Error() string {
	return fmt.Sprintf("alpaca request failed: HTTP %d: %s", e.Status, e.Message)
}

// Option configures a device client.
type Option func(*Device)

// WithClientID sets the ClientID sent on every request (default: random 1–65535).
func WithClientID(id uint32) Option { return func(d *Device) { d.clientID = id } }

// WithHTTPClient supplies a custom *http.Client (transport, timeout, TLS, …).
func WithHTTPClient(c *http.Client) Option { return func(d *Device) { d.http = c } }

// WithTimeout sets the per-request timeout on the default HTTP client.
func WithTimeout(t time.Duration) Option {
	return func(d *Device) { d.http = &http.Client{Timeout: t} }
}

// Device is the common base embedded by every typed client. It carries the
// connection target and performs the Alpaca request/response transaction.
type Device struct {
	baseURL      string
	deviceType   alpaca.DeviceType
	deviceNumber int
	clientID     uint32
	txCounter    uint32
	http         *http.Client
}

func newDevice(address string, dt alpaca.DeviceType, number int, opts ...Option) Device {
	d := Device{
		baseURL:      normalizeBaseURL(address),
		deviceType:   dt,
		deviceNumber: number,
		clientID:     rand.Uint32()%65535 + 1,
		http:         &http.Client{Timeout: defaultTimeout},
	}
	for _, o := range opts {
		o(&d)
	}
	return d
}

// Target inspection.
func (d *Device) BaseURL() string    { return d.baseURL }
func (d *Device) DeviceType() string { return string(d.deviceType) }
func (d *Device) DeviceNumber() int  { return d.deviceNumber }
func (d *Device) ClientID() uint32   { return d.clientID }

func normalizeBaseURL(address string) string {
	a := strings.TrimSpace(address)
	if !strings.Contains(a, "://") {
		a = "http://" + a
	}
	return strings.TrimRight(a, "/")
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
	body, err := io.ReadAll(resp.Body)
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
	if out != nil && len(env.Value) > 0 {
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
	req, err := d.prepare(http.MethodGet, member, nil)
	if err != nil {
		return alpaca.ImageFrame{}, err
	}
	req.Header.Set("Accept", alpaca.ImageBytesMIME)
	resp, err := d.http.Do(req)
	if err != nil {
		return alpaca.ImageFrame{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return alpaca.ImageFrame{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return alpaca.ImageFrame{}, &RequestError{Status: resp.StatusCode, Message: strings.TrimSpace(string(body))}
	}
	if strings.Contains(resp.Header.Get("Content-Type"), alpaca.ImageBytesMIME) {
		return alpaca.DecodeImageBytes(body)
	}
	// Some servers return a JSON error envelope instead of ImageBytes.
	var env response
	if err := json.Unmarshal(body, &env); err == nil && env.ErrorNumber != 0 {
		return alpaca.ImageFrame{}, &alpaca.AlpacaError{Number: env.ErrorNumber, Message: env.ErrorMessage}
	}
	return alpaca.ImageFrame{}, fmt.Errorf("alpaca: unexpected imagearray content-type %q", resp.Header.Get("Content-Type"))
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
