package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReviewRepository handles review data access
type ReviewRepository struct {
	pool *pgxpool.Pool
}

// NewReviewRepository creates a new review repository
func NewReviewRepository(pool *pgxpool.Pool) *ReviewRepository {
	return &ReviewRepository{
		pool: pool,
	}
}

// ReviewCheckResult holds the result of checking if a review can be submitted
type ReviewCheckResult struct {
	CanSubmit  bool
	MentorName string
}

// CheckCanSubmitReview checks if a review can be submitted for a given request ID.
// Returns whether the request exists, has status 'done', and has no existing review.
func (r *ReviewRepository) CheckCanSubmitReview(ctx context.Context, requestID string) (*ReviewCheckResult, error) {
	query := `
		SELECT cr.status, m.name as mentor_name,
			EXISTS(SELECT 1 FROM reviews rv WHERE rv.client_request_id = cr.id) as has_review
		FROM client_requests cr
		JOIN mentors m ON m.id = cr.mentor_id
		WHERE cr.id = $1
	`

	var status string
	var mentorName string
	var hasReview bool

	err := r.pool.QueryRow(ctx, query, requestID).Scan(&status, &mentorName, &hasReview)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &ReviewCheckResult{CanSubmit: false}, nil
		}
		return nil, fmt.Errorf("failed to check review eligibility: %w", err)
	}

	if status != "done" {
		return &ReviewCheckResult{CanSubmit: false, MentorName: mentorName}, nil
	}

	if hasReview {
		return &ReviewCheckResult{CanSubmit: false, MentorName: mentorName}, nil
	}

	return &ReviewCheckResult{CanSubmit: true, MentorName: mentorName}, nil
}

// CreateReview creates a new review for a client request.
// Returns the review ID, or an error if the review already exists (unique constraint).
func (r *ReviewRepository) CreateReview(ctx context.Context, requestID, mentorReview, platformReview, improvements string) (string, error) {
	query := `
		INSERT INTO reviews (client_request_id, mentor_review, platform_review, improvements)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`

	var reviewID string
	err := r.pool.QueryRow(ctx, query, requestID, mentorReview, platformReview, improvements).Scan(&reviewID)
	if err != nil {
		// Check for unique constraint violation (review already exists)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", fmt.Errorf("review already exists for this request")
		}
		return "", fmt.Errorf("failed to create review: %w", err)
	}

	return reviewID, nil
}
