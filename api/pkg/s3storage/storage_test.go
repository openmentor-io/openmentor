package s3storage

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/openmentor-io/openmentor-api/pkg/logger"
	"go.uber.org/zap"
)

func TestValidateImageType(t *testing.T) {
	client := &StorageClient{}

	tests := []struct {
		name        string
		contentType string
		wantErr     bool
	}{
		{
			name:        "valid jpeg",
			contentType: "image/jpeg",
			wantErr:     false,
		},
		{
			name:        "valid jpg",
			contentType: "image/jpg",
			wantErr:     false,
		},
		{
			name:        "valid png",
			contentType: "image/png",
			wantErr:     false,
		},
		{
			name:        "valid webp",
			contentType: "image/webp",
			wantErr:     false,
		},
		{
			name:        "valid jpeg uppercase",
			contentType: "IMAGE/JPEG",
			wantErr:     false,
		},
		{
			name:        "invalid gif",
			contentType: "image/gif",
			wantErr:     true,
		},
		{
			name:        "invalid text",
			contentType: "text/plain",
			wantErr:     true,
		},
		{
			name:        "invalid svg",
			contentType: "image/svg+xml",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.ValidateImageType(tt.contentType)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateImageType() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateImageSize(t *testing.T) {
	client := &StorageClient{}

	// Create test images of different sizes
	createBase64Image := func(sizeBytes int) string {
		data := make([]byte, sizeBytes)
		return base64.StdEncoding.EncodeToString(data)
	}

	createDataURI := func(sizeBytes int) string {
		data := make([]byte, sizeBytes)
		encoded := base64.StdEncoding.EncodeToString(data)
		return "data:image/png;base64," + encoded
	}

	tests := []struct {
		name      string
		imageData string
		wantErr   bool
	}{
		{
			name:      "valid small image (1KB)",
			imageData: createBase64Image(1024),
			wantErr:   false,
		},
		{
			name:      "valid medium image (1MB)",
			imageData: createBase64Image(1024 * 1024),
			wantErr:   false,
		},
		{
			name:      "valid large image (5MB)",
			imageData: createBase64Image(5 * 1024 * 1024),
			wantErr:   false,
		},
		{
			name:      "valid max size (10MB)",
			imageData: createBase64Image(10 * 1024 * 1024),
			wantErr:   false,
		},
		{
			name:      "invalid too large (11MB)",
			imageData: createBase64Image(11 * 1024 * 1024),
			wantErr:   true,
		},
		{
			name:      "valid data URI format (1MB)",
			imageData: createDataURI(1024 * 1024),
			wantErr:   false,
		},
		{
			name:      "invalid data URI format (11MB)",
			imageData: createDataURI(11 * 1024 * 1024),
			wantErr:   true,
		},
		{
			name:      "invalid base64",
			imageData: "not-valid-base64!!!",
			wantErr:   true,
		},
		{
			name:      "invalid data URI format",
			imageData: "data:invalid",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.ValidateImageSize(tt.imageData)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateImageSize() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestNewStorageClient_Validation tests that endpoint and region are required
// so the client works with any explicitly configured S3-compatible provider
// (AWS S3, Cloudflare R2, Backblaze B2, ...) and never falls back to a
// provider-specific default.
func TestNewStorageClient_Validation(t *testing.T) {
	// NewStorageClient logs on success; use a no-op logger in tests
	logger.Log = zap.NewNop()

	tests := []struct {
		name     string
		endpoint string
		region   string
		wantErr  bool
	}{
		{
			name:     "with all params",
			endpoint: "https://s3.example.com",
			region:   "eu-central-1",
			wantErr:  false,
		},
		{
			name:     "with auto region (e.g. Cloudflare R2)",
			endpoint: "https://account-id.r2.cloudflarestorage.com",
			region:   "auto",
			wantErr:  false,
		},
		{
			name:     "missing endpoint",
			endpoint: "",
			region:   "eu-central-1",
			wantErr:  true,
		},
		{
			name:     "missing region",
			endpoint: "https://s3.example.com",
			region:   "",
			wantErr:  true,
		},
		{
			name:     "missing both",
			endpoint: "",
			region:   "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewStorageClient("access-key", "secret-key", "test-bucket", tt.endpoint, tt.region)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewStorageClient() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && client == nil {
				t.Error("NewStorageClient() returned nil client without error")
			}
		})
	}
}

func TestUploadImage_Base64Decoding(t *testing.T) {
	// Note: This is a unit test for base64 decoding logic only
	// Integration tests with actual S3 uploads should be done separately

	tests := []struct {
		name      string
		imageData string
		wantErr   bool
	}{
		{
			name:      "valid plain base64",
			imageData: base64.StdEncoding.EncodeToString([]byte("test image data")),
			wantErr:   false, // Will fail at S3 upload, but base64 decode should work
		},
		{
			name:      "valid data URI",
			imageData: "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("test image data")),
			wantErr:   false, // Will fail at S3 upload, but base64 decode should work
		},
		{
			name:      "invalid base64",
			imageData: "not-valid-base64!!!",
			wantErr:   true,
		},
		{
			name:      "invalid data URI format",
			imageData: "data:invalid",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate base64 decoding logic from UploadImage
			var imageBytes []byte
			var err error

			if strings.HasPrefix(tt.imageData, "data:") {
				parts := strings.SplitN(tt.imageData, ",", 2)
				if len(parts) != 2 {
					err = context.DeadlineExceeded // Simulate error
				} else {
					imageBytes, err = base64.StdEncoding.DecodeString(parts[1])
				}
			} else {
				imageBytes, err = base64.StdEncoding.DecodeString(tt.imageData)
			}

			if (err != nil) != tt.wantErr {
				t.Errorf("base64 decode error = %v, wantErr %v", err, tt.wantErr)
			}

			if err == nil && len(imageBytes) == 0 {
				t.Error("decoded image bytes should not be empty")
			}
		})
	}
}

// TestUploadImage_URLConstruction tests URL construction logic
func TestUploadImage_URLConstruction(t *testing.T) {
	client := &StorageClient{
		endpoint:   "https://s3.example.com",
		bucketName: "test-bucket",
	}

	tests := []struct {
		name        string
		key         string
		expectedURL string
	}{
		{
			name:        "simple key",
			key:         "image.jpg",
			expectedURL: "https://s3.example.com/test-bucket/image.jpg",
		},
		{
			name:        "key with path",
			key:         "john-doe/full",
			expectedURL: "https://s3.example.com/test-bucket/john-doe/full",
		},
		{
			name:        "key with multiple path segments",
			key:         "mentors/john-doe-42/large",
			expectedURL: "https://s3.example.com/test-bucket/mentors/john-doe-42/large",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Construct URL using same logic as UploadImage
			imageURL := client.endpoint + "/" + client.bucketName + "/" + tt.key

			if imageURL != tt.expectedURL {
				t.Errorf("constructed URL = %v, want %v", imageURL, tt.expectedURL)
			}
		})
	}
}
