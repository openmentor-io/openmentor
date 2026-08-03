package services

import (
	apperrors "github.com/openmentor-io/openmentor/api/pkg/errors"
	"github.com/openmentor-io/openmentor/api/pkg/imageclass"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/s3storage"
	"go.uber.org/zap"
)

// ErrUploadsUnavailable is returned by the photo paths when the S3 storage
// client is not configured. Production cannot reach this state any more
// (config.validateS3StorageConfig fails startup), but off production the API
// boots without object storage, and a nil client must be refused BEFORE the
// mentor row is written: the registration upload runs in a detached
// goroutine, so a nil dereference there committed the row, told the
// registrant they had succeeded, and then killed the whole process.
var ErrUploadsUnavailable = apperrors.InternalError("profile picture uploads are not configured")

// PhotoRejectedError reports a profile picture the API refuses to store. Reason
// is written for the person who uploaded it — the handlers put it straight into
// the 400 body — and the error wraps apperrors.ErrInvalidInput so the existing
// handler mapping treats it as bad input rather than a server fault.
type PhotoRejectedError struct {
	Reason string
}

func (e *PhotoRejectedError) Error() string { return e.Reason }

func (e *PhotoRejectedError) Unwrap() error { return apperrors.ErrInvalidInput }

// preparedPhoto is a profile picture that has passed every check and been
// classified, from ONE base64 decode and ONE image decode.
type preparedPhoto struct {
	bytes       []byte
	contentType string
	style       string
}

// styleOrDefault is the photo_style to store — the safe default for a
// registration that submitted no picture (nil receiver).
func (p *preparedPhoto) styleOrDefault() string {
	if p == nil {
		return imageclass.StyleFrame
	}
	return p.style
}

// preparePhoto validates and classifies an uploaded profile picture.
//
// The ORDER is the fix: s3storage.ValidateImage ends with a header-only
// geometry check (imageclass.CheckBounds), so a decompression bomb is refused
// in microseconds instead of reaching image.Decode — a crafted 47 KiB PNG
// declaring 40000x40000 allocated ~400 MiB of one contiguous buffer, and
// 190 KiB reached 1.5 GiB against a 512 MiB container. Doing it here also means
// the check runs before the mentor row is written and before anything is stored
// in the bucket, so a rejected image costs no DB write and no S3 object.
func preparePhoto(imageData, contentType string) (*preparedPhoto, error) {
	raw, err := s3storage.DecodeImageData(imageData)
	if err != nil {
		return nil, &PhotoRejectedError{Reason: "The image could not be read — please re-upload the file"}
	}
	if err := s3storage.ValidateImage(raw, contentType); err != nil {
		return nil, &PhotoRejectedError{Reason: err.Error()}
	}

	// Classification is cosmetic and the geometry is already bounded, so a
	// decode failure here only costs the display style.
	style, err := imageclass.ClassifyBytes(raw)
	if err != nil {
		logger.Warn("Failed to classify profile picture style, defaulting to frame", zap.Error(err))
		style = imageclass.StyleFrame
	}

	return &preparedPhoto{bytes: raw, contentType: contentType, style: style}, nil
}
