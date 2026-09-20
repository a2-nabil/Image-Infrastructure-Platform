package storage

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"image-infrastructure-platform/services/image-api/internal/config"
)

// S3Client wraps the AWS S3 SDK client for development-bucket uploads.
type S3Client struct {
	client *s3.Client
	bucket string
	region string
}

// NewS3Client builds an S3 client from application configuration.
func NewS3Client(ctx context.Context, cfg *config.Config) (*S3Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.AWS.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AWS.AccessKeyID,
			cfg.AWS.SecretAccessKey,
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	return &S3Client{
		client: s3.NewFromConfig(awsCfg),
		bucket: cfg.AWS.S3DevBucket,
		region: cfg.AWS.Region,
	}, nil
}

// UploadImage uploads an object to the configured S3 development bucket and
// returns a public-style object URL for the uploaded key.
func (c *S3Client) UploadImage(ctx context.Context, key string, body io.Reader, contentType string) (string, error) {
	if c == nil || c.client == nil {
		return "", fmt.Errorf("s3 client is not initialized")
	}
	if key == "" {
		return "", fmt.Errorf("object key is required")
	}
	if c.bucket == "" {
		return "", fmt.Errorf("s3 bucket is not configured")
	}

	input := &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Body:   body,
	}
	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}

	if _, err := c.client.PutObject(ctx, input); err != nil {
		return "", fmt.Errorf("put object %q: %w", key, err)
	}

	return c.ObjectURL(key), nil
}

// ObjectURL returns the HTTPS object URL for a key in the configured bucket.
func (c *S3Client) ObjectURL(key string) string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", c.bucket, c.region, key)
}
