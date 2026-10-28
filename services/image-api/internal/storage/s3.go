package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"image-infrastructure-platform/services/image-api/internal/config"
)

// S3Client wraps the AWS S3 SDK client for development-bucket uploads.
type S3Client struct {
	client   *s3.Client
	presign  *s3.PresignClient
	bucket   string
	region   string
	credMode string
}

// NewS3Client builds an S3 client from application configuration.
// Prefer IRSA / default credential chain when static keys are unset.
func NewS3Client(ctx context.Context, cfg *config.Config) (*S3Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.AWS.Region),
	}
	credMode := "default_chain"
	if cfg.AWS.AccessKeyID != "" && cfg.AWS.SecretAccessKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AWS.AccessKeyID,
			cfg.AWS.SecretAccessKey,
			"",
		)))
		credMode = "static"
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg)
	return &S3Client{
		client:   client,
		presign:  s3.NewPresignClient(client),
		bucket:   cfg.AWS.S3DevBucket,
		region:   cfg.AWS.Region,
		credMode: credMode,
	}, nil
}

// CredentialMode reports whether static keys or the default chain (IRSA) was used.
func (c *S3Client) CredentialMode() string {
	if c == nil {
		return ""
	}
	return c.credMode
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

// GetObject retrieves an S3 object body and its content type.
func (c *S3Client) GetObject(ctx context.Context, key string) (io.ReadCloser, string, error) {
	if c == nil || c.client == nil {
		return nil, "", fmt.Errorf("s3 client is not initialized")
	}
	if key == "" {
		return nil, "", fmt.Errorf("object key is required")
	}
	if c.bucket == "" {
		return nil, "", fmt.Errorf("s3 bucket is not configured")
	}

	out, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, "", fmt.Errorf("get object %q: %w", key, err)
	}

	contentType := "application/octet-stream"
	if out.ContentType != nil && *out.ContentType != "" {
		contentType = *out.ContentType
	}

	return out.Body, contentType, nil
}

// ObjectURL returns the HTTPS object URL for a key in the configured bucket.
func (c *S3Client) ObjectURL(key string) string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", c.bucket, c.region, key)
}

// PresignGetURL returns a time-limited HTTPS URL for GetObject.
func (c *S3Client) PresignGetURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if c == nil || c.presign == nil {
		return "", fmt.Errorf("s3 client is not initialized")
	}
	if key == "" {
		return "", fmt.Errorf("object key is required")
	}
	if c.bucket == "" {
		return "", fmt.Errorf("s3 bucket is not configured")
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}

	out, err := c.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("presign get object %q: %w", key, err)
	}
	return out.URL, nil
}

// DeleteObject removes an object from the configured bucket.
// Missing keys are treated as success so cleanup is idempotent.
func (c *S3Client) DeleteObject(ctx context.Context, key string) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("s3 client is not initialized")
	}
	if key == "" {
		return fmt.Errorf("object key is required")
	}
	if c.bucket == "" {
		return fmt.Errorf("s3 bucket is not configured")
	}

	_, err := c.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isS3NotFound(err) {
			return nil
		}
		return fmt.Errorf("delete object %q: %w", key, err)
	}
	return nil
}

func isS3NotFound(err error) bool {
	var noSuchKey *types.NoSuchKey
	if errors.As(err, &noSuchKey) {
		return true
	}
	var notFound *types.NotFound
	if errors.As(err, &notFound) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound", "404":
			return true
		}
	}
	return false
}

// Ready verifies the S3 client can reach the configured bucket.
func (c *S3Client) Ready(ctx context.Context) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("s3 client is not initialized")
	}
	if c.bucket == "" {
		return fmt.Errorf("s3 bucket is not configured")
	}

	_, err := c.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(c.bucket),
	})
	if err != nil {
		return fmt.Errorf("head bucket %q: %w", c.bucket, err)
	}
	return nil
}
