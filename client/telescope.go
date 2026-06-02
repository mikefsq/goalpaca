package client

import (
	"net/url"

	alpaca "github.com/mikefsq/goalpaca/server"
)

// Telescope is a client for an ASCOM Telescope (mount) device.
type Telescope struct{ Device }

// NewTelescope returns a client for the mount at the given Alpaca address and
// device number.
func NewTelescope(address string, deviceNumber int, opts ...Option) *Telescope {
	return &Telescope{newDevice(address, alpaca.TelescopeType, deviceNumber, opts...)}
}

// Read-only properties.
func (t *Telescope) Altitude() (float64, error)           { return t.getFloat("altitude") }
func (t *Telescope) ApertureArea() (float64, error)       { return t.getFloat("aperturearea") }
func (t *Telescope) ApertureDiameter() (float64, error)   { return t.getFloat("aperturediameter") }
func (t *Telescope) AtHome() (bool, error)                { return t.getBool("athome") }
func (t *Telescope) AtPark() (bool, error)                { return t.getBool("atpark") }
func (t *Telescope) Azimuth() (float64, error)            { return t.getFloat("azimuth") }
func (t *Telescope) CanFindHome() (bool, error)           { return t.getBool("canfindhome") }
func (t *Telescope) CanPark() (bool, error)               { return t.getBool("canpark") }
func (t *Telescope) CanPulseGuide() (bool, error)         { return t.getBool("canpulseguide") }
func (t *Telescope) CanSetDeclinationRate() (bool, error) { return t.getBool("cansetdeclinationrate") }
func (t *Telescope) CanSetGuideRates() (bool, error)      { return t.getBool("cansetguiderates") }
func (t *Telescope) CanSetPark() (bool, error)            { return t.getBool("cansetpark") }
func (t *Telescope) CanSetPierSide() (bool, error)        { return t.getBool("cansetpierside") }
func (t *Telescope) CanSetRightAscensionRate() (bool, error) {
	return t.getBool("cansetrightascensionrate")
}
func (t *Telescope) CanSetTracking() (bool, error)    { return t.getBool("cansettracking") }
func (t *Telescope) CanSlew() (bool, error)           { return t.getBool("canslew") }
func (t *Telescope) CanSlewAltAz() (bool, error)      { return t.getBool("canslewaltaz") }
func (t *Telescope) CanSlewAltAzAsync() (bool, error) { return t.getBool("canslewaltazasync") }
func (t *Telescope) CanSlewAsync() (bool, error)      { return t.getBool("canslewasync") }
func (t *Telescope) CanSync() (bool, error)           { return t.getBool("cansync") }
func (t *Telescope) CanSyncAltAz() (bool, error)      { return t.getBool("cansyncaltaz") }
func (t *Telescope) CanUnpark() (bool, error)         { return t.getBool("canunpark") }
func (t *Telescope) Declination() (float64, error)    { return t.getFloat("declination") }
func (t *Telescope) FocalLength() (float64, error)    { return t.getFloat("focallength") }
func (t *Telescope) IsPulseGuiding() (bool, error)    { return t.getBool("ispulseguiding") }
func (t *Telescope) RightAscension() (float64, error) { return t.getFloat("rightascension") }
func (t *Telescope) SiderealTime() (float64, error)   { return t.getFloat("siderealtime") }
func (t *Telescope) Slewing() (bool, error)           { return t.getBool("slewing") }

