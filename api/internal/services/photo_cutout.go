package services

import (
	"context"
	"encoding/base64"
	"strings"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/pkg/cutout"
	"github.com/openmentor-io/openmentor/api/pkg/imageclass"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/s3storage"
	"go.uber.org/zap"
)

// heroSizeKey is the object-storage suffix for the alpha-transparent cutout
// the frontend loads for the 'hero' treatment (alongside full/large/small).
const heroSizeKey = "hero"

// photoStyleUpdater is the subset of the mentor repository the cutout pipeline
// needs. Both the concrete repo and ProfileMentorRepository satisfy it.
type photoStyleUpdater interface {
	Update(ctx context.Context, mentorID string, updates map[string]interface{}) error
}

// resolvePhotoStyle decides a mentor's photo_style for a freshly uploaded
// image. When the cutout service is configured it removes the background,
// quality-gates the result, and on success uploads a <slug>/hero alpha PNG
// and returns "hero". On any cutout failure/rejection it returns "frame".
// When the cutout service is disabled it falls back to the border-luminance
// classifier (pkg/imageclass) so behavior is unchanged without the sidecar.
// Best-effort: never returns an error.
func resolvePhotoStyle(ctx context.Context, cfg *config.Config, sc *s3storage.StorageClient, slug, imageBase64 string) string {
	if strings.TrimSpace(imageBase64) == "" {
		return imageclass.StyleFrame
	}

	cc := cutout.New(cutout.Config{
		ServiceURL:     cfg.Cutout.ServiceURL,
		Model:          cfg.Cutout.Model,
		TimeoutSeconds: cfg.Cutout.TimeoutSeconds,
	})
	if !cc.Enabled() {
		return classifyPhotoStyle(imageBase64)
	}

	imageBytes, err := decodeImageBase64(imageBase64)
	if err != nil {
		cutout.RecordOutcome(cutout.SourceUpload, cutout.OutcomeError)
		logger.Warn("cutout: could not decode image, using frame", zap.String("slug", slug), zap.Error(err))
		return imageclass.StyleFrame
	}

	upload := func(ctx context.Context, png []byte) error {
		_, uerr := sc.UploadObject(ctx, png, slug+"/"+heroSizeKey, "image/png")
		return uerr
	}
	res := cc.ProcessImage(ctx, cutout.SourceUpload, imageBytes, upload)
	logCutoutResult(slug, cutout.SourceUpload, &res)
	return res.Style
}

// logCutoutResult emits one structured log line per cutout attempt, matching
// the outcome recorded in metrics. Errors are logged at Warn: the cutout is
// best-effort and always falls back to the 'frame' treatment (S3 upload
// failures are additionally logged/metered inside the storage client).
func logCutoutResult(slug, source string, res *cutout.Result) {
	switch res.Outcome {
	case cutout.OutcomeHero:
		logger.Info("cutout: hero asset generated",
			zap.String("slug", slug), zap.String("source", source),
			zap.Float64("coverage", res.Gate.Coverage), zap.Float64("dominance", res.Gate.Dominance))
	case cutout.OutcomeFrame:
		logger.Info("cutout: rejected by quality gate, using frame",
			zap.String("slug", slug), zap.String("source", source), zap.String("reason", res.Gate.Reason),
			zap.Float64("coverage", res.Gate.Coverage), zap.Float64("dominance", res.Gate.Dominance))
	default: // OutcomeError
		logger.Warn("cutout: background removal failed, using frame",
			zap.String("slug", slug), zap.String("source", source), zap.Error(res.Err))
	}
}

// applyPhotoStyle resolves and persists photo_style for one mentor. All
// failures are logged; the caller never needs to handle an error (a failed
// cutout must never break an upload or registration).
func applyPhotoStyle(ctx context.Context, cfg *config.Config, sc *s3storage.StorageClient, repo photoStyleUpdater, mentorID, slug, imageBase64 string) string {
	style := resolvePhotoStyle(ctx, cfg, sc, slug, imageBase64)
	if err := repo.Update(ctx, mentorID, map[string]interface{}{"photo_style": style}); err != nil {
		logger.Error("cutout: failed to store photo_style",
			zap.String("mentor_id", mentorID), zap.String("photo_style", style), zap.Error(err))
	}
	return style
}

// decodeImageBase64 decodes raw base64 or a data URI (data:image/...;base64,)
// into image bytes — matching the payload shape the upload endpoints receive.
func decodeImageBase64(imageData string) ([]byte, error) {
	if strings.HasPrefix(imageData, "data:") {
		if parts := strings.SplitN(imageData, ",", 2); len(parts) == 2 {
			imageData = parts[1]
		}
	}
	return base64.StdEncoding.DecodeString(imageData)
}
