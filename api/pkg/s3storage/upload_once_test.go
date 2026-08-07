package s3storage

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
	"github.com/openmentor-io/openmentor/api/test/imagefixture"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// How many objects an upload writes is only visible from the other side of the
// wire, so these tests put a recording S3 endpoint there. It answers every
// request with success, which is all the SDK needs from PutObject and
// DeleteObject.

// storageTestTelemetry stands in for main(): the upload paths record metrics and
// log, and both are nil until initialized.
func storageTestTelemetry() {
	logger.Log = zap.NewNop()
	metrics.Init("s3storage-test")
}

type recordedRequest struct {
	method string
	path   string
	body   int
}

type recordingBucket struct {
	mu       sync.Mutex
	requests []recordedRequest
}

func (b *recordingBucket) add(r recordedRequest) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.requests = append(b.requests, r)
}

func (b *recordingBucket) byMethod(method string) []recordedRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []recordedRequest
	for _, r := range b.requests {
		if r.method == method {
			out = append(out, r)
		}
	}
	return out
}

// clientFor points a StorageClient at an in-process bucket. Path-style
// addressing keeps the request paths as /{bucket}/{key} rather than a
// virtual-host name no test server could resolve.
func clientFor(t *testing.T, handler http.HandlerFunc) *StorageClient {
	t.Helper()
	storageTestTelemetry()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &StorageClient{
		s3Client: s3.New(s3.Options{
			Region:       "auto",
			BaseEndpoint: aws.String(server.URL),
			UsePathStyle: true,
			Credentials:  credentials.NewStaticCredentialsProvider("access-key", "secret-key", ""),
		}),
		bucketName: "mentor-images",
		endpoint:   server.URL,
	}
}

// recordingClient is a StorageClient whose bucket accepts everything and
// remembers what it was asked to do.
func recordingClient(t *testing.T) (*StorageClient, *recordingBucket) {
	t.Helper()

	bucket := &recordingBucket{}
	client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		body := 0
		if r.Body != nil {
			buf := make([]byte, 32*1024)
			for {
				n, err := r.Body.Read(buf)
				body += n
				if err != nil {
					break
				}
			}
		}
		bucket.add(recordedRequest{method: r.Method, path: r.URL.Path, body: body})
		w.WriteHeader(http.StatusOK)
	})
	return client, bucket
}

// TestUploadProfileImageStoresOneObject is the regression test for C5: the same
// bytes used to be PUT once per size, so a 6 MB photo cost 18 MB of upload and
// three stored copies of one image.
func TestUploadProfileImageStoresOneObject(t *testing.T) {
	client, bucket := recordingClient(t)
	photo := imagefixture.PhotoPNG(t, 800, 600)

	url, err := client.UploadProfileImage(t.Context(), photo, "mentor-uuid", "image/png")
	require.NoError(t, err)

	puts := bucket.byMethod(http.MethodPut)
	require.Len(t, puts, 1, "one photo is one object; got %d PUTs", len(puts))
	assert.Equal(t, "/mentor-images/mentor-uuid/full", puts[0].path)
	assert.Equal(t, len(photo), puts[0].body, "the stored object must be the photo, whole")

	// The returned URL is what the DB row and web/ resolve to, so it has to stay
	// the canonical key.
	assert.Equal(t, client.endpoint+"/mentor-images/mentor-uuid/full", url)
}

// TestUploadProfileImageClearsSupersededAliases: a mentor who replaces their
// photo must not leave the previous one readable under the alias keys nothing
// overwrites any more.
func TestUploadProfileImageClearsSupersededAliases(t *testing.T) {
	client, bucket := recordingClient(t)

	_, err := client.UploadProfileImage(t.Context(), imagefixture.PhotoPNG(t, 800, 600), "mentor-uuid", "image/png")
	require.NoError(t, err)

	deleted := make([]string, 0, 2)
	for _, r := range bucket.byMethod(http.MethodDelete) {
		deleted = append(deleted, r.path)
	}
	assert.ElementsMatch(t,
		[]string{"/mentor-images/mentor-uuid/large", "/mentor-images/mentor-uuid/small"},
		deleted,
		"every non-canonical size in imageSizes must be cleared")
}

// TestSupersededAliasFailureStillReturnsTheUpload: the cleanup is housekeeping.
// A bucket that refuses the DELETE leaves a stale alias behind — it must not
// cost the mentor the photo they just uploaded.
func TestSupersededAliasFailureStillReturnsTheUpload(t *testing.T) {
	client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	url, err := client.UploadProfileImage(t.Context(), imagefixture.PhotoPNG(t, 400, 400), "mentor-uuid", "image/png")
	require.NoError(t, err)
	assert.Equal(t, client.endpoint+"/mentor-images/mentor-uuid/full", url)
}
