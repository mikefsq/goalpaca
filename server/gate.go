package server

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// This file holds the spec-fixed compliance gates the library applies in the
// HTTP dispatch layer, before a driver method is called. The rules here are
// device-independent — parameter ranges, Can-flag → NotImplemented mapping,
// parked gating, and similar conditions fixed by the ASCOM master interface
// definitions and enforced by ConformU. Enforcing them here means any driver
// that implements the typed interfaces presents a compliant Alpaca device;
// drivers only implement hardware-specific behavior (and may impose stricter
// hardware limits of their own, which run after these gates).

// invalidValuef builds an InvalidValue (0x401) error with a descriptive message.
func invalidValuef(format string, a ...any) error {
	return &AlpacaError{Number: ErrNumInvalidValue, Message: fmt.Sprintf(format, a...)}
}

// invalidRange rejects v outside [lo, hi].
func invalidRange(name string, v, lo, hi float64) error {
	if v < lo || v > hi {
		return invalidValuef("%s %g is outside the valid range %g to %g", name, v, lo, hi)
	}
	return nil
}

// notImplErr builds a NotImplemented (0x400) error naming the member, for
// Can-flag gates ("CanX is false so X is not implemented").
func notImplErr(member string) error {
	return &AlpacaError{Number: ErrNumNotImplemented, Message: member + " is not implemented"}
}

// parkedErr builds a Parked (0x408) error naming the member.
func parkedErr(member string) error {
	return &AlpacaError{Number: ErrNumParked, Message: member + " is not valid while the mount is parked"}
}

// invalidOpErr builds an InvalidOperation (0x40B) error.
func invalidOpErr(message string) error {
	return &AlpacaError{Number: ErrNumInvalidOperation, Message: message}
}

// validAxisRate reports whether rate is zero (stop, always allowed) or its
// magnitude falls inside one of the device's advertised AxisRates ranges.
func validAxisRate(t Telescope, axis TelescopeAxis, rate float64) bool {
	if rate == 0 {
		return true
	}
	r := math.Abs(rate)
	for _, ar := range t.AxisRates(axis) {
		if r >= ar.Minimum && r <= ar.Maximum {
			return true
		}
	}
	return false
}

// validDriveRate reports whether dr is one of the device's TrackingRates.
func validDriveRate(t Telescope, dr DriveRate) bool {
	for _, r := range t.TrackingRates() {
		if r == dr {
			return true
		}
	}
	return false
}

// utcDateLayouts are the ISO-8601 forms accepted for PUT utcdate.
var utcDateLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999999999", // no zone designator: treated as UTC
	"2006-01-02T15:04:05",
}

// parseUTCDate validates an ISO-8601 UTCDate string.
func parseUTCDate(s string) error {
	for _, layout := range utcDateLayouts {
		if _, err := time.Parse(layout, s); err == nil {
			return nil
		}
	}
	return invalidValuef("UTCDate %q is not an ISO-8601 date-time", s)
}

// setTargets propagates a successful coordinate slew/sync into the target
// properties, per the ITelescopeV4 rule that SlewToCoordinates[Async] and
// SyncToCoordinates set TargetRightAscension/TargetDeclination. Best effort:
// a driver that does this itself just gets an idempotent re-set, and errors
// from drivers that do not implement targets are ignored.
func setTargets(t Telescope, ra, dec float64) {
	_ = t.SetTargetRightAscension(ra)
	_ = t.SetTargetDeclination(dec)
}

// requireTargetsSet enforces the read-before-set rule for SlewToTarget[Async]
// and SyncToTarget: both target properties must have been set (their getters
// must not error) before a target operation is valid.
func requireTargetsSet(t Telescope) error {
	if _, err := t.TargetRightAscension(); err != nil {
		return err
	}
	if _, err := t.TargetDeclination(); err != nil {
		return err
	}
	return nil
}

// ocSensors is the canonical ObservingConditions sensor-property set
// (IObservingConditionsV2). SensorDescription and TimeSinceLastUpdate must
// reject any other name with InvalidValue.
var ocSensors = map[string]bool{
	"cloudcover": true, "dewpoint": true, "humidity": true, "pressure": true,
	"rainrate": true, "skybrightness": true, "skyquality": true,
	"skytemperature": true, "starfwhm": true, "temperature": true,
	"winddirection": true, "windgust": true, "windspeed": true,
}

// validOCSensor reports whether name is a canonical sensor property name
// (case-insensitive). The empty string is valid only where allowEmpty says so
// (TimeSinceLastUpdate("") means "any sensor").
func validOCSensor(name string, allowEmpty bool) bool {
	if name == "" {
		return allowEmpty
	}
	return ocSensors[strings.ToLower(name)]
}
