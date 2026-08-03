package s3storage

import (
	"errors"
	"testing"

	"github.com/openmentor-io/openmentor/api/pkg/imageclass"
	"github.com/openmentor-io/openmentor/api/test/imagefixture"
)

// TestValidateImage covers the single validation entry point the upload paths
// use. The bomb case is the one that matters: it passes the content-type check,
// the 10 MB compressed cap and the 512-byte magic sniff — every check that
// existed before — and is only stopped by the geometry bound.
func TestValidateImage(t *testing.T) {
	bomb := imagefixture.BombPNG(t, 10000, 10000)
	photo := imagefixture.PhotoPNG(t, 800, 600)

	if err := ValidateImageSize(bomb); err != nil {
		t.Fatalf("the bomb must pass the compressed-size cap (that is the point): %v", err)
	}
	if err := ValidateImageContent(bomb); err != nil {
		t.Fatalf("the bomb must pass the magic-byte sniff (that is the point): %v", err)
	}

	if err := ValidateImage(bomb, "image/png"); !errors.Is(err, imageclass.ErrImageTooLarge) {
		t.Errorf("ValidateImage(bomb) error = %v, want ErrImageTooLarge", err)
	}
	if err := ValidateImage(photo, "image/png"); err != nil {
		t.Errorf("ValidateImage(photo) error = %v, want nil", err)
	}
	if err := ValidateImage(photo, "image/gif"); err == nil {
		t.Error("ValidateImage() with an unsupported content type must fail")
	}
}

// TestUploadImageAllSizesBytesRejectsBombBeforeStoring: the storage layer is the
// last line of defense — nothing may reach the bucket. A nil s3Client makes the
// point loudly: any PutObject attempt would panic instead of returning an error.
func TestUploadImageAllSizesBytesRejectsBombBeforeStoring(t *testing.T) {
	client := &StorageClient{bucketName: "test-bucket", endpoint: "https://s3.example.com"}

	url, err := client.UploadImageAllSizesBytes(t.Context(), imagefixture.BombPNG(t, 10000, 10000), "mentor-uuid", "image/png")
	if !errors.Is(err, imageclass.ErrImageTooLarge) {
		t.Fatalf("UploadImageAllSizesBytes() error = %v, want ErrImageTooLarge", err)
	}
	if url != "" {
		t.Errorf("UploadImageAllSizesBytes() url = %q, want empty", url)
	}
}
