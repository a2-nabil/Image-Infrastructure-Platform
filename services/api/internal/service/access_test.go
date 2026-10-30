package service

import (
	"testing"

	"github.com/google/uuid"

	"image-infrastructure-platform/services/api/internal/model"
)

func TestCanAccessImage(t *testing.T) {
	user := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	other := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	owner := user.String()
	guest := "guest-session-1"

	cases := []struct {
		name    string
		img     *model.Image
		auth    *uuid.UUID
		guestID string
		want    bool
	}{
		{
			name: "owner user",
			img:  &model.Image{UserID: &owner},
			auth: &user,
			want: true,
		},
		{
			name: "wrong user",
			img:  &model.Image{UserID: &owner},
			auth: &other,
			want: false,
		},
		{
			name: "user image without auth",
			img:  &model.Image{UserID: &owner},
			want: false,
		},
		{
			name:    "matching guest",
			img:     &model.Image{GuestSessionID: &guest},
			guestID: guest,
			want:    true,
		},
		{
			name:    "wrong guest",
			img:     &model.Image{GuestSessionID: &guest},
			guestID: "other",
			want:    false,
		},
		{
			name: "unowned denied",
			img:  &model.Image{},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CanAccessImage(tc.img, tc.auth, tc.guestID)
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
