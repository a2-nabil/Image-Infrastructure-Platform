package service

import (
	"bytes"
	"io"
	"strconv"
	"strings"

	"github.com/rwcarlsen/goexif/exif"
)

const (
	locationSourceEXIF      = "exif"
	locationSourceClientGPS = "client_gps"
)

// LocationData holds optional GPS metadata for an image.
type LocationData struct {
	Latitude  *float64
	Longitude *float64
	Altitude  *float64
	Source    string
}

// ExtractLocation reads GPS tags from an image stream.
func ExtractLocation(r io.Reader) (lat *float64, lng *float64, alt *float64, source string) {
	if r == nil {
		return nil, nil, nil, ""
	}

	x, err := exif.Decode(r)
	if err != nil {
		return nil, nil, nil, ""
	}

	latitude, longitude, err := x.LatLong()
	if err != nil {
		return nil, nil, nil, ""
	}

	latCopy := latitude
	lngCopy := longitude
	lat, lng = &latCopy, &lngCopy
	source = locationSourceEXIF

	if tag, err := x.Get(exif.GPSAltitude); err == nil {
		if rat, err := tag.Rat(0); err == nil && rat != nil {
			altVal, _ := rat.Float64()
			if ref, err := x.Get(exif.GPSAltitudeRef); err == nil {
				if v, err := ref.Int(0); err == nil && v == 1 {
					altVal = -altVal
				}
			}
			alt = &altVal
		}
	}

	return lat, lng, alt, source
}

// ResolveLocation prefers EXIF GPS, then optional client-provided coordinates.
func ResolveLocation(imageBytes []byte, clientLat, clientLng *float64) LocationData {
	lat, lng, alt, source := ExtractLocation(bytes.NewReader(imageBytes))
	if lat != nil && lng != nil {
		return LocationData{
			Latitude:  lat,
			Longitude: lng,
			Altitude:  alt,
			Source:    source,
		}
	}

	if clientLat != nil && clientLng != nil {
		return LocationData{
			Latitude:  clientLat,
			Longitude: clientLng,
			Source:    locationSourceClientGPS,
		}
	}

	return LocationData{}
}

// ParseOptionalFloat parses a form field into *float64.
func ParseOptionalFloat(raw string) *float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil
	}
	return &v
}
