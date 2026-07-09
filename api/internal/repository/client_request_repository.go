package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openmentor-io/openmentor/api/internal/models"
)

// ClientRequestRepository handles client request data access
type ClientRequestRepository struct {
	pool *pgxpool.Pool
}

// NewClientRequestRepository creates a new client request repository
func NewClientRequestRepository(pool *pgxpool.Pool) *ClientRequestRepository {
	return &ClientRequestRepository{
		pool: pool,
	}
}

// Create creates a new client request in PostgreSQL
// Returns: requestID (UUID), error
func (r *ClientRequestRepository) Create(ctx context.Context, req *models.ClientRequest) (string, error) {
	query := `
		INSERT INTO client_requests (mentor_id, email, name, preferred_contact, description, level, status)
		VALUES ($1, $2, $3, $4, $5, $6, 'pending')
		RETURNING id
	`

	var requestID string
	err := r.pool.QueryRow(ctx, query,
		req.MentorID,
		req.Email,
		req.Name,
		req.PreferredContact,
		req.Description,
		req.Level,
	).Scan(&requestID)

	if err != nil {
		return "", fmt.Errorf("failed to create client request: %w", err)
	}

	return requestID, nil
}

// GetByMentor retrieves all client requests for a mentor filtered by statuses
func (r *ClientRequestRepository) GetByMentor(ctx context.Context, mentorId string, statuses []models.RequestStatus) ([]*models.MentorClientRequest, error) {
	query := `
		SELECT cr.id, cr.mentor_id, cr.email, cr.name, cr.preferred_contact, cr.description,
			cr.level, cr.status, cr.created_at, cr.updated_at, cr.status_changed_at,
			cr.scheduled_at, cr.decline_reason, cr.decline_comment,
			r.mentor_review
		FROM client_requests cr
		LEFT JOIN reviews r ON r.client_request_id = cr.id
		WHERE cr.mentor_id = $1 AND cr.status = ANY($2)
		ORDER BY cr.created_at ASC
	`

	// Convert statuses to strings for PostgreSQL array
	statusStrs := make([]string, len(statuses))
	for i, s := range statuses {
		statusStrs[i] = string(s)
	}

	rows, err := r.pool.Query(ctx, query, mentorId, statusStrs)
	if err != nil {
		return nil, fmt.Errorf("failed to get client requests: %w", err)
	}

	return models.ScanClientRequests(rows)
}

// GetByID retrieves a single client request by ID
func (r *ClientRequestRepository) GetByID(ctx context.Context, id string) (*models.MentorClientRequest, error) {
	query := `
		SELECT cr.id, cr.mentor_id, cr.email, cr.name, cr.preferred_contact, cr.description,
			cr.level, cr.status, cr.created_at, cr.updated_at, cr.status_changed_at,
			cr.scheduled_at, cr.decline_reason, cr.decline_comment,
			r.mentor_review
		FROM client_requests cr
		LEFT JOIN reviews r ON r.client_request_id = cr.id
		WHERE cr.id = $1
	`

	row := r.pool.QueryRow(ctx, query, id)
	return models.ScanClientRequest(row)
}

// UpdateStatus updates the status of a client request
func (r *ClientRequestRepository) UpdateStatus(ctx context.Context, id string, status models.RequestStatus) error {
	query := `
		UPDATE client_requests
		SET status = $1, status_changed_at = NOW(), updated_at = NOW()
		WHERE id = $2
	`

	_, err := r.pool.Exec(ctx, query, status, id)
	if err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	return nil
}

// UpdateDecline updates a client request with decline info
func (r *ClientRequestRepository) UpdateDecline(ctx context.Context, id string, reason models.DeclineReason, comment string) error {
	query := `
		UPDATE client_requests
		SET status = 'declined', decline_reason = $1, decline_comment = $2,
			status_changed_at = NOW(), updated_at = NOW()
		WHERE id = $3
	`

	_, err := r.pool.Exec(ctx, query, reason, comment, id)
	if err != nil {
		return fmt.Errorf("failed to update decline: %w", err)
	}

	return nil
}
