package alpacadev

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ServeHTTP is the Alpaca handler. With a configured Logger it records one line
// per request (capturing the response status); otherwise it routes directly.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Logger == nil {
		s.route(w, r)
		return
	}
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	start := time.Now()
	s.route(rec, r)
	body := ""
	if r.Method == http.MethodPut && len(r.PostForm) > 0 {
		body = " body=" + r.PostForm.Encode()
	}
	s.cfg.Logger.Printf("%s %s %s -> %d (%s)%s",
		r.RemoteAddr, r.Method, r.URL.RequestURI(), rec.status,
		time.Since(start).Round(time.Millisecond), body)
}

// statusRecorder wraps an http.ResponseWriter to capture the status code for
// request logging (defaults to 200 if the handler never calls WriteHeader).
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// route is the Alpaca router: device API, management, and optional health.
func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case strings.HasPrefix(path, "/management"):
		s.handleManagement(w, r)
	case strings.HasPrefix(path, "/api/v1/"):
		s.handleDeviceAPI(w, r)
	case path == "/health":
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	default:
		http.NotFound(w, r)
	}
}

// handleDeviceAPI serves GET|PUT /api/v1/{device_type}/{device_number}/{member}.
func (s *Server) handleDeviceAPI(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	parts := strings.Split(rest, "/")
	if len(parts) != 3 {
		http.Error(w, "bad device API path", http.StatusBadRequest)
		return
	}
	// Device type must be lowercase on the wire (ASCOM/ConformU): a non-lowercase
	// type won't match any registered (lowercase) type and is rejected below.
	// Member names, by contrast, remain case-insensitive (lowercased further down).
	devType := DeviceType(parts[0])
	number, err := strconv.Atoi(parts[1])
	if err != nil {
		http.Error(w, "bad device number", http.StatusBadRequest)
		return
	}
	member := strings.ToLower(parts[2])

	dev, ok := s.lookup(devType, number)
	if !ok {
		http.Error(w, "no such device", http.StatusBadRequest)
		return
	}

	p := parseParams(r)
	serverTx := s.tx.next()

	switch r.Method {
	case http.MethodGet:
		s.handleGet(w, r, devType, member, dev, p, serverTx)
	case http.MethodPut:
		s.handlePut(w, r, devType, member, dev, p, serverTx)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// connectionExemptMembers are the members that must work even when the device
// is not Connected (introspection + the connection controls themselves). Every
// other member is gated and returns NotConnected (0x407) while disconnected,
// per the ASCOM contract (ConformU checks this).
var connectionExemptMembers = map[string]bool{
	"connected":        true,
	"connect":          true,
	"disconnect":       true,
	"connecting":       true,
	"name":             true,
	"description":      true,
	"driverinfo":       true,
	"driverversion":    true,
	"interfaceversion": true,
	"supportedactions": true,
}

// interruptMembers are mutating PUTs that must work even while the device is in a
// transitory (Busy) state — they are how a client gets the device OUT of that
// state. Everything else mutating is rejected with InvalidOperation while Busy.
var interruptMembers = map[string]bool{
	"abortexposure": true,
	"stopexposure":  true,
	"halt":          true,
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request, devType DeviceType,
	member string, dev Device, p params, serverTx uint32) {

	// NotConnected gating: operational members require a connected session.
	if !connectionExemptMembers[member] && !dev.Connected() {
		writeValue(w, nil, ErrNotConnected, p.clientTransactionID, serverTx)
		return
	}

	// Camera image transport is binary (or an explicit JSON refusal).
	if devType == CameraType && (member == "imagearray" || member == "imagearrayvariant") {
		s.handleImage(w, r, dev, p, serverTx)
		return
	}

	// DeviceState is built per device type (Platform 7 operational set + TimeStamp).
	if member == "devicestate" {
		writeValue(w, stateValueArray(deviceStateValues(devType, dev)), nil, p.clientTransactionID, serverTx)
		return
	}

	// Common Device members first.
	if v, handled, err := deviceGet(member, dev); handled {
		writeValue(w, v, err, p.clientTransactionID, serverTx)
		return
	}

	// Type-specific members.
	if v, handled, err := typeGet(devType, member, dev, p); handled {
		writeValue(w, v, err, p.clientTransactionID, serverTx)
		return
	}

	http.Error(w, "unknown member: "+member, http.StatusBadRequest)
}

// typeGet dispatches a GET to the per-type member table, asserting dev to the
// device-type interface. Returns (value, handled, err).
func typeGet(devType DeviceType, member string, dev Device, p params) (any, bool, error) {
	switch devType {
	case CameraType:
		if x, ok := dev.(Camera); ok {
			return cameraGet(member, x, p)
		}
	case CoverCalibratorType:
		if x, ok := dev.(CoverCalibrator); ok {
			return coverCalibratorGet(member, x, p)
		}
	case DomeType:
		if x, ok := dev.(Dome); ok {
			return domeGet(member, x, p)
		}
	case FilterWheelType:
		if x, ok := dev.(FilterWheel); ok {
			return filterWheelGet(member, x, p)
		}
	case FocuserType:
		if x, ok := dev.(Focuser); ok {
			return focuserGet(member, x, p)
		}
	case ObservingConditionsType:
		if x, ok := dev.(ObservingConditions); ok {
			return observingConditionsGet(member, x, p)
		}
	case RotatorType:
		if x, ok := dev.(Rotator); ok {
			return rotatorGet(member, x, p)
		}
	case SafetyMonitorType:
		if x, ok := dev.(SafetyMonitor); ok {
			return safetyMonitorGet(member, x, p)
		}
	case SwitchType:
		if x, ok := dev.(Switch); ok {
			return switchGet(member, x, p)
		}
	case TelescopeType:
		if x, ok := dev.(Telescope); ok {
			return telescopeGet(member, x, p)
		}
	}
	return nil, false, nil
}

func (s *Server) handlePut(w http.ResponseWriter, r *http.Request, devType DeviceType,
	member string, dev Device, p params, serverTx uint32) {

	// NotConnected gating: operational members require a connected session.
	if !connectionExemptMembers[member] && !dev.Connected() {
		writeMethod(w, ErrNotConnected, p.clientTransactionID, serverTx)
		return
	}

	// Busy gating: while the device is in a transitory state (exposing, moving),
	// reject mutating writes so a second request can't clobber the operation in
	// progress. Reads (GET) are never gated this way; the connection controls and
	// explicit interrupts (abort/stop/halt) are exempt so a client can always
	// disconnect or stop the device.
	if !connectionExemptMembers[member] && !interruptMembers[member] {
		if busy, ok := dev.(Busyable); ok && busy.Busy() {
			writeMethod(w, ErrInvalidOperation, p.clientTransactionID, serverTx)
			return
		}
	}

	// Common PUT members that return a Value (Action, CommandString, CommandBool).
	if v, handled, err := devicePutValue(member, dev, p); handled {
		writeValue(w, v, err, p.clientTransactionID, serverTx)
		return
	}

	// Common void PUT members (Connected, Connect, Disconnect, CommandBlind).
	if handled, err := devicePut(r.Context(), member, dev, p); handled {
		writeMethod(w, err, p.clientTransactionID, serverTx)
		return
	}

	// Type-specific PUT members.
	if handled, err := typePut(devType, member, dev, p); handled {
		writeMethod(w, err, p.clientTransactionID, serverTx)
		return
	}

	http.Error(w, "unknown member: "+member, http.StatusBadRequest)
}

// typePut dispatches a PUT to the per-type member table. All type-specific PUTs
// are void (method/property-set); value-returning PUTs are common (Action,
// CommandString, CommandBool) and handled before this. Returns (handled, err).
func typePut(devType DeviceType, member string, dev Device, p params) (bool, error) {
	switch devType {
	case CameraType:
		if x, ok := dev.(Camera); ok {
			return cameraPut(member, x, p)
		}
	case CoverCalibratorType:
		if x, ok := dev.(CoverCalibrator); ok {
			return coverCalibratorPut(member, x, p)
		}
	case DomeType:
		if x, ok := dev.(Dome); ok {
			return domePut(member, x, p)
		}
	case FilterWheelType:
		if x, ok := dev.(FilterWheel); ok {
			return filterWheelPut(member, x, p)
		}
	case FocuserType:
		if x, ok := dev.(Focuser); ok {
			return focuserPut(member, x, p)
		}
	case ObservingConditionsType:
		if x, ok := dev.(ObservingConditions); ok {
			return observingConditionsPut(member, x, p)
		}
	case RotatorType:
		if x, ok := dev.(Rotator); ok {
			return rotatorPut(member, x, p)
		}
	case SwitchType:
		if x, ok := dev.(Switch); ok {
			return switchPut(member, x, p)
		}
	case TelescopeType:
		if x, ok := dev.(Telescope); ok {
			return telescopePut(member, x, p)
		}
	}
	return false, nil
}

// handleImage serves the camera image. With Accept: application/imagebytes it
// returns the binary ImageBytes transport; otherwise it refuses (ImageArray
// JSON is a last-resort fallback not implemented in this build — see spec §6.4).
func (s *Server) handleImage(w http.ResponseWriter, r *http.Request, dev Device, p params, serverTx uint32) {
	c, ok := dev.(Camera)
	if !ok {
		http.Error(w, "not a camera", http.StatusBadRequest)
		return
	}
	wantBytes := strings.Contains(strings.ToLower(r.Header.Get("Accept")), ImageBytesMIME)

	frame, err := c.ImageFrame()
	if err != nil {
		if wantBytes {
			num, msg := ErrorNumberFor(err)
			w.Header().Set("Content-Type", ImageBytesMIME)
			_, _ = w.Write(EncodeImageBytesError(num, msg, p.clientTransactionID, serverTx))
			return
		}
		writeValue(w, nil, err, p.clientTransactionID, serverTx)
		return
	}

	if !wantBytes {
		// JSON ImageArray not supported in this build.
		writeValue(w, nil, NewError(ErrNumNotImplemented,
			"ImageArray JSON not supported; request Accept: application/imagebytes"),
			p.clientTransactionID, serverTx)
		return
	}

	w.Header().Set("Content-Type", ImageBytesMIME)
	buf := getImageBuf(imageBytesMetadataLen + len(frame.Pixels))
	defer putImageBuf(buf)
	encStart := time.Now()
	encodeImageBytesInto(buf, frame, p.clientTransactionID, serverTx)
	encMs := float64(time.Since(encStart).Microseconds()) / 1000
	w.Header().Set("Content-Length", strconv.Itoa(len(buf)))
	wrStart := time.Now()
	_, _ = w.Write(buf)
	if imageDebug && s.cfg.Logger != nil {
		wrMs := float64(time.Since(wrStart).Microseconds()) / 1000
		s.cfg.Logger.Printf("imagebytes %dx%d rank%d: %d bytes  encode=%.1fms write=%.1fms",
			frame.Width, frame.Height, frame.Rank, len(buf), encMs, wrMs)
	}
}

// handleManagement serves the /management endpoints.
func (s *Server) handleManagement(w http.ResponseWriter, r *http.Request) {
	p := parseParams(r)
	serverTx := s.tx.next()

	switch r.URL.Path {
	case "/management/apiversions":
		writeValue(w, []int{1}, nil, p.clientTransactionID, serverTx)
	case "/management/v1/description":
		writeValue(w, map[string]any{
			"ServerName":          s.cfg.ServerName,
			"Manufacturer":        s.cfg.Manufacturer,
			"ManufacturerVersion": s.cfg.ManufacturerVersion,
			"Location":            s.cfg.Location,
		}, nil, p.clientTransactionID, serverTx)
	case "/management/v1/configureddevices":
		writeValue(w, s.configuredDevices(), nil, p.clientTransactionID, serverTx)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) configuredDevices() []map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]map[string]any, 0, len(s.order))
	for _, rd := range s.order {
		out = append(out, map[string]any{
			"DeviceName":   rd.dev.Name(),
			"DeviceType":   string(rd.typ),
			"DeviceNumber": rd.num,
			"UniqueID":     rd.dev.UniqueID(),
		})
	}
	return out
}
