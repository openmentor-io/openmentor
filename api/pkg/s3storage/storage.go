// Package s3storage provides an S3-compatible object storage client for
// profile pictures. It works with any S3-compatible provider (AWS S3,
// Cloudflare R2, Backblaze B2, ...) — the provider is selected purely via
// the configured endpoint and region.
package s3storage

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/openmentor-io/openmentor-api/pkg/logger"
	"github.com/openmentor-io/openmentor-api/pkg/metrics"
	"go.uber.org/zap"
)

// StorageClient represents an S3-compatible object storage client
type StorageClient struct {
	s3Client   *s3.Client
	bucketName string
	endpoint   string
}

// NewStorageClient creates a new S3-compatible object storage client.
// The endpoint is required and determines the provider (e.g.
// https://<account>.r2.cloudflarestorage.com for Cloudflare R2,
// https://s3.<region>.amazonaws.com for AWS S3). The region is required by
// the AWS SDK; use "auto" for providers that don't use regions (e.g. R2).
func NewStorageClient(accessKeyID, secretAccessKey, bucketName, endpoint, region string) (*StorageClient, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("S3_STORAGE_ENDPOINT is required (any S3-compatible endpoint, e.g. R2/S3/B2)")
	}
	if region == "" {
		return nil, fmt.Errorf("S3_STORAGE_REGION is required (use \"auto\" for providers without regions, e.g. R2)")
	}

	// Create S3 client configured for the S3-compatible endpoint
	s3Client := s3.New(s3.Options{
		Region:       region,
		BaseEndpoint: aws.String(endpoint),
		Credentials: credentials.NewStaticCredentialsProvider(
			accessKeyID,
			secretAccessKey,
			"", // session token not needed
		),
	})

	logger.Info("S3 object storage client initialized",
		zap.String("bucket", bucketName),
		zap.String("endpoint", endpoint),
		zap.String("region", region),
	)

	return &StorageClient{
		s3Client:   s3Client,
		bucketName: bucketName,
		endpoint:   endpoint,
	}, nil
}

// decodeBase64Image decodes a base64-encoded image string, handling both raw base64
// and data URI format (data:image/png;base64,...). Returns the decoded bytes.
func decodeBase64Image(imageData string) ([]byte, error) {
	if strings.HasPrefix(imageData, "data:") {
		parts := strings.SplitN(imageData, ",", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid data URI format")
		}
		return base64.StdEncoding.DecodeString(parts[1])
	}
	return base64.StdEncoding.DecodeString(imageData)
}

// UploadImage uploads an image to the S3-compatible object storage
// Returns the public URL of the uploaded image
func (s *StorageClient) UploadImage(ctx context.Context, imageData, key, contentType string) (string, error) {
	start := time.Now()
	operation := "uploadImage"

	// Decode base64 image data
	imageBytes, err := decodeBase64Image(imageData)
	if err != nil {
		metrics.S3StorageRequestDuration.WithLabelValues(operation, "error").Observe(metrics.MeasureDuration(start))
		metrics.S3StorageRequestTotal.WithLabelValues(operation, "error").Inc()
		return "", fmt.Errorf("failed to decode base64 image: %w", err)
	}

	// Upload to the S3-compatible object storage
	_, err = s.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucketName),
		Key:         aws.String(key),
		Body:        bytes.NewReader(imageBytes),
		ContentType: aws.String(contentType),
	})

	duration := metrics.MeasureDuration(start)

	if err != nil {
		metrics.S3StorageRequestDuration.WithLabelValues(operation, "error").Observe(duration)
		metrics.S3StorageRequestTotal.WithLabelValues(operation, "error").Inc()
		logger.LogAPICall(ctx, "s3_storage", operation, "error", duration,
			zap.Error(err),
			zap.String("key", key),
		)
		return "", fmt.Errorf("failed to upload image to storage: %w", err)
	}

	metrics.S3StorageRequestDuration.WithLabelValues(operation, "success").Observe(duration)
	metrics.S3StorageRequestTotal.WithLabelValues(operation, "success").Inc()
	logger.LogAPICall(ctx, "s3_storage", operation, "success", duration,
		zap.String("key", key),
		zap.Int("size_bytes", len(imageBytes)),
	)

	// Construct public URL
	// Format: {endpoint}/{bucket}/{key}
	imageURL := fmt.Sprintf("%s/%s/%s", s.endpoint, s.bucketName, key)

	return imageURL, nil
}