// Read/write properties.
func (t *Telescope) DeclinationRate() (float64, error) { return t.getFloat("declinationrate") }
func (t *Telescope) SetDeclinationRate(v float64) error {
	return t.put("declinationrate", url.Values{"DeclinationRate": {floatParam(v)}})
}
func (t *Telescope) DoesRefraction() (bool, error) { return t.getBool("doesrefraction") }
func (t *Telescope) SetDoesRefraction(v bool) error {
	return t.put("doesrefraction", url.Values{"DoesRefraction": {boolParam(v)}})
}
func (t *Telescope) GuideRateDeclination() (float64, error) {
	return t.getFloat("guideratedeclination")
}
func (t *Telescope) SetGuideRateDeclination(v float64) error {
	return t.put("guideratedeclination", url.Values{"GuideRateDeclination": {floatParam(v)}})
}
func (t *Telescope) GuideRateRightAscension() (float64, error) {
	return t.getFloat("guideraterightascension")
}
func (t *Telescope) SetGuideRateRightAscension(v float64) error {
	return t.put("guideraterightascension", url.Values{"GuideRateRightAscension": {floatParam(v)}})
}
func (t *Telescope) RightAscensionRate() (float64, error) { return t.getFloat("rightascensionrate") }
func (t *Telescope) SetRightAscensionRate(v float64) error {
	return t.put("rightascensionrate", url.Values{"RightAscensionRate": {floatParam(v)}})
}
func (t *Telescope) SiteElevation() (float64, error) { return t.getFloat("siteelevation") }
func (t *Telescope) SetSiteElevation(v float64) error {
	return t.put("siteelevation", url.Values{"SiteElevation": {floatParam(v)}})
}
func (t *Telescope) SiteLatitude() (float64, error) { return t.getFloat("sitelatitude") }
func (t *Telescope) SetSiteLatitude(v float64) error {
	return t.put("sitelatitude", url.Values{"SiteLatitude": {floatParam(v)}})
}
func (t *Telescope) SiteLongitude() (float64, error) { return t.getFloat("sitelongitude") }
func (t *Telescope) SetSiteLongitude(v float64) error {
	return t.put("sitelongitude", url.Values{"SiteLongitude": {floatParam(v)}})
}
func (t *Telescope) SlewSettleTime() (int, error) { return t.getInt("slewsettletime") }
func (t *Telescope) SetSlewSettleTime(seconds int) error {
	return t.put("slewsettletime", url.Values{"SlewSettleTime": {intParam(seconds)}})
}
func (t *Telescope) TargetDeclination() (float64, error) { return t.getFloat("targetdeclination") }
func (t *Telescope) SetTargetDeclination(v float64) error {
	return t.put("targetdeclination", url.Values{"TargetDeclination": {floatParam(v)}})
}
func (t *Telescope) TargetRightAscension() (float64, error) {
	return t.getFloat("targetrightascension")
}
func (t *Telescope) SetTargetRightAscension(v float64) error {
	return t.put("targetrightascension", url.Values{"TargetRightAscension": {floatParam(v)}})
}
func (t *Telescope) Tracking() (bool, error) { return t.getBool("tracking") }
func (t *Telescope) SetTracking(on bool) error {
	return t.put("tracking", url.Values{"Tracking": {boolParam(on)}})
}
func (t *Telescope) UTCDate() (string, error) { return t.getString("utcdate") }
func (t *Telescope) SetUTCDate(iso8601 string) error {
	return t.put("utcdate", url.Values{"UTCDate": {iso8601}})
}

// Enum-typed properties.
func (t *Telescope) AlignmentMode() (alpaca.AlignmentMode, error) {
	v, err := t.getInt("alignmentmode")
	return alpaca.AlignmentMode(v), err
}
func (t *Telescope) EquatorialSystem() (alpaca.EquatorialCoordinateType, error) {
	v, err := t.getInt("equatorialsystem")
	return alpaca.EquatorialCoordinateType(v), err
}
func (t *Telescope) SideOfPier() (alpaca.PierSide, error) {
	v, err := t.getInt("sideofpier")
	return alpaca.PierSide(v), err
}
func (t *Telescope) SetSideOfPier(s alpaca.PierSide) error {
	return t.put("sideofpier", url.Values{"SideOfPier": {intParam(int(s))}})
}
func (t *Telescope) TrackingRate() (alpaca.DriveRate, error) {
	v, err := t.getInt("trackingrate")
	return alpaca.DriveRate(v), err
}
func (t *Telescope) SetTrackingRate(r alpaca.DriveRate) error {
	return t.put("trackingrate", url.Values{"TrackingRate": {intParam(int(r))}})
}
func (t *Telescope) TrackingRates() ([]alpaca.DriveRate, error) {
	var raw []int
	err := t.getInto("trackingrates", nil, &raw)
	rates := make([]alpaca.DriveRate, len(raw))
	for i, r := range raw {
		rates[i] = alpaca.DriveRate(r)
	}
	return rates, err
}

