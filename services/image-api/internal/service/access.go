package service

import (
	"github.com/google/uuid"

	"image-infrastructure-platform/services/image-api/internal/model"
)

// CanAccessImage enforces tenant ownership for image metadata and variants.
// Authenticated images require a matching JWT user. Guest images require a matching
// X-Guest-Session-ID. Rows with neither owner are denied.
func CanAccessImage(img *model.Image, authUserID *uuid.UUID, guestSessionID string) bool {
	if img == nil {
		return false
	}
	if img.UserID != nil && *img.UserID != "" {
		if authUserID == nil {
			return false
		}
		return authUserID.String() == *img.UserID
	}
	if img.GuestSessionID != nil && *img.GuestSessionID != "" {
		return guestSessionID != "" && guestSessionID == *img.GuestSessionID
	}
	return false
}
