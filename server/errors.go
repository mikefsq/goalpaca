package alpacadev

import "errors"

// ASCOM Alpaca reserved error numbers. Driver-defined errors use the
// 0x500–0xFFF range. (Confirm the full reserved table against the Alpaca API
// reference — spec §12 open item #1.)
const (
	ErrNumNotImplemented        = 0x400
	ErrNumInvalidValue          = 0x401
	ErrNumValueNotSet           = 0x402
	ErrNumNotConnected          = 0x407
	ErrNumParked                = 0x408 // Platform 7 ParkedException
	ErrNumSlaved                = 0x409 // Platform 7 SlavedException
	ErrNumSettingsProviderError = 0x40A
	ErrNumInvalidOperation      = 0x40B
	ErrNumActionNotImplemented  = 0x40C
	ErrNumOperationCancelled    = 0x40E // Platform 7: async op cancelled
	ErrNumUnspecified           = 0x4FF

	// Pre-Platform-7 names retained as aliases.
	ErrNumInvalidWhileParked = ErrNumParked
	ErrNumInvalidWhileSlaved = ErrNumSlaved

	ErrNumDriverBase = 0x500 // first driver-defined number
	ErrNumDriverMax  = 0xFFF // last driver-defined number
)

// AlpacaError pairs an ASCOM error number with a message. A driver member may
// return one of these directly to control the in-band ErrorNumber; any other
// (plain) error is mapped via ErrorNumberFor.
type AlpacaError struct {
	Number  int
	Message string
}

func (e *AlpacaError) Error() string { return e.Message }

// Is matches by ASCOM error number, so errors.Is(err, ErrNotImplemented) works
// whether err is a sentinel or a freshly built AlpacaError (e.g. one the client
// reconstructs from a response ErrorNumber). Matching by number, not identity.
func (e *AlpacaError) Is(target error) bool {
	var t *AlpacaError
	if errors.As(target, &t) {
		return e.Number == t.Number
	}
	return false
}

// NewError builds an AlpacaError with a driver-defined number (clamped into the
// reserved 0x500–0xFFF range).
func NewError(number int, message string) *AlpacaError {
	if number < ErrNumDriverBase || number > ErrNumDriverMax {
		// Allow the well-known reserved numbers through unchanged; otherwise
		// fold stray values into the driver range so we never emit a number
		// that collides with a different reserved meaning.
		if !isReservedNumber(number) {
			number = ErrNumDriverBase
		}
	}
	return &AlpacaError{Number: number, Message: message}
}

func isReservedNumber(n int) bool {
	switch n {
	case ErrNumNotImplemented, ErrNumInvalidValue, ErrNumValueNotSet,
		ErrNumNotConnected, ErrNumParked, ErrNumSlaved, ErrNumSettingsProviderError,
		ErrNumInvalidOperation, ErrNumActionNotImplemented, ErrNumOperationCancelled,
		ErrNumUnspecified:
		return true
	}
	return false
}

// Sentinel errors for the common ASCOM conditions. Return these (or wrap them)
// from a driver member and the HTTP layer emits the matching ErrorNumber.
var (
	ErrNotImplemented       = &AlpacaError{ErrNumNotImplemented, "Not implemented"}
	ErrInvalidValue         = &AlpacaError{ErrNumInvalidValue, "Invalid value"}
	ErrValueNotSet          = &AlpacaError{ErrNumValueNotSet, "Value not set"}
	ErrNotConnected         = &AlpacaError{ErrNumNotConnected, "Not connected"}
	ErrParked               = &AlpacaError{ErrNumParked, "Parked"}
	ErrSlaved               = &AlpacaError{ErrNumSlaved, "Slaved"}
	ErrInvalidOperation     = &AlpacaError{ErrNumInvalidOperation, "Invalid operation"}
	ErrActionNotImplemented = &AlpacaError{ErrNumActionNotImplemented, "Action not implemented"}
	ErrOperationCancelled   = &AlpacaError{ErrNumOperationCancelled, "Operation cancelled"}

	// Pre-Platform-7 sentinel names retained as aliases.
	ErrInvalidWhileParked = ErrParked
	ErrInvalidWhileSlaved = ErrSlaved
)

// ErrorNumberFor maps a Go error to an ASCOM (number, message) pair for the
// in-band envelope. A nil error is success (0, ""). A known AlpacaError keeps
// its number; anything else becomes UnspecifiedError (0x4FF) with the error's
// text.
func ErrorNumberFor(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	var ae *AlpacaError
	if errors.As(err, &ae) {
		return ae.Number, ae.Message
	}
	return ErrNumUnspecified, err.Error()
}
