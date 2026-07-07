package server

import "sync"

// Telescope is the ASCOM Telescope interface (ITelescopeV3/V4). The *Async slew
// methods, FindHome, Park, Unpark and PulseGuide are initiators; Slewing /
// AtHome / AtPark / IsPulseGuiding are the completion properties. AxisRates,
// CanMoveAxis and DestinationSideOfPier take parameters on GET.
type Telescope interface {
	Device

	AlignmentMode() AlignmentMode
	Altitude() float64
	ApertureArea() float64
	ApertureDiameter() float64
	AtHome() bool
	AtPark() bool
	Azimuth() float64
	CanFindHome() bool
	CanPark() bool
	CanPulseGuide() bool
	CanSetDeclinationRate() bool
	CanSetGuideRates() bool
	CanSetPark() bool
	CanSetPierSide() bool
	CanSetRightAscensionRate() bool
	CanSetTracking() bool
	CanSlew() bool
	CanSlewAltAz() bool
	CanSlewAltAzAsync() bool
	CanSlewAsync() bool
	CanSync() bool
	CanSyncAltAz() bool
	CanUnpark() bool
	Declination() float64
	DeclinationRate() float64
	SetDeclinationRate(float64) error
	DoesRefraction() bool
	SetDoesRefraction(bool) error
	EquatorialSystem() EquatorialCoordinateType
	FocalLength() float64
	GuideRateDeclination() float64
	SetGuideRateDeclination(float64) error
	GuideRateRightAscension() float64
	SetGuideRateRightAscension(float64) error
	IsPulseGuiding() bool
	RightAscension() float64
	RightAscensionRate() float64
	SetRightAscensionRate(float64) error
	SideOfPier() PierSide
	SetSideOfPier(PierSide) error
	SiderealTime() float64
	SiteElevation() float64
	SetSiteElevation(float64) error
	SiteLatitude() float64
	SetSiteLatitude(float64) error
	SiteLongitude() float64
	SetSiteLongitude(float64) error
	Slewing() bool
	SlewSettleTime() int
	SetSlewSettleTime(int) error
	// Target getters error with InvalidOperation until a target has been set
	// (the ASCOM read-before-set rule).
	TargetDeclination() (float64, error)
	SetTargetDeclination(float64) error
	TargetRightAscension() (float64, error)
	SetTargetRightAscension(float64) error
	Tracking() bool
	SetTracking(bool) error
	TrackingRate() DriveRate
	SetTrackingRate(DriveRate) error
	TrackingRates() []DriveRate
	UTCDate() string
	SetUTCDate(string) error

	AbortSlew() error
	AxisRates(axis TelescopeAxis) []AxisRate
	CanMoveAxis(axis TelescopeAxis) bool
	DestinationSideOfPier(rightAscension, declination float64) (PierSide, error)
	FindHome() error
	MoveAxis(axis TelescopeAxis, rate float64) error
	Park() error
	PulseGuide(direction GuideDirection, duration int) error
	SetPark() error
	SlewToAltAz(azimuth, altitude float64) error
	SlewToAltAzAsync(azimuth, altitude float64) error
	SlewToCoordinates(rightAscension, declination float64) error
	SlewToCoordinatesAsync(rightAscension, declination float64) error
	SlewToTarget() error
	SlewToTargetAsync() error
	SyncToAltAz(azimuth, altitude float64) error
	SyncToCoordinates(rightAscension, declination float64) error
	SyncToTarget() error
	Unpark() error
}

// BaseTelescope provides not-implemented / incapable defaults for Telescope.
//
// The target properties are fully implemented: values are stored, and the
// getters return InvalidOperation until the matching setter has been called
// (the ITelescopeV4 read-before-set rule). The HTTP layer also propagates
// SlewToCoordinates[Async]/SyncToCoordinates coordinates into the targets, so
// a driver that embeds BaseTelescope gets compliant target behavior without
// writing any of it. Override the four target members together to take over.
type BaseTelescope struct {
	BaseDevice

	targetMu     sync.Mutex
	targetRA     float64
	targetDec    float64
	targetRASet  bool
	targetDecSet bool
}

