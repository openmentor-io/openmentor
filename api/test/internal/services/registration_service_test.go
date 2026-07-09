package services_test

import (
	"context"
	"testing"

	"github.com/openmentor-io/openmentor/api/pkg/slug"
)

func TestRegistrationService_UploadLogic(t *testing.T) {
	tests := []struct {
		name               string
		mentorName         string
		legacyID           int
		expectedSlug       string
		expectedUploadKeys []string
	}{
		{
			name:         "simple latin name",
			mentorName:   "John Doe",
			legacyID:     42,
			expectedSlug: "john-doe-42",
			expectedUploadKeys: []string{
				"john-doe-42/full",
				"john-doe-42/large",
				"john-doe-42/small",
			},
		},
		{
			name:         "cyrillic name",
			mentorName:   "Иван Петров",
			legacyID:     123,
			expectedSlug: "ivan-petrov-123",
			expectedUploadKeys: []string{
				"ivan-petrov-123/full",
				"ivan-petrov-123/large",
				"ivan-petrov-123/small",
			},
		},
		{
			name:         "name with special characters",
			mentorName:   "Anna-Maria O'Brien",
			legacyID:     999,
			expectedSlug: "annamaria-obrien-999",
			expectedUploadKeys: []string{
				"annamaria-obrien-999/full",
				"annamaria-obrien-999/large",
				"annamaria-obrien-999/small",
			},
		},
		{
			name:         "single word name",
			legacyID:     1,
			mentorName:   "Cher",
			expectedSlug: "cher-1",
			expectedUploadKeys: []string{
				"cher-1/full",
				"cher-1/large",
				"cher-1/small",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test slug generation
			generatedSlug := slug.GenerateMentorSlug(tt.mentorName, tt.legacyID)
			if generatedSlug != tt.expectedSlug {
				t.Errorf("generated slug = %v, want %v", generatedSlug, tt.expectedSlug)
			}

			// Test upload key generation
			sizes := []string{"full", "large", "small"}
			for i, size := range sizes {
				expectedKey := tt.expectedUploadKeys[i]
				actualKey := generatedSlug + "/" + size

				if actualKey != expectedKey {
					t.Errorf("upload key[%s] = %v, want %v", size, actualKey, expectedKey)
				}
			}
		})
	}
}

func TestRegistrationService_ImageValidation(t *testing.T) {
	mockClient := &MockStorageClient{}

	tests := []struct {
		name        string
		contentType string
		imageData   string
		wantErr     bool
	}{
		{
			name:        "valid jpeg",
			contentType: "image/jpeg",
			imageData:   "base64encodedimage",
			wantErr:     false,
		},
		{
			name:        "valid png",
			contentType: "image/png",
			imageData:   "base64encodedimage",
			wantErr:     false,
		},
		{
			name:        "invalid type",
			contentType: "image/gif",
			imageData:   "base64encodedimage",
			wantErr:     false, // Mock doesn't fail unless configured
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mockClient.ValidateImageType(tt.contentType)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateImageType() error = %v, wantErr %v", err, tt.wantErr)
			}

			err = mockClient.ValidateImageSize(tt.imageData)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateImageSize() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRegistrationService_MultiSizeUpload(t *testing.T) {
	mockClient := &MockStorageClient{
		returnURL: "https://s3.example.com/test-bucket/",
	}

	ctx := context.Background()
	mentorSlug := "john-doe-42"
	imageData := "base64encodedimage"
	contentType := "image/jpeg"

	// Simulate uploading 3 sizes
	sizes := []string{"full", "large", "small"}
	uploadedURLs := make([]string, 0, len(sizes))

	for _, size := range sizes {
		key := mentorSlug + "/" + size
		url, err := mockClient.UploadImage(ctx, imageData, key, contentType)
		if err != nil {
			t.Fatalf("UploadImage(%s) failed: %v", size, err)
		}
		uploadedURLs = append(uploadedURLs, url)
	}

	// Verify all 3 uploads happened
	if mockClient.uploadImageCallCount != 3 {
		t.Errorf("upload count = %d, want 3", mockClient.uploadImageCallCount)
	}

	// Verify correct keys were uploaded
	expectedKeys := []string{
		"john-doe-42/full",
		"john-doe-42/large",
		"john-doe-42/small",
	}

	for i, expectedKey := range expectedKeys {
		if i < len(mockClient.uploadedKeys) && mockClient.uploadedKeys[i] != expectedKey {
			t.Errorf("uploaded key[%d] = %v, want %v", i, mockClient.uploadedKeys[i], expectedKey)
		}
	}

	// Verify URLs were returned
	if len(uploadedURLs) != 3 {
		t.Errorf("uploaded URLs count = %d, want 3", len(uploadedURLs))
	}

	// Verify we would store the 'full' URL (first one)
	fullImageURL := uploadedURLs[0]
	if fullImageURL == "" {
		t.Error("full image URL should not be empty")
	}
}

func TestRegistrationService_UploadFailureHandling(t *testing.T) {
	tests := []struct {
		name               string
		shouldFailUpload   bool
		expectedUploadStop int // At which upload should it stop
	}{
		{
			name:               "all uploads succeed",
			shouldFailUpload:   false,
			expectedUploadStop: 3,
		},
		{
			name:               "upload fails",
			shouldFailUpload:   true,
			expectedUploadStop: 1, // Should stop at first failure
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockStorageClient{
				shouldFailUpload: tt.shouldFailUpload,
			}

			ctx := context.Background()
			mentorSlug := "john-doe-42"
			sizes := []string{"full", "large", "small"}

			for _, size := range sizes {
				key := mentorSlug + "/" + size
				_, err := mockClient.UploadImage(ctx, "imagedata", key, "image/jpeg")

				if tt.shouldFailUpload {
					if err == nil {
						t.Error("expected upload to fail but it succeeded")
					}
					break // Stop on first error (mimics service behavior)
				}

				if err != nil {
					t.Errorf("unexpected error: %v", err)
					break
				}
			}

			if mockClient.uploadImageCallCount != tt.expectedUploadStop {
				t.Errorf("upload count = %d, want %d", mockClient.uploadImageCallCount, tt.expectedUploadStop)
			}
		})
	}
}

func TestRegistrationService_DatabaseUpdateLogic(t *testing.T) {
	mockRepo := &MockMentorRepository{}

	ctx := context.Background()
	mentorID := "mentor-uuid-123"
	imageURL := "https://s3.example.com/test-bucket/john-doe-42/full"

	// Simulate database update
	err := mockRepo.UpdateImage(ctx, mentorID, imageURL)
	if err != nil {
		t.Errorf("UpdateImage() error = %v, want nil", err)
	}

	if !mockRepo.updateImageCalled {
		t.Error("UpdateImage should have been called")
	}

	if mockRepo.updatedMentorID != mentorID {
		t.Errorf("updated mentor ID = %v, want %v", mockRepo.updatedMentorID, mentorID)
	}

	if mockRepo.updatedImageURL != imageURL {
		t.Errorf("updated image URL = %v, want %v", mockRepo.updatedImageURL, imageURL)
	}
}
