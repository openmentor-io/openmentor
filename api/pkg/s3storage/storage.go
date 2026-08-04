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
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/openmentor-io/openmentor/api/pkg/imageclass"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
	"github.com/openmentor-io/openmentor/api/pkg/safego"
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

// DecodeImageData decodes a base64-encoded image string, handling both raw
// base64 and data URI format (data:image/png;base64,...).
//
// Callers decode ONCE and pass the bytes down (ValidateImage,
// UploadImageAllSizesBytes): threading the string around meant the same
// payload was base64-decoded five to six times per upload — once for the size
// check, once for the content sniff, and once per stored size.
func DecodeImageData(imageData string) ([]byte, error) {
	if strings.HasPrefix(imageData, "data:") {
		parts := strings.SplitN(imageData, ",", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid data URI format")
		}
		return base64.StdEncoding.DecodeString(parts[1])
	}
	return base64.StdEncoding.DecodeString(imageData)
}

// UploadImageBytes uploads already-decoded image bytes.
func (s *StorageClient) UploadImageBytes(ctx context.Context, imageBytes []byte, key, contentType string) (string, error) {
	start := time.Now()
	operation := "uploadImage"

	// Upload to the S3-compatible object storage
	_, err := s.s3Client.PutObject(ctx, &s3.PutObjectInput{
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

	return s.publicURL(key), nil
}

// publicURL is the address a stored object is served from:
// {endpoint}/{bucket}/{key}. Split out so the format has exactly one
// definition — a test that rebuilds it inline proves nothing about the URL
// callers actually get.
func (s *StorageClient) publicURL(key string) string {
	return fmt.Sprintf("%s/%s/%s", s.endpoint, s.bucketName, key)
}

// ValidateImageType validates the declared image content type.
func ValidateImageType(contentType string) error {
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

// ValidateImageContent checks the magic bytes of an image payload.
// SECURITY: the client-supplied Content-Type is untrusted; without a
// magic-byte check an attacker could store arbitrary bytes under an image MIME
// (L4). jpeg/png/webp are what the exporter accepts.
func ValidateImageContent(imageBytes []byte) error {
	// http.DetectContentType only reads the first 512 bytes (the sniff window),
	// which is why it cannot see a decompression bomb — see ValidateImage.
	detected := http.DetectContentType(imageBytes)
	allowed := map[string]bool{
		"image/jpeg": true,
		"image/png":  true,
		"image/webp": true,
	}
	if !allowed[detected] {
		return fmt.Errorf("file content is not a supported image (detected %q)", detected)
	}

	return nil
}

// maxImageBytes caps the COMPRESSED payload. It bounds transfer and storage
// only: compressed size says nothing about decoded size (a few KB can declare
// gigapixels), so imageclass.CheckBounds is the real memory guard.
const maxImageBytes = 10 * 1024 * 1024 // 10MB

// ValidateImageSize validates the compressed payload size.
func ValidateImageSize(imageBytes []byte) error {
	if len(imageBytes) > maxImageBytes {
		return fmt.Errorf("file too large: %d bytes (max %d bytes)", len(imageBytes), maxImageBytes)
	}
	return nil
}

// ValidateImage runs every check an uploaded image must pass, cheapest first:
// the declared content type, the compressed byte size, the sniffed magic bytes
// and finally the header-only geometry bound. All of it happens before any
// pixel is decoded and before a single byte reaches the bucket.
func ValidateImage(imageBytes []byte, contentType string) error {
	if err := ValidateImageType(contentType); err != nil {
		return err
	}
	if err := ValidateImageSize(imageBytes); err != nil {
		return err
	}
	if err := ValidateImageContent(imageBytes); err != nil {
		return err
	}
	return imageclass.CheckBounds(imageBytes)
}

// UploadImageAllSizesBytes stores the same validated payload under each size.
// NOTE: Currently uploads same image 3 times (tech debt - future: generate thumbnails)
// Returns the URL of the 'full' size image.
// keyBase is the immutable mentor UUID (mentors.id) — NOT the slug: usernames are
// user-changeable, and keying objects on the UUID makes renames free (no S3 moves).
func (s *StorageClient) UploadImageAllSizesBytes(ctx context.Context, imageBytes []byte, keyBase, contentType string) (string, error) {
	if err := ValidateImage(imageBytes, contentType); err != nil {
		return "", err
	}

	sizes := []string{"full", "large", "small"}
	var fullImageURL string

	for _, size := range sizes {
		// Generate key: {keyBase}/{size} (e.g., "<mentor-uuid>/full")
		key := fmt.Sprintf("%s/%s", keyBase, size)

		// Upload to object storage
		imageURL, err := s.UploadImageBytes(ctx, imageBytes, key, contentType)
		if err != nil {
			return "", fmt.Errorf("failed to upload image size %s: %w", size, err)
		}

		// Store the 'full' URL to return
		if size == "full" {
			fullImageURL = imageURL
		}

		logger.Info("Uploaded image size to storage",
			zap.String("key_base", keyBase),
			zap.String("size", size),
			zap.String("url", imageURL))
	}

	return fullImageURL, nil
}

// UploadImageAllSizesAsync uploads the same image in 3 sizes (full, large, small) asynchronously
// NOTE: Currently uploads same image 3 times (tech debt - future: generate thumbnails)
// This is non-blocking and returns immediately. Errors are logged but not returned.
// Use this when you don't need to wait for upload completion (e.g., during registration).
// Objects are keyed by the mentor UUID (see UploadImageAllSizesBytes).
//
// Takes decoded bytes: the caller has already validated and classified them
// (services.preparePhoto), and a background goroutine is the last place that
// should be discovering an image is unacceptable.
func (s *StorageClient) UploadImageAllSizesAsync(ctx context.Context, imageBytes []byte, contentType, mentorID string) {
	// Detach from the HTTP request context so the upload isn't canceled
	// when the handler returns the response to the client. safego.Go, not a
	// bare goroutine: nothing here is allowed to take the process down, and
	// the caller's recover() (RecoveryMiddleware) cannot reach this stack.
	bgCtx := context.WithoutCancel(ctx)
	safego.Go("s3_upload_image_all_sizes", func() {
		fullImageURL, err := s.UploadImageAllSizesBytes(bgCtx, imageBytes, mentorID, contentType)
		if err != nil {
			logger.Error("Failed to upload profile picture asynchronously",
				zap.Error(err),
				zap.String("mentor_id", mentorID))
		} else {
			logger.Info("Profile picture uploaded successfully during registration",
				zap.String("mentor_id", mentorID),
				zap.String("full_image_url", fullImageURL))
		}
	})
}
