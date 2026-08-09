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
// UploadProfileImage): threading the string around meant the same
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

// deleteObject removes one object. S3 treats deleting an absent key as success,
// so callers cannot use this to test existence.
func (s *StorageClient) deleteObject(ctx context.Context, key string) error {
	start := time.Now()
	const operation = "deleteImage"

	_, err := s.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(key),
	})

	duration := metrics.MeasureDuration(start)
	outcome := "success"
	fields := []zap.Field{zap.String("key", key)}
	if err != nil {
		outcome = "error"
		fields = append(fields, zap.Error(err))
	}
	metrics.S3StorageRequestDuration.WithLabelValues(operation, outcome).Observe(duration)
	metrics.S3StorageRequestTotal.WithLabelValues(operation, outcome).Inc()
	logger.LogAPICall(ctx, "s3_storage", operation, outcome, duration, fields...)

	if err != nil {
		return fmt.Errorf("failed to delete object: %w", err)
	}
	return nil
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

// MaxImageBytes caps the COMPRESSED payload. It bounds transfer and storage
// only: compressed size says nothing about decoded size (a few KB can declare
// gigapixels), so imageclass.CheckBounds is the real memory guard.
//
// Exported because it is the number the whole system advertises: the upload
// forms in web/ promise exactly this ("maximum size is 10 MB", see
// web/src/config/uploads.ts), and middleware.MaxImageBodyBytes has to be big
// enough to carry it base64-encoded inside JSON. Those three numbers disagreed
// until TestPhotoAtTheAdvertisedLimitFitsTheImageBodyCap tied them together.
const MaxImageBytes = 10 * 1024 * 1024 // 10 MiB

// ValidateImageSize validates the compressed payload size.
func ValidateImageSize(imageBytes []byte) error {
	if len(imageBytes) > MaxImageBytes {
		return fmt.Errorf("file too large: %d bytes (max %d bytes)", len(imageBytes), MaxImageBytes)
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

// UploadProfileImage validates the payload and stores it as ONE object, at
// {keyBase}/full, whose URL it returns.
//
// It used to PUT the identical bytes three times, under full/large/small (audit
// C5). Those were never variants — no resizing existed, and the code said so —
// so the platform paid three uploads, three times the storage and three times
// the egress to serve three copies of one photo.
//
// The URL scheme is unchanged: full/large/small remain the key layout, web/'s
// image loader resolves every requested size to canonicalImageSize, and when
// real thumbnails exist they come back as *different bytes* under the same keys.
//
// keyBase is the immutable mentor UUID (mentors.id) — NOT the slug: usernames are
// user-changeable, and keying objects on the UUID makes renames free (no S3 moves).
func (s *StorageClient) UploadProfileImage(ctx context.Context, imageBytes []byte, keyBase, contentType string) (string, error) {
	if err := ValidateImage(imageBytes, contentType); err != nil {
		return "", err
	}

	imageURL, err := s.UploadImageBytes(ctx, imageBytes, imageKey(keyBase, canonicalImageSize), contentType)
	if err != nil {
		return "", fmt.Errorf("failed to upload image: %w", err)
	}

	s.deleteSupersededAliases(ctx, keyBase)

	logger.Info("Uploaded profile image to storage",
		zap.String("key_base", keyBase),
		zap.String("size", canonicalImageSize),
		zap.String("url", imageURL))

	return imageURL, nil
}

// deleteSupersededAliases removes the byte-identical copies an earlier upload
// left under the other size keys.
//
// Without it, replacing a photo would leave the PREVIOUS one publicly readable
// at {uuid}/large forever: nothing overwrites those keys any more, and no reader
// asks for them, so nothing would ever notice.
//
// Best effort by design — a failure here costs a stale object (still collected
// by the trash lifecycle when the profile is deleted), and the mentor's new
// photo is already stored. Deleting an absent key is a no-op on S3, so this is
// two cheap requests for accounts that never had aliases, against the two
// multi-megabyte PUTs it replaces.
func (s *StorageClient) deleteSupersededAliases(ctx context.Context, keyBase string) {
	for _, size := range imageSizes {
		if size == canonicalImageSize {
			continue
		}
		key := imageKey(keyBase, size)
		if err := s.deleteObject(ctx, key); err != nil {
			logger.Warn("Could not remove superseded image alias",
				zap.Error(err),
				zap.String("key", key))
		}
	}
}

// UploadProfileImageAsync stores the image without blocking the caller: it
// returns immediately and errors are logged, not returned. Use it when the
// response must not wait for the bucket (e.g. during registration).
// Objects are keyed by the mentor UUID (see UploadProfileImage).
//
// Takes decoded bytes: the caller has already validated and classified them
// (services.preparePhoto), and a background goroutine is the last place that
// should be discovering an image is unacceptable.
func (s *StorageClient) UploadProfileImageAsync(ctx context.Context, imageBytes []byte, contentType, mentorID string) {
	// Detach from the HTTP request context so the upload isn't canceled
	// when the handler returns the response to the client. safego.Go, not a
	// bare goroutine: nothing here is allowed to take the process down, and
	// the caller's recover() (RecoveryMiddleware) cannot reach this stack.
	bgCtx := context.WithoutCancel(ctx)
	safego.Go("s3_upload_profile_image", func() {
		fullImageURL, err := s.UploadProfileImage(bgCtx, imageBytes, mentorID, contentType)
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
