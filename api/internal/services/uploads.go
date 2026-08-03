package services

import (
	apperrors "github.com/openmentor-io/openmentor/api/pkg/errors"
)

// ErrUploadsUnavailable is returned by the photo paths when the S3 storage
// client is not configured. Production cannot reach this state any more
// (config.validateS3StorageConfig fails startup), but off production the API
// boots without object storage, and a nil client must be refused BEFORE the
// mentor row is written: the registration upload runs in a detached
// goroutine, so a nil dereference there committed the row, told the
// registrant they had succeeded, and then killed the whole process.
var ErrUploadsUnavailable = apperrors.InternalError("profile picture uploads are not configured")