func (b *BaseTelescope) AlignmentMode() AlignmentMode               { return AlignGermanPolar }
func (b *BaseTelescope) Altitude() float64                          { return 0 }
func (b *BaseTelescope) ApertureArea() float64                      { return 0 }
func (b *BaseTelescope) ApertureDiameter() float64                  { return 0 }
func (b *BaseTelescope) AtHome() bool                               { return false }
func (b *BaseTelescope) AtPark() bool                               { return false }
func (b *BaseTelescope) Azimuth() float64                           { return 0 }
func (b *BaseTelescope) CanFindHome() bool                          { return false }
func (b *BaseTelescope) CanPark() bool                              { return false }
func (b *BaseTelescope) CanPulseGuide() bool                        { return false }
func (b *BaseTelescope) CanSetDeclinationRate() bool                { return false }
func (b *BaseTelescope) CanSetGuideRates() bool                     { return false }
func (b *BaseTelescope) CanSetPark() bool                           { return false }
func (b *BaseTelescope) CanSetPierSide() bool                       { return false }
func (b *BaseTelescope) CanSetRightAscensionRate() bool             { return false }
func (b *BaseTelescope) CanSetTracking() bool                       { return false }
func (b *BaseTelescope) CanSlew() bool                              { return false }
func (b *BaseTelescope) CanSlewAltAz() bool                         { return false }
func (b *BaseTelescope) CanSlewAltAzAsync() bool                    { return false }
func (b *BaseTelescope) CanSlewAsync() bool                         { return false }
func (b *BaseTelescope) CanSync() bool                              { return false }
func (b *BaseTelescope) CanSyncAltAz() bool                         { return false }
func (b *BaseTelescope) CanUnpark() bool                            { return false }
func (b *BaseTelescope) Declination() float64                       { return 0 }
func (b *BaseTelescope) DeclinationRate() float64                   { return 0 }
func (b *BaseTelescope) SetDeclinationRate(float64) error           { return ErrNotImplemented }
func (b *BaseTelescope) DoesRefraction() bool                       { return false }
func (b *BaseTelescope) SetDoesRefraction(bool) error               { return ErrNotImplemented }
func (b *BaseTelescope) EquatorialSystem() EquatorialCoordinateType { return EquTopocentric }
func (b *BaseTelescope) FocalLength() float64                       { return 0 }
func (b *BaseTelescope) GuideRateDeclination() float64              { return 0 }
func (b *BaseTelescope) SetGuideRateDeclination(float64) error      { return ErrNotImplemented }
func (b *BaseTelescope) GuideRateRightAscension() float64           { return 0 }
func (b *BaseTelescope) SetGuideRateRightAscension(float64) error   { return ErrNotImplemented }
func (b *BaseTelescope) IsPulseGuiding() bool                       { return false }
func (b *BaseTelescope) RightAscension() float64                    { return 0 }
func (b *BaseTelescope) RightAscensionRate() float64                { return 0 }
func (b *BaseTelescope) SetRightAscensionRate(float64) error        { return ErrNotImplemented }
func (b *BaseTelescope) SideOfPier() PierSide                       { return PierUnknown }
func (b *BaseTelescope) SetSideOfPier(PierSide) error               { return ErrNotImplemented }
func (b *BaseTelescope) SiderealTime() float64                      { return 0 }
func (b *BaseTelescope) SiteElevation() float64                     { return 0 }
func (b *BaseTelescope) SetSiteElevation(float64) error             { return ErrNotImplemented }
func (b *BaseTelescope) SiteLatitude() float64                      { return 0 }
func (b *BaseTelescope) SetSiteLatitude(float64) error              { return ErrNotImplemented }
func (b *BaseTelescope) SiteLongitude() float64                     { return 0 }
func (b *BaseTelescope) SetSiteLongitude(float64) error             { return ErrNotImplemented }
func (b *BaseTelescope) Slewing() bool                              { return false }
func (b *BaseTelescope) SlewSettleTime() int                        { return 0 }
func (b *BaseTelescope) SetSlewSettleTime(int) error                { return ErrNotImplemented }
func (b *BaseTelescope) TargetDeclination() (float64, error) {
	b.targetMu.Lock()
	defer b.targetMu.Unlock()
	if !b.targetDecSet {
		return 0, invalidOpErr("TargetDeclination has not been set")
	}
	return b.targetDec, nil
}
func (b *BaseTelescope) SetTargetDeclination(v float64) error {
	if err := invalidRange("TargetDeclination", v, -90, 90); err != nil {
		return err
	}
	b.targetMu.Lock()
	defer b.targetMu.Unlock()
	b.targetDec, b.targetDecSet = v, true
	return nil
}
func (b *BaseTelescope) TargetRightAscension() (float64, error) {
	b.targetMu.Lock()
	defer b.targetMu.Unlock()
	if !b.targetRASet {
		return 0, invalidOpErr("TargetRightAscension has not been set")
	}
	return b.targetRA, nil
}
func (b *BaseTelescope) SetTargetRightAscension(v float64) error {
	if v < 0 || v >= 24 {
		return invalidValuef("TargetRightAscension %g is outside the valid range 0 to 23.999", v)
	}
	b.targetMu.Lock()
	defer b.targetMu.Unlock()
	b.targetRA, b.targetRASet = v, true
	return nil
}
func (b *BaseTelescope) Tracking() bool                  { return false }
func (b *BaseTelescope) SetTracking(bool) error          { return ErrNotImplemented }
func (b *BaseTelescope) TrackingRate() DriveRate         { return DriveSidereal }
func (b *BaseTelescope) SetTrackingRate(DriveRate) error { return ErrNotImplemented }
func (b *BaseTelescope) TrackingRates() []DriveRate      { return []DriveRate{DriveSidereal} }
func (b *BaseTelescope) UTCDate() string                 { return "" }
func (b *BaseTelescope) SetUTCDate(string) error         { return ErrNotImplemented }

