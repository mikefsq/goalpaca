package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
)

// serverTxID is the monotonic per-server transaction counter.
type serverTxCounter struct{ n uint32 }

func (s *serverTxCounter) next() uint32 { return atomic.AddUint32(&s.n, 1) }

// valueResponse is the Alpaca envelope for a property GET (carries Value).
type valueResponse struct {
	Value               any    `json:"Value"`
	ClientTransactionID uint32 `json:"ClientTransactionID"`
	ServerTransactionID uint32 `json:"ServerTransactionID"`
	ErrorNumber         int    `json:"ErrorNumber"`
	ErrorMessage        string `json:"ErrorMessage"`
}

// methodResponse is the Alpaca envelope for a PUT (method/property-set); no Value.
type methodResponse struct {
	ClientTransactionID uint32 `json:"ClientTransactionID"`
	ServerTransactionID uint32 `json:"ServerTransactionID"`
	ErrorNumber         int    `json:"ErrorNumber"`
	ErrorMessage        string `json:"ErrorMessage"`
}

// params holds request parameters (merged query + form), indexed two ways: by
// original casing (vals) and by lowercase (lower). ClientID/ClientTransactionID
// are always resolved case-insensitively — real Alpaca clients and ConformU's
// own GET-parameter checks both require it — regardless of strict. Only PUT's
// device-specific parameters honor strict; see Config.StrictParamCasing.
type params struct {
	vals                map[string]string
	lower               map[string]string
	strict              bool
	clientID            uint32
	clientTransactionID uint32
}

// parseParams reads the merged query + form parameters from r. strict selects
// exact-case matching for PUT's device-specific parameters (Config.
// StrictParamCasing); it never applies to GET parameters or to
// ClientID/ClientTransactionID, both of which ConformU itself expects to be
// recognized regardless of casing.
func parseParams(r *http.Request, strict bool) params {
	p := params{
		vals:   map[string]string{},
		lower:  map[string]string{},
		strict: strict && r.Method == http.MethodPut,
	}
	// ParseForm merges URL query and (for PUT) the x-www-form-urlencoded body.
	_ = r.ParseForm()
	for k, v := range r.Form {
		if len(v) > 0 {
			p.vals[k] = v[0]
			p.lower[strings.ToLower(k)] = v[0]
		}
	}
	if v, ok := p.lower["clientid"]; ok {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			p.clientID = uint32(n)
		}
	}
	if v, ok := p.lower["clienttransactionid"]; ok {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			p.clientTransactionID = uint32(n)
		}
	}
	return p
}

// get looks up a parameter by its canonical ASCOM name (e.g. "DeclinationRate").
// In the default mode, and always for GET, the match is case-insensitive; for
// a PUT request with Config.StrictParamCasing set, name must match the wire
// parameter exactly.
func (p params) get(name string) (string, bool) {
	if p.strict {
		v, ok := p.vals[name]
		return v, ok
	}
	v, ok := p.lower[strings.ToLower(name)]
	return v, ok
}

// badRequestError marks a protocol-level bad request: a required parameter is
// missing or cannot be parsed. The HTTP layer renders it as 400 text/plain per
// the Alpaca spec (components/responses/400), distinct from a device-level
// InvalidValue (0x401) — a value the device rejects — which is returned in-band
// with HTTP 200.
type badRequestError struct{ msg string }

func (e *badRequestError) Error() string { return e.msg }

func missingParam(name string) error {
	return &badRequestError{"missing or empty required parameter: " + name}
}
func badParam(name, val string) error {
	return &badRequestError{"invalid value for parameter " + name + ": " + val}
}

// reqInt/reqFloat/reqBool read a required parameter. A missing or unparseable
// value is a protocol-level bad request (HTTP 400 via badRequestError), NOT an
// in-band 0x401 — the latter is reserved for values the device itself rejects.
func (p params) reqInt(name string) (int, error) {
	v, ok := p.get(name)
	if !ok {
		return 0, missingParam(name)
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, badParam(name, v)
	}
	return n, nil
}

func (p params) reqFloat(name string) (float64, error) {
	v, ok := p.get(name)
	if !ok {
		return 0, missingParam(name)
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, badParam(name, v)
	}
	return f, nil
}

func (p params) reqBool(name string) (bool, error) {
	v, ok := p.get(name)
	if !ok {
		return false, missingParam(name)
	}
	// Alpaca booleans are case-insensitive "true"/"false".
	switch strings.ToLower(v) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, badParam(name, v)
}

// reqString reads a required string parameter. A missing value is a
// protocol-level bad request (HTTP 400), matching reqInt/reqFloat/reqBool;
// an empty-but-present value is left for the driver/caller to validate.
func (p params) reqString(name string) (string, error) {
	v, ok := p.get(name)
	if !ok {
		return "", missingParam(name)
	}
	return v, nil
}

// writeBadRequest renders a protocol-level bad request as HTTP 400 text/plain
// (Alpaca components/responses/400) and reports whether it handled the error.
func writeBadRequest(w http.ResponseWriter, err error) bool {
	var bre *badRequestError
	if errors.As(err, &bre) {
		http.Error(w, bre.msg, http.StatusBadRequest) // http.Error sets text/plain
		return true
	}
	return false
}

// writeValue emits a property-GET envelope. A device-level error maps to an
// in-band ErrorNumber (HTTP 200) with the Value field OMITTED — the typed Value
// has a concrete schema type, so emitting null would violate it. A missing or
// unparseable parameter is a 400 instead (writeBadRequest).
func writeValue(w http.ResponseWriter, value any, err error, clientTxID, serverTxID uint32) {
	if writeBadRequest(w, err) {
		return
	}
	if err != nil {
		num, msg := ErrorNumberFor(err)
		writeJSON(w, methodResponse{ // Value omitted on error
			ClientTransactionID: clientTxID,
			ServerTransactionID: serverTxID,
			ErrorNumber:         num,
			ErrorMessage:        msg,
		})
		return
	}
	writeJSON(w, valueResponse{
		Value:               value,
		ClientTransactionID: clientTxID,
		ServerTransactionID: serverTxID,
		ErrorNumber:         0,
		ErrorMessage:        "",
	})
}

// writeMethod emits a PUT (method/property-set) envelope.
func writeMethod(w http.ResponseWriter, err error, clientTxID, serverTxID uint32) {
	if writeBadRequest(w, err) {
		return
	}
	num, msg := ErrorNumberFor(err)
	writeJSON(w, methodResponse{
		ClientTransactionID: clientTxID,
		ServerTransactionID: serverTxID,
		ErrorNumber:         num,
		ErrorMessage:        msg,
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
