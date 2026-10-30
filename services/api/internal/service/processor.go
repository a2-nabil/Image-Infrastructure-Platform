package service

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"strings"

	"github.com/chai2010/webp"
	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp"

	"image-infrastructure-platform/services/api/internal/model"
)

const defaultVariantWebPQuality = 80

type variantPreset struct {
	name string
	fn   func(image.Image) image.Image
}

// Named custom presets (opt-in). Auto conversion uses "original" only.
var customVariantPresets = []variantPreset{
	{
		name: "thumbnail",
		fn: func(src image.Image) image.Image {
			return imaging.Fill(src, 150, 150, imaging.Center, imaging.Lanczos)
		},
	},
	{
		name: "small",
		fn: func(src image.Image) image.Image {
			return imaging.Resize(src, 320, 0, imaging.Lanczos)
		},
	},
	{
		name: "medium",
		fn: func(src image.Image) image.Image {
			return imaging.Resize(src, 768, 0, imaging.Lanczos)
		},
	},
	{
		name: "large",
		fn: func(src image.Image) image.Image {
			return imaging.Resize(src, 1280, 0, imaging.Lanczos)
		},
	},
}

// NormalizeVariantPresets returns the presets to generate.
// Empty input defaults to original-size WebP only (auto convert).
func NormalizeVariantPresets(presets []string) []string {
	if len(presets) == 0 {
		return []string{"original"}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(presets))
	for _, raw := range presets {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		if name != "original" && name != "custom" {
			ok := false
			for _, p := range customVariantPresets {
				if p.name == name {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return []string{"original"}
	}
	return out
}

func clampWebPQuality(q int) float32 {
	if q <= 0 {
		return defaultVariantWebPQuality
	}
	if q > 100 {
		return 100
	}
	return float32(q)
}

func clampDimension(n int) int {
	if n < 0 {
		return 0
	}
	if n > 8192 {
		return 8192
	}
	return n
}

func resizeCustom(src image.Image, width, height int) image.Image {
	w := clampDimension(width)
	h := clampDimension(height)
	if w <= 0 && h <= 0 {
		return src
	}
	if w > 0 && h > 0 {
		return imaging.Fit(src, w, h, imaging.Lanczos)
	}
	return imaging.Resize(src, w, h, imaging.Lanczos)
}

// GenerateVariants decodes the raw image, creates selected WebP variants, uploads
// them to S3, and returns variant metadata ready for persistence.
// Default (empty presets) produces only an original-size WebP.
// Optional customWidth/customHeight produce a "custom" fitted WebP when requested.
func (s *ImageService) GenerateVariants(
	ctx context.Context,
	imgID string,
	userID *string,
	rawReader io.Reader,
	presets []string,
	quality int,
	customWidth, customHeight int,
) ([]*model.ImageVariant, error) {
	if s == nil || s.s3 == nil {
		return nil, fmt.Errorf("image service is not initialized")
	}
	if imgID == "" {
		return nil, fmt.Errorf("image id is required")
	}
	if rawReader == nil {
		return nil, fmt.Errorf("raw image reader is required")
	}

	src, err := imaging.Decode(rawReader, imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	names := NormalizeVariantPresets(presets)
	if (customWidth > 0 || customHeight > 0) && !containsPreset(names, "custom") {
		names = append(names, "custom")
	}

	q := clampWebPQuality(quality)
	variants := make([]*model.ImageVariant, 0, len(names))

	for _, name := range names {
		var resized image.Image
		switch name {
		case "original":
			resized = src
		case "custom":
			if customWidth <= 0 && customHeight <= 0 {
				continue
			}
			resized = resizeCustom(src, customWidth, customHeight)
		default:
			preset, ok := findCustomPreset(name)
			if !ok {
				continue
			}
			resized = preset.fn(src)
		}

		var buf bytes.Buffer
		if err := webp.Encode(&buf, resized, &webp.Options{Quality: q}); err != nil {
			return nil, fmt.Errorf("encode %s variant: %w", name, err)
		}

		storagePath := variantStoragePath(userID, imgID, name)
		if _, err := s.s3.UploadImage(ctx, storagePath, bytes.NewReader(buf.Bytes()), "image/webp"); err != nil {
			return nil, fmt.Errorf("upload %s variant: %w", name, err)
		}

		bounds := resized.Bounds()
		variants = append(variants, &model.ImageVariant{
			ImageID:       imgID,
			PresetName:    name,
			StoragePath:   storagePath,
			MimeType:      "image/webp",
			FileSizeBytes: int64(buf.Len()),
			Width:         bounds.Dx(),
			Height:        bounds.Dy(),
		})
	}

	if len(variants) == 0 {
		return nil, fmt.Errorf("no variants generated")
	}
	return variants, nil
}

func containsPreset(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func findCustomPreset(name string) (variantPreset, bool) {
	for _, p := range customVariantPresets {
		if p.name == name {
			return p, true
		}
	}
	return variantPreset{}, false
}

func variantStoragePath(userID *string, imgID, presetName string) string {
	if userID != nil && *userID != "" {
		return fmt.Sprintf("users/%s/variants/%s/%s.webp", *userID, imgID, presetName)
	}
	return fmt.Sprintf("temp/variants/%s/%s.webp", imgID, presetName)
}

func rawStoragePath(userID *string, objectID, filename string) string {
	if userID != nil && *userID != "" {
		return fmt.Sprintf("users/%s/raw/%s/%s", *userID, objectID, filename)
	}
	return fmt.Sprintf("temp/raw/%s/%s", objectID, filename)
}
