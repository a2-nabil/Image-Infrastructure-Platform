package service

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"

	"github.com/chai2010/webp"
	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp"

	"image-infrastructure-platform/services/image-api/internal/model"
)

const variantWebPQuality = 80

type variantPreset struct {
	name string
	fn   func(image.Image) image.Image
}

var variantPresets = []variantPreset{
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
}

// GenerateVariants decodes the raw image, creates preset WebP variants, uploads
// them to S3, and returns variant metadata ready for persistence.
func (s *ImageService) GenerateVariants(ctx context.Context, imgID string, rawReader io.Reader) ([]*model.ImageVariant, error) {
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

	variants := make([]*model.ImageVariant, 0, len(variantPresets))
	for _, preset := range variantPresets {
		resized := preset.fn(src)

		var buf bytes.Buffer
		if err := webp.Encode(&buf, resized, &webp.Options{Quality: variantWebPQuality}); err != nil {
			return nil, fmt.Errorf("encode %s variant: %w", preset.name, err)
		}

		storagePath := fmt.Sprintf("variants/%s/%s.webp", imgID, preset.name)
		if _, err := s.s3.UploadImage(ctx, storagePath, bytes.NewReader(buf.Bytes()), "image/webp"); err != nil {
			return nil, fmt.Errorf("upload %s variant: %w", preset.name, err)
		}

		bounds := resized.Bounds()
		variants = append(variants, &model.ImageVariant{
			ImageID:       imgID,
			PresetName:    preset.name,
			StoragePath:   storagePath,
			MimeType:      "image/webp",
			FileSizeBytes: int64(buf.Len()),
			Width:         bounds.Dx(),
			Height:        bounds.Dy(),
		})
	}

	return variants, nil
}