// Parameterized getters.
func (t *Telescope) AxisRates(axis alpaca.TelescopeAxis) ([]alpaca.AxisRate, error) {
	var v []alpaca.AxisRate
	err := t.getInto("axisrates", url.Values{"Axis": {intParam(int(axis))}}, &v)
	return v, err
}
func (t *Telescope) CanMoveAxis(axis alpaca.TelescopeAxis) (bool, error) {
	var v bool
	err := t.getInto("canmoveaxis", url.Values{"Axis": {intParam(int(axis))}}, &v)
	return v, err
}
func (t *Telescope) DestinationSideOfPier(rightAscension, declination float64) (alpaca.PierSide, error) {
	var v int
	err := t.getInto("destinationsideofpier", url.Values{
		"RightAscension": {floatParam(rightAscension)},
		"Declination":    {floatParam(declination)},
	}, &v)
	return alpaca.PierSide(v), err
}

// Methods.
func (t *Telescope) AbortSlew() error         { return t.put("abortslew", nil) }
func (t *Telescope) FindHome() error          { return t.put("findhome", nil) }
func (t *Telescope) Park() error              { return t.put("park", nil) }
func (t *Telescope) SetPark() error           { return t.put("setpark", nil) }
func (t *Telescope) SlewToTarget() error      { return t.put("slewtotarget", nil) }
func (t *Telescope) SlewToTargetAsync() error { return t.put("slewtotargetasync", nil) }
func (t *Telescope) SyncToTarget() error      { return t.put("synctotarget", nil) }
func (t *Telescope) Unpark() error            { return t.put("unpark", nil) }

func (t *Telescope) MoveAxis(axis alpaca.TelescopeAxis, rate float64) error {
	return t.put("moveaxis", url.Values{"Axis": {intParam(int(axis))}, "Rate": {floatParam(rate)}})
}
func (t *Telescope) PulseGuide(direction alpaca.GuideDirection, duration int) error {
	return t.put("pulseguide", url.Values{
		"Direction": {intParam(int(direction))}, "Duration": {intParam(duration)},
	})
}
func (t *Telescope) SlewToAltAz(azimuth, altitude float64) error {
	return t.put("slewtoaltaz", altAz(azimuth, altitude))
}
func (t *Telescope) SlewToAltAzAsync(azimuth, altitude float64) error {
	return t.put("slewtoaltazasync", altAz(azimuth, altitude))
}
func (t *Telescope) SyncToAltAz(azimuth, altitude float64) error {
	return t.put("synctoaltaz", altAz(azimuth, altitude))
}
func (t *Telescope) SlewToCoordinates(rightAscension, declination float64) error {
	return t.put("slewtocoordinates", raDec(rightAscension, declination))
}
func (t *Telescope) SlewToCoordinatesAsync(rightAscension, declination float64) error {
	return t.put("slewtocoordinatesasync", raDec(rightAscension, declination))
}
func (t *Telescope) SyncToCoordinates(rightAscension, declination float64) error {
	return t.put("synctocoordinates", raDec(rightAscension, declination))
}

func altAz(azimuth, altitude float64) url.Values {
	return url.Values{"Azimuth": {floatParam(azimuth)}, "Altitude": {floatParam(altitude)}}
}
func raDec(ra, dec float64) url.Values {
	return url.Values{"RightAscension": {floatParam(ra)}, "Declination": {floatParam(dec)}}
}
