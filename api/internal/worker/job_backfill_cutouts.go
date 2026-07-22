package worker

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/cutout"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
)

// backfillHeroKey is the object-storage suffix for the generated cutout,
// matching the frontend's hero asset and services.heroSizeKey.
const backfillHeroKey = "hero"

// BackfillCutoutsResult is the JSON summary returned by the backfill endpoint.
type BackfillCutoutsResult struct {
	Total     int `json:"total"`     // mentors considered
	NoPhoto   int `json:"no_photo"`  // no <slug>/full object in storage
	Hero      int `json:"hero"`      // cutout generated + accepted
	Frame     int `json:"frame"`     // rejected by the quality gate
	Errors    int `json:"errors"`    // cutout/storage failures (left unchanged)
	Processed int `json:"processed"` // hero + frame (photo_style written)
}

// BackfillCutouts regenerates hero cut-outs for existing mentors: for each
// public mentor it downloads <slug>/full, removes the background, quality-gates
// the result, and on success uploads <slug>/hero and sets photo_style='hero'
// (else 'frame'). Idempotent and safe to re-run. Intended for migrated mentors
// (whose photos predate the cutout pipeline) and one-off reprocessing.
//
// POST /jobs/backfill-cutouts
func (h *Handlers) BackfillCutouts(c *gin.Context) {
	if h.objects == nil || !h.cutout.Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error":   "cutout backfill not configured (needs S3 + CUTOUT_SERVICE_URL)",
		})
		return
	}

	ctx := c.Request.Context()
	mentors, err := h.repo.ListMentorsForCutout(ctx)
	if err != nil {
		logger.Error("[Backfill Cutouts] Failed to list mentors", zap.Error(err))
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "failed to list mentors"})
		return
	}

	res := BackfillCutoutsResult{Total: len(mentors)}
	for _, m := range mentors {
		style, outcome := h.backfillOne(ctx, m.Slug)
		switch outcome {
		case "no_photo":
			res.NoPhoto++
			continue
		case "error":
			res.Errors++
			continue
		}
		if err := h.repo.SetPhotoStyle(ctx, m.ID, style); err != nil {
			logger.Error("[Backfill Cutouts] Failed to set photo_style",
				zap.String("mentor_id", m.ID), zap.String("slug", m.Slug), zap.Error(err))
			res.Errors++
			continue
		}
		res.Processed++
		if style == cutout.StyleHero {
			res.Hero++
		} else {
			res.Frame++
		}
	}

	logger.Info("[Backfill Cutouts] Done",
		zap.Int("total", res.Total), zap.Int("hero", res.Hero),
		zap.Int("frame", res.Frame), zap.Int("no_photo", res.NoPhoto), zap.Int("errors", res.Errors))
	c.JSON(http.StatusOK, gin.H{"success": true, "result": res})
}

// CutoutMentorResult is the JSON summary returned by the single-mentor endpoint.
type CutoutMentorResult struct {
	MentorID   string `json:"mentor_id"`
	Slug       string `json:"slug"`
	Outcome    string `json:"outcome"`     // ok | no_photo | error
	PhotoStyle string `json:"photo_style"` // hero | frame (empty when not written)
}

// CutoutMentor regenerates the hero cut-out for a SINGLE mentor: it downloads
// <slug>/full, removes the background, quality-gates it, and on success uploads
// <slug>/hero and sets photo_style. Intended for targeted (re)processing of
// specific known mentors — e.g. verifying the pipeline on a handful of profiles
// before a full backfill. Idempotent and safe to re-run.
//
// POST|GET /jobs/cutout-mentor?mentorId=<uuid>
func (h *Handlers) CutoutMentor(c *gin.Context) {
	if h.objects == nil || !h.cutout.Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"error":   "cutout not configured (needs S3 + CUTOUT_SERVICE_URL)",
		})
		return
	}

	mentorID := c.Query("mentorId")
	if mentorID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "mentorId is required"})
		return
	}

	ctx := c.Request.Context()
	mentor, err := h.repo.GetMentorForCutout(ctx, mentorID)
	if err != nil {
		logger.Error("[Cutout Mentor] Failed to fetch mentor", zap.String("mentor_id", mentorID), zap.Error(err))
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "failed to fetch mentor"})
		return
	}
	if mentor == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "mentor not found"})
		return
	}
	if mentor.Slug == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"success": false, "error": "mentor has no slug"})
		return
	}

	style, outcome := h.backfillOne(ctx, mentor.Slug)
	res := CutoutMentorResult{MentorID: mentor.ID, Slug: mentor.Slug, Outcome: outcome}
	if outcome == "ok" {
		if err := h.repo.SetPhotoStyle(ctx, mentor.ID, style); err != nil {
			logger.Error("[Cutout Mentor] Failed to set photo_style",
				zap.String("mentor_id", mentor.ID), zap.String("slug", mentor.Slug), zap.Error(err))
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "failed to store photo_style"})
			return
		}
		res.PhotoStyle = style
	}

	logger.Info("[Cutout Mentor] Done",
		zap.String("mentor_id", mentor.ID), zap.String("slug", mentor.Slug),
		zap.String("outcome", outcome), zap.String("photo_style", res.PhotoStyle))
	c.JSON(http.StatusOK, gin.H{"success": true, "result": res})
}

// backfillOne runs the cutout pipeline for one mentor slug via the shared
// cutout.ProcessImage (which records all cutout metrics). It returns the
// resolved photo_style ("hero"/"frame") and an outcome tag ("ok"/"no_photo"/
// "error"). On "no_photo"/"error" the returned style must be ignored.
func (h *Handlers) backfillOne(ctx context.Context, slug string) (style, outcome string) {
	src, err := h.objects.GetObject(ctx, slug+"/full")
	if err != nil {
		cutout.RecordOutcome(cutout.SourceBackfill, cutout.OutcomeError)
		logger.Warn("[Backfill Cutouts] Failed to fetch source photo",
			zap.String("slug", slug), zap.Error(err))
		return "", "error"
	}
	if src == nil {
		cutout.RecordOutcome(cutout.SourceBackfill, cutout.OutcomeNoPhoto)
		return "", "no_photo"
	}

	upload := func(ctx context.Context, png []byte) error {
		_, uerr := h.objects.UploadObject(ctx, png, slug+"/"+backfillHeroKey, "image/png")
		return uerr
	}
	res := h.cutout.ProcessImage(ctx, cutout.SourceBackfill, src, upload)
	logBackfillResult(slug, &res)
	if res.Outcome == cutout.OutcomeError {
		return "", "error"
	}
	return res.Style, "ok"
}

// logBackfillResult emits one structured log line per cutout attempt, keyed to
// the metric outcome. Errors are logged at Warn (the cutout is best-effort;
// S3 upload failures are additionally logged/metered by the storage client).
func logBackfillResult(slug string, res *cutout.Result) {
	switch res.Outcome {
	case cutout.OutcomeHero:
		logger.Info("[Backfill Cutouts] Hero asset generated",
			zap.String("slug", slug),
			zap.Float64("coverage", res.Gate.Coverage), zap.Float64("dominance", res.Gate.Dominance))
	case cutout.OutcomeFrame:
		logger.Info("[Backfill Cutouts] Rejected by quality gate",
			zap.String("slug", slug), zap.String("reason", res.Gate.Reason),
			zap.Float64("coverage", res.Gate.Coverage), zap.Float64("dominance", res.Gate.Dominance))
	default: // OutcomeError
		logger.Warn("[Backfill Cutouts] Cutout failed",
			zap.String("slug", slug), zap.Error(res.Err))
	}
}
