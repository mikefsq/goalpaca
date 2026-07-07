// Package sim provides ASCOM Alpaca device simulators built on the goalpaca
// server library. Each simulator is an ordinary driver: it implements the typed
// device interface and embeds the matching Base type, so the server library
// supplies the protocol compliance (parameter validation, capability and
// parked gating, connection handling) and the simulator implements only the
// hardware behaviour — time-based asynchronous motion and derived-on-read
// physics. Together they pass ConformU, and the package serves as a
// hardware-free test target and a worked reference for driver authors.
// Behaviour mirrors the official ASCOM.Alpaca.Simulators reference devices.
package sim
