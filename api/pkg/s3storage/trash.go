package s3storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
	"go.uber.org/zap"
)

// TrashPrefix is where a deleted profile's images live during the retention
// window (D70). The API moves them here when a profile is deleted and back when
// it is restored; nothing in this codebase erases them for good. That is the
// point: cmd/worker holds no S3 credential by design (the a57aec2 outage), so
// the nightly purge cannot delete objects — instead a bucket LIFECYCLE RULE
// expires everything under this prefix. A server-side copy resets the object's
// age, so the lifecycle clock starts at deletion, not at upload.
//
// The rule is operator-configured (see infra/.env.example, the retention
// block): expire "deleted/" after MORE days than
// WORKER_PROFILE_PURGE_RETENTION_DAYS, or a restore near the end of the window
// brings the profile back without its photo.
const TrashPrefix = "deleted/"

// imageSizes is every size UploadImageAllSizesBytes stores, and therefore every
// key the trash moves have to cover. One definition, so an added size cannot be
// uploaded but left behind on deletion.
var imageSizes = []string{"full", "large", "small"}

func trashKey(key string) string {
	return TrashPrefix + key
}

// imageKeys returns the object keys one key base (a mentor UUID, or a
// current/retired slug for pre-D29 legacy copies) stores images under.
func imageKeys(keyBase string) []string {
	keys := make([]string, 0, len(imageSizes))
	for _, size := range imageSizes {
		keys = append(keys, fmt.Sprintf("%s/%s", keyBase, size))
	}
	return keys
}

// MoveImagesToTrash moves every image stored under the given key bases into
// TrashPrefix. Called when a profile is deleted (D70).
//
// keyBases is the mentor UUID plus every current and retired slug: the UUID is
// the canonical key since D29, but the rekey deliberately left the old
// slug-keyed copies in place, and an erasure that misses them has not erased
// the photo. The slugs must be captured HERE, at deletion time — the purge
// cascade-deletes mentor_slug_history with the mentor row, so by purge time
// nobody can reconstruct them (the same trap the data-deletion runbook warns
// about).
//
// A missing object is a skip, not an error: most mentors have no legacy copies,
// and some have no photo at all. Objects that do fail to move are logged and
// counted, and the profile deletion has already committed regardless — see
// ProfileService.StashDeletedProfileImages for why that ordering is deliberate.
func (s *StorageClient) MoveImagesToTrash(ctx context.Context, keyBases []string) error {
	var errs []error
	moved := 0
	for _, base := range keyBases {
		for _, key := range imageKeys(base) {
			didMove, err := s.moveObject(ctx, key, trashKey(key))
			if err != nil {
				errs = append(errs, fmt.Errorf("move %s: %w", key, err))
				continue
			}
			if didMove {
				moved++
			}
		}
	}
	logger.Info("Deleted profile images moved to trash",
		zap.Strings("key_bases", keyBases),
		zap.Int("objects_moved", moved),
		zap.Int("failures", len(errs)),
	)
	return errors.Join(errs...)
}

// RestoreImagesFromTrash moves a restored profile's images back out of
// TrashPrefix. Only the UUID keys come back: the legacy slug-keyed copies are
// never read (every reader uses the UUID since D29), so restoring them would
// only re-create the garbage the trash exists to collect.
//
// A missing trash object is a skip: the mentor may never have had a photo, or
// the lifecycle rule already expired it — in which case the profile comes back
// without a picture and the mentor re-uploads, which is the same state as an
// upload that never happened.
func (s *StorageClient) RestoreImagesFromTrash(ctx context.Context, mentorID string) error {
	var errs []error
	restored := 0
	for _, key := range imageKeys(mentorID) {
		didMove, err := s.moveObject(ctx, trashKey(key), key)
		if err != nil {
			errs = append(errs, fmt.Errorf("restore %s: %w", key, err))
			continue
		}
		if didMove {
			restored++
		}
	}
	logger.Info("Restored profile images from trash",
		zap.String("mentor_id", mentorID),
		zap.Int("objects_restored", restored),
		zap.Int("failures", len(errs)),
	)
	return errors.Join(errs...)
}

// moveObject is a server-side copy followed by a delete of the source — S3 has
// no rename. Returns false with no error when the source does not exist.
func (s *StorageClient) moveObject(ctx context.Context, srcKey, dstKey string) (bool, error) {
	start := time.Now()
	const operation = "moveImage"

	_, err := s.s3Client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket: aws.String(s.bucketName),
		// Keys here are UUIDs, slugs and the trash prefix — all URL-safe by
		// construction, so no escaping is needed in the copy source.
		CopySource: aws.String(s.bucketName + "/" + srcKey),
		Key:        aws.String(dstKey),
	})
	if err != nil {
		if isNoSuchKey(err) {
			return false, nil
		}
		s.recordMoveOutcome(ctx, operation, "error", start, srcKey, dstKey, err)
		return false, fmt.Errorf("failed to copy object: %w", err)
	}

	// Delete only after the copy succeeded: a failure here leaves a duplicate
	// (harmless, and still lifecycle-collected if the source is in the trash),
	// never a lost object.
	if _, err := s.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(srcKey),
	}); err != nil {
		s.recordMoveOutcome(ctx, operation, "error", start, srcKey, dstKey, err)
		return false, fmt.Errorf("failed to delete source object after copy: %w", err)
	}

	s.recordMoveOutcome(ctx, operation, "success", start, srcKey, dstKey, nil)
	return true, nil
}

func (s *StorageClient) recordMoveOutcome(ctx context.Context, operation, outcome string, start time.Time, srcKey, dstKey string, err error) {
	duration := metrics.MeasureDuration(start)
	metrics.S3StorageRequestDuration.WithLabelValues(operation, outcome).Observe(duration)
	metrics.S3StorageRequestTotal.WithLabelValues(operation, outcome).Inc()
	fields := []zap.Field{
		zap.String("src_key", srcKey),
		zap.String("dst_key", dstKey),
	}
	if err != nil {
		fields = append(fields, zap.Error(err))
	}
	logger.LogAPICall(ctx, "s3_storage", operation, outcome, duration, fields...)
}

// isNoSuchKey reports whether an S3 error means the source object is absent.
// Checked by code rather than type: CopyObject surfaces the condition as a
// generic API error, and S3-compatible providers (R2, B2) agree on the code
// string, not on the SDK's typed variants.
func isNoSuchKey(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchKey"
}