// ValidateImageType validates the image content type
func (s *StorageClient) ValidateImageType(contentType string) error {
	validTypes := map[string]bool{
		"image/jpeg": true,
		"image/jpg":  true,
		"image/png":  true,
		"image/webp": true,
	}

	if !validTypes[strings.ToLower(contentType)] {
		return fmt.Errorf("invalid file type: %s. Allowed types: jpeg, jpg, png, webp", contentType)
	}

	return nil
}

// ValidateImageSize validates the image size (max 10MB)
func (s *StorageClient) ValidateImageSize(imageData string) error {
	const maxSize = 10 * 1024 * 1024 // 10MB

	// Decode to check size
	imageBytes, err := decodeBase64Image(imageData)
	if err != nil {
		return fmt.Errorf("failed to decode image for size validation: %w", err)
	}

	if len(imageBytes) > maxSize {
		return fmt.Errorf("file too large: %d bytes (max %d bytes)", len(imageBytes), maxSize)
	}

	return nil
}

// UploadImageAllSizes uploads the same image in 3 sizes (full, large, small) synchronously
// NOTE: Currently uploads same image 3 times (tech debt - future: generate thumbnails)
// Validates image type and size before uploading. Returns the URL of the 'full' size image
func (s *StorageClient) UploadImageAllSizes(ctx context.Context, imageData, slug, contentType string) (string, error) {
	// Validate image type
	if err := s.ValidateImageType(contentType); err != nil {
		return "", err
	}

	// Validate image size
	if err := s.ValidateImageSize(imageData); err != nil {
		return "", err
	}

	sizes := []string{"full", "large", "small"}
	var fullImageURL string

	for _, size := range sizes {
		// Generate key: {slug}/{size} (e.g., "john-doe/full")
		key := fmt.Sprintf("%s/%s", slug, size)

		// Upload to object storage
		imageURL, err := s.UploadImage(ctx, imageData, key, contentType)
		if err != nil {
			return "", fmt.Errorf("failed to upload image size %s: %w", size, err)
		}

		// Store the 'full' URL to return
		if size == "full" {
			fullImageURL = imageURL
		}

		logger.Info("Uploaded image size to storage",
			zap.String("slug", slug),
			zap.String("size", size),
			zap.String("url", imageURL))
	}

	return fullImageURL, nil
}

// UploadImageAllSizesAsync uploads the same image in 3 sizes (full, large, small) asynchronously
// NOTE: Currently uploads same image 3 times (tech debt - future: generate thumbnails)
// This is non-blocking and returns immediately. Errors are logged but not returned.
// Use this when you don't need to wait for upload completion (e.g., during registration)
func (s *StorageClient) UploadImageAllSizesAsync(ctx context.Context, imageData, slug, contentType, mentorID string) {
	// Detach from the HTTP request context so the upload isn't canceled
	// when the handler returns the response to the client.
	bgCtx := context.WithoutCancel(ctx)
	go func() {
		fullImageURL, err := s.UploadImageAllSizes(bgCtx, imageData, slug, contentType)
		if err != nil {
			logger.Error("Failed to upload profile picture asynchronously",
				zap.Error(err),
				zap.String("mentor_id", mentorID),
				zap.String("slug", slug))
		} else {
			logger.Info("Profile picture uploaded successfully during registration",
				zap.String("mentor_id", mentorID),
				zap.String("slug", slug),
				zap.String("full_image_url", fullImageURL))
		}
	}()
}
