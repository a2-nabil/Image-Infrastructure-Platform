package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims are the JWT claims issued by the image-api.
type Claims struct {
	Email  string `json:"email,omitempty"`
	UserID string `json:"user_id,omitempty"`
	jwt.RegisteredClaims
}

// GenerateToken issues a signed HS256 JWT for the given user.
func GenerateToken(userID uuid.UUID, email string, secret string, ttl time.Duration) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("jwt secret is required")
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	now := time.Now().UTC()
	claims := Claims{
		Email:  email,
		UserID: userID.String(),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return signed, nil
}

// ParseUserID extracts the user UUID from a signed JWT.
func ParseUserID(tokenString, secret string) (uuid.UUID, bool) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrTokenSignatureInvalid
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return uuid.Nil, false
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		return uuid.Nil, false
	}

	raw := claims.Subject
	if raw == "" {
		raw = claims.UserID
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}
