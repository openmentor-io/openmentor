package services

import (
	apperrors "github.com/openmentor-io/openmentor/api/pkg/errors"
	"github.com/openmentor-io/openmentor/api/pkg/imageclass"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/s3storage"
	"go.uber.org/zap"
)

// ErrUploadsUnavailable is returned by the photo paths when the S3 storage
// client is not configured. Production cannot reach this state
// (config.validateS3StorageConfig fails startup), but off production the API
// boots without object storage, and a nil client has to be refused BEFORE the
// mentor row is written: the registration upload runs in a detached goroutine,
// where a nil dereference takes the process down after the row has committed.
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
// The order matters: s3storage.ValidateImage ends with a header-only geometry
// check (imageclass.CheckBounds), so a decompression bomb is refused in
// microseconds instead of reaching image.Decode, where it costs hundreds of
// megabytes. Running all of it here keeps a rejected image from costing a DB
// write or an S3 object too.
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
