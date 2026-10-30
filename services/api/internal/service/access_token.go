package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"image-infrastructure-platform/services/api/internal/model"
)

const defaultVariantAccessTTL = time.Hour

// VariantAccessSigner issues short-lived HMAC tokens so <img src> / download
// links work without custom auth headers (capability URLs).
// Tokens are bound to the image id AND tenant (user or guest) so a leaked
// URL from another account's session cannot authorize a different tenant's object.
type VariantAccessSigner struct {
	secret []byte
	ttl    time.Duration
}

func NewVariantAccessSigner(secret string, ttl time.Duration) *VariantAccessSigner {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = defaultVariantAccessTTL
	}
	return &VariantAccessSigner{secret: []byte(secret), ttl: ttl}
}

func (s *VariantAccessSigner) Enabled() bool {
	return s != nil && len(s.secret) > 0
}

// ImageTenantKey returns the ownership scope embedded in capability URLs.
func ImageTenantKey(img *model.Image) string {
	if img == nil {
		return ""
	}
	if img.UserID != nil && strings.TrimSpace(*img.UserID) != "" {
		return "u:" + strings.TrimSpace(*img.UserID)
	}
	if img.GuestSessionID != nil && strings.TrimSpace(*img.GuestSessionID) != "" {
		return "g:" + strings.TrimSpace(*img.GuestSessionID)
	}
	return ""
}

// Sign returns exp unix + hex HMAC for an image id + tenant.
func (s *VariantAccessSigner) Sign(imageID, tenantKey string) (exp int64, sig string, err error) {
	if !s.Enabled() {
		return 0, "", fmt.Errorf("variant access signer is not configured")
	}
	if imageID == "" || tenantKey == "" {
		return 0, "", fmt.Errorf("image id and tenant are required")
	}
	exp = time.Now().UTC().Add(s.ttl).Unix()
	sig = s.mac(imageID, tenantKey, exp)
	return exp, sig, nil
}

// Verify checks exp+sig for the image id + tenant.
func (s *VariantAccessSigner) Verify(imageID, tenantKey, sig string, exp int64) bool {
	if !s.Enabled() || imageID == "" || tenantKey == "" || sig == "" || exp <= 0 {
		return false
	}
	if time.Now().UTC().Unix() > exp {
		return false
	}
	expected := s.mac(imageID, tenantKey, exp)
	return hmac.Equal([]byte(expected), []byte(strings.ToLower(strings.TrimSpace(sig))))
}

func (s *VariantAccessSigner) mac(imageID, tenantKey string, exp int64) string {
	payload := imageID + "|" + tenantKey + "|" + strconv.FormatInt(exp, 10)
	h := hmac.New(sha256.New, s.secret)
	_, _ = h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}

// Query returns "?exp=...&sig=..." for appending to a variant proxy path.
func (s *VariantAccessSigner) Query(imageID, tenantKey string) string {
	exp, sig, err := s.Sign(imageID, tenantKey)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("?exp=%d&sig=%s", exp, sig)
}
