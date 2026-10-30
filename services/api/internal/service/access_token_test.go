package service

import (
	"testing"
	"time"

	"image-infrastructure-platform/services/api/internal/model"
)

func TestVariantAccessSignerRoundTrip(t *testing.T) {
	s := NewVariantAccessSigner("test-secret", time.Hour)
	exp, sig, err := s.Sign("img-1", "u:user-a")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Verify("img-1", "u:user-a", sig, exp) {
		t.Fatal("expected valid signature")
	}
}

func TestVariantAccessSignerRejectsExpired(t *testing.T) {
	s := NewVariantAccessSigner("test-secret", time.Hour)
	exp := time.Now().UTC().Add(-time.Minute).Unix()
	sig := s.mac("img-1", "u:user-a", exp)
	if s.Verify("img-1", "u:user-a", sig, exp) {
		t.Fatal("expired token must be rejected")
	}
}

func TestVariantAccessSignerRejectsOtherTenant(t *testing.T) {
	s := NewVariantAccessSigner("test-secret", time.Hour)
	exp, sig, err := s.Sign("img-1", "u:user-a")
	if err != nil {
		t.Fatal(err)
	}
	if s.Verify("img-1", "u:user-b", sig, exp) {
		t.Fatal("signature must not verify for a different tenant")
	}
}

func TestVariantAccessSignerDisabledWithoutSecret(t *testing.T) {
	if NewVariantAccessSigner("", time.Hour) != nil {
		t.Fatal("expected nil signer without secret")
	}
}

func TestImageTenantKey(t *testing.T) {
	uid := "11111111-1111-1111-1111-111111111111"
	gid := "guest-abc"
	userOwned := &model.Image{UserID: &uid, GuestSessionID: &gid}
	if got := ImageTenantKey(userOwned); got != "u:"+uid {
		t.Fatalf("user tenant = %q", got)
	}
	guestOwned := &model.Image{GuestSessionID: &gid}
	if got := ImageTenantKey(guestOwned); got != "g:"+gid {
		t.Fatalf("guest tenant = %q", got)
	}
	if ImageTenantKey(&model.Image{}) != "" {
		t.Fatal("unowned image must have empty tenant")
	}
}
