package alpacadev

// OpticsStore is the shared source of truth for a telescope's optical-train
// parameters — the mount itself can't report them, so they come from config and
// can be updated at runtime. A driver that exposes UseOptics(OpticsStore) lets a
// composition root inject ONE holder shared with other front-ends (e.g. an INDI
// server's TELESCOPE_INFO), so ApertureDiameter/FocalLength here and the other
// front-end never diverge. Values are metres / m²; the guider members feed
// non-ASCOM consumers such as INDI's GUIDER_APERTURE / GUIDER_FOCAL_LENGTH.
type OpticsStore interface {
	Optics() (apertureM, apertureAreaM2, focalLengthM, guiderApertureM, guiderFocalLengthM float64)
	SetOptics(apertureM, apertureAreaM2, focalLengthM, guiderApertureM, guiderFocalLengthM float64)
}