func (b *BaseTelescope) AbortSlew() error                   { return ErrNotImplemented }
func (b *BaseTelescope) AxisRates(TelescopeAxis) []AxisRate { return []AxisRate{} }
func (b *BaseTelescope) CanMoveAxis(TelescopeAxis) bool     { return false }
func (b *BaseTelescope) DestinationSideOfPier(float64, float64) (PierSide, error) {
	return PierUnknown, ErrNotImplemented
}
func (b *BaseTelescope) FindHome() error                         { return ErrNotImplemented }
func (b *BaseTelescope) MoveAxis(TelescopeAxis, float64) error   { return ErrNotImplemented }
func (b *BaseTelescope) Park() error                             { return ErrNotImplemented }
func (b *BaseTelescope) PulseGuide(GuideDirection, int) error    { return ErrNotImplemented }
func (b *BaseTelescope) SetPark() error                          { return ErrNotImplemented }
func (b *BaseTelescope) SlewToAltAz(float64, float64) error      { return ErrNotImplemented }
func (b *BaseTelescope) SlewToAltAzAsync(float64, float64) error { return ErrNotImplemented }
func (b *BaseTelescope) SlewToCoordinates(float64, float64) error {
	return ErrNotImplemented
}
func (b *BaseTelescope) SlewToCoordinatesAsync(float64, float64) error {
	return ErrNotImplemented
}
func (b *BaseTelescope) SlewToTarget() error                { return ErrNotImplemented }
func (b *BaseTelescope) SlewToTargetAsync() error           { return ErrNotImplemented }
func (b *BaseTelescope) SyncToAltAz(float64, float64) error { return ErrNotImplemented }
func (b *BaseTelescope) SyncToCoordinates(float64, float64) error {
	return ErrNotImplemented
}
func (b *BaseTelescope) SyncToTarget() error { return ErrNotImplemented }
func (b *BaseTelescope) Unpark() error       { return ErrNotImplemented }
