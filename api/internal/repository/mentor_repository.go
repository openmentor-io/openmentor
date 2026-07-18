package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openmentor-io/openmentor/api/internal/cache"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/slug"
	"go.uber.org/zap"
)

// MentorRepository handles mentor data access with PostgreSQL
type MentorRepository struct {
	pool               *pgxpool.Pool
	mentorCache        *cache.MentorCache
	tagsCache          *cache.TagsCache
	disableMentorCache bool
}

// NewMentorRepository creates a new PostgreSQL-based mentor repository
func NewMentorRepository(pool *pgxpool.Pool, mentorCache *cache.MentorCache, tagsCache *cache.TagsCache, disableMentorCache bool) *MentorRepository {
	return &MentorRepository{
		pool:               pool,
		mentorCache:        mentorCache,
		tagsCache:          tagsCache,
		disableMentorCache: disableMentorCache,
	}
}

// GetAll retrieves all mentors with optional filtering
func (r *MentorRepository) GetAll(ctx context.Context, opts models.FilterOptions) ([]*models.Mentor, error) {
	var mentors []*models.Mentor
	var err error

	// Experimental: bypass cache if disabled
	if r.disableMentorCache {
		logger.Debug("Cache disabled, fetching mentors from database")
		mentors, err = r.FetchAllMentorsFromDB(ctx)
		if err != nil {
			logger.Error("Failed to fetch mentors from database",
				zap.Error(err))
			return nil, err
		}
		logger.Debug("Successfully fetched mentors from database",
			zap.Int("count", len(mentors)))
	} else {
		// ForceRefresh triggers background refresh but returns current data
		if opts.ForceRefresh {
			mentors, err = r.mentorCache.ForceRefresh()
		} else {
			mentors, err = r.mentorCache.Get()
		}

		if err != nil {
			return nil, err
		}
	}

	// Apply filters
	filtered := r.applyFilters(mentors, opts)

	return filtered, nil
}

// GetByID retrieves a mentor by legacy numeric ID
// Note: O(n) complexity is acceptable as per requirements
func (r *MentorRepository) GetByID(ctx context.Context, id int, opts models.FilterOptions) (*models.Mentor, error) {
	mentors, err := r.GetAll(ctx, opts)
	if err != nil {
		return nil, err
	}

	for _, mentor := range mentors {
		if mentor.LegacyID == id {
			return mentor, nil
		}
	}

	return nil, fmt.Errorf("mentor with ID %d not found", id)
}

// GetBySlug retrieves a mentor by slug with O(1) complexity
func (r *MentorRepository) GetBySlug(ctx context.Context, mentorSlug string, opts models.FilterOptions) (*models.Mentor, error) {
	var mentor *models.Mentor
	var err error

	// Experimental: bypass cache if disabled
	if r.disableMentorCache {
		mentor, err = r.FetchSingleMentorFromDB(ctx, mentorSlug)
		if err != nil {
			return nil, err
		}
	} else {
		// Note: ForceRefresh is ignored for single lookups
		// Only webhook/profile updates trigger single-mentor refresh
		mentor, err = r.mentorCache.GetBySlug(mentorSlug)
		if err != nil {
			return nil, err
		}
	}

	// Apply filters to single mentor
	filtered := r.applySingleMentorFilters(mentor, opts)
	if filtered == nil {
		return nil, fmt.Errorf("mentor with slug %s not found or not visible", mentorSlug)
	}

	return filtered, nil
}

// GetByMentorId retrieves a mentor by UUID
// First tries cache (active mentors only), then falls back to database query
func (r *MentorRepository) GetByMentorId(ctx context.Context, mentorId string, opts models.FilterOptions) (*models.Mentor, error) {
	// Try cache first (contains only active mentors)
	mentors, err := r.GetAll(ctx, opts)
	if err != nil {
		return nil, err
	}

	for _, mentor := range mentors {
		if mentor.MentorID == mentorId {
			return mentor, nil
		}
	}

	// Fallback to DB query for inactive mentors or mentors not in cache
	mentor, err := r.fetchMentorByUUIDFromDB(ctx, mentorId)
	if err != nil {
		return nil, fmt.Errorf("mentor with ID %s not found", mentorId)
	}

	// Apply filters to the fetched mentor
	filtered := r.applySingleMentorFilters(mentor, opts)
	if filtered == nil {
		return nil, fmt.Errorf("mentor with ID %s not found or filtered out", mentorId)
	}

	return filtered, nil
}

// fetchMentorByUUIDFromDB retrieves a single mentor by UUID from PostgreSQL
func (r *MentorRepository) fetchMentorByUUIDFromDB(ctx context.Context, mentorId string) (*models.Mentor, error) {
	query := `
		SELECT m.id, m.airtable_id, m.legacy_id, m.slug, m.name, m.job_title, m.workplace,
			m.about, m.details, m.competencies, m.experience, m.price, m.status,
			COALESCE(array_to_string(array_agg(t.name), ','), '') as tags,
			m.calendar_url, m.sort_order, m.created_at, m.updated_at,
			COALESCE(
				(SELECT COUNT(*)
				 FROM client_requests cr
				 WHERE cr.mentor_id = m.id
				 AND cr.status = 'done'),
				0
			) AS mentee_count,
			m.photo_style, m.moderation_note
		FROM mentors m
		LEFT JOIN mentor_tags mt ON mt.mentor_id = m.id
		LEFT JOIN tags t ON t.id = mt.tag_id
		WHERE m.id = $1
		GROUP BY m.id
	`

	row := r.pool.QueryRow(ctx, query, mentorId)
	return models.ScanMentor(row)
}

// allowedUpdateColumns defines the columns that can be updated via the Update method
var allowedUpdateColumns = map[string]bool{
	"name":              true,
	"email":             true,
	"job_title":         true,
	"workplace":         true,
	"about":             true,
	"details":           true,
	"competencies":      true,
	"experience":        true,
	"price":             true,
	"preferred_contact": true,
	"calendar_url":      true,
	"slug":              true,
	"status":            true,
	"photo_style":       true,
	"updated_at":        true,
}

// Update updates a mentor in PostgreSQL
func (r *MentorRepository) Update(ctx context.Context, mentorId string, updates map[string]interface{}) error {
	// Validate all keys against allowlist to prevent SQL injection
	for key := range updates {
		if !allowedUpdateColumns[key] {
			return fmt.Errorf("invalid column name: %s", key)
		}
	}

	// Build dynamic UPDATE query
	// This is simplified - in production you'd want proper query building
	query := `UPDATE mentors SET `
	args := []interface{}{}
	argPos := 1

	for key, value := range updates {
		if argPos > 1 {
			query += ", "
		}
		query += fmt.Sprintf("%s = $%d", key, argPos)
		args = append(args, value)
		argPos++
	}

	query += fmt.Sprintf(", updated_at = NOW() WHERE id = $%d", argPos)
	args = append(args, mentorId)

	_, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update mentor: %w", err)
	}

	// Note: Cache will auto-refresh after TTL expires
	return nil
}

// CreateMentor creates a new mentor record in PostgreSQL
// Returns: mentorId (UUID), legacyId (int), error
// Note: slug is generated automatically using pre-fetched legacy_id
func (r *MentorRepository) CreateMentor(ctx context.Context, fields map[string]interface{}) (string, int, string, error) {
	// Begin transaction to ensure atomicity
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", 0, "", fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		// Rollback is safe to call even after Commit
		_ = tx.Rollback(ctx) //nolint:errcheck
	}()

	// Pre-fetch the next legacy_id from the sequence
	var nextLegacyID int
	err = tx.QueryRow(ctx, "SELECT nextval('mentors_legacy_id_seq')").Scan(&nextLegacyID)
	if err != nil {
		return "", 0, "", fmt.Errorf("failed to get next legacy_id: %w", err)
	}

	// Generate slug from name and legacy_id
	name, ok := fields["name"].(string)
	if !ok || name == "" {
		return "", 0, "", fmt.Errorf("name is required")
	}
	mentorSlug := slug.GenerateMentorSlug(name, nextLegacyID)

	// photo_style has a NOT NULL DEFAULT 'frame'; fall back to it when the
	// caller did not classify a profile picture.
	photoStyle, ok := fields["photo_style"].(string)
	if !ok || photoStyle == "" {
		photoStyle = "frame"
	}

	query := `
		INSERT INTO mentors (legacy_id, slug, name, email, job_title, workplace, about, details,
			competencies, experience, price, status, preferred_contact, calendar_url, sort_order,
			photo_style)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		RETURNING id
	`

	var mentorId string

	err = tx.QueryRow(ctx, query,
		nextLegacyID, // Explicit legacy_id
		mentorSlug,   // Generated slug
		fields["name"],
		fields["email"],
		fields["job_title"],
		fields["workplace"],
		fields["about"],
		fields["details"],
		fields["competencies"],
		fields["experience"],
		fields["price"],
		fields["status"],
		fields["preferred_contact"],
		fields["calendar_url"],
		fields["sort_order"],
		photoStyle,
	).Scan(&mentorId)

	if err != nil {
		return "", 0, "", fmt.Errorf("failed to create mentor: %w", err)
	}

	// Commit transaction
	if err = tx.Commit(ctx); err != nil {
		return "", 0, "", fmt.Errorf("failed to commit transaction: %w", err)
	}

	return mentorId, nextLegacyID, mentorSlug, nil
}

// GetTagIDByName retrieves a tag ID by name
func (r *MentorRepository) GetTagIDByName(ctx context.Context, name string) (string, error) {
	return r.tagsCache.GetTagIDByName(name)
}

// UpdateMentorTags updates the tags for a mentor
func (r *MentorRepository) UpdateMentorTags(ctx context.Context, mentorID string, tagIDs []string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		// Rollback is safe to call even after Commit
		// Error is ignored as we prioritize the Commit error
		_ = tx.Rollback(ctx) //nolint:errcheck
	}()

	// Delete existing tags for this mentor
	_, err = tx.Exec(ctx, "DELETE FROM mentor_tags WHERE mentor_id = $1", mentorID)
	if err != nil {
		return fmt.Errorf("failed to delete existing tags: %w", err)
	}

	// Insert new tags
	for _, tagID := range tagIDs {
		_, err = tx.Exec(ctx,
			"INSERT INTO mentor_tags (mentor_id, tag_id) VALUES ($1, $2)",
			mentorID, tagID)
		if err != nil {
			return fmt.Errorf("failed to insert tag: %w", err)
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetAllTags retrieves all tags
func (r *MentorRepository) GetAllTags(ctx context.Context) (map[string]string, error) {
	return r.tagsCache.Get()
}

// GetByEmail retrieves a mentor by email address. Draft and pending
// mentors can log in too (to finish/fix their profile); declined mentors
// stay excluded. When several rows share an email (only active emails are
// unique), the most "advanced" profile wins.
func (r *MentorRepository) GetByEmail(ctx context.Context, email string) (*models.Mentor, error) {
	query := `
		SELECT id, airtable_id, legacy_id, slug, name, job_title, workplace, about, details,
			competencies, experience, price, status, '' as tags, calendar_url,
			sort_order, created_at, updated_at, 0 as mentee_count, photo_style, moderation_note
		FROM mentors
		WHERE email = $1 AND status IN ('active', 'inactive', 'pending', 'draft')
		ORDER BY CASE status
			WHEN 'active' THEN 0
			WHEN 'inactive' THEN 1
			WHEN 'pending' THEN 2
			ELSE 3
		END
		LIMIT 1
	`

	row := r.pool.QueryRow(ctx, query, email)
	return models.ScanMentor(row)
}

// GetByLoginToken retrieves a mentor by login token
// GetByLoginToken finds a mentor by their login token
// Note: Returns the token parameter for backwards compatibility, but it's not used for validation
// The SQL WHERE clause (login_token = $1) is the actual security check
func (r *MentorRepository) GetByLoginToken(ctx context.Context, token string) (*models.Mentor, time.Time, error) {
	query := `
		SELECT id, airtable_id, legacy_id, slug, name, job_title, workplace, about, details,
			competencies, experience, price, status, '' as tags, calendar_url,
			sort_order, created_at, 0 as mentee_count, login_token_expires_at
		FROM mentors
		WHERE login_token = $1
		LIMIT 1
	`

	// SECURITY: tokens are stored hashed (L1); look up by hash.
	row := r.pool.QueryRow(ctx, query, HashLoginToken(token))

	var mentor models.Mentor
	var tagsStr *string
	var airtableID *string
	var job, workplace, about, description, competencies *string
	var experience, price *string
	var calendarURL *string
	var sortOrder *int
	var expiresAt *time.Time

	err := row.Scan(
		&mentor.MentorID,
		&airtableID,
		&mentor.LegacyID,
		&mentor.Slug,
		&mentor.Name,
		&job,
		&workplace,
		&about,
		&description,
		&competencies,
		&experience,
		&price,
		&mentor.Status,
		&tagsStr,
		&calendarURL,
		&sortOrder,
		&mentor.CreatedAt,
		&mentor.MenteeCount,
		&expiresAt,
	)
	if err != nil {
		return nil, time.Time{}, err
	}

	mentor.AirtableID = airtableID
	mentor.SessionsCount = mentor.MenteeCount
	if job != nil {
		mentor.Job = *job
	}
	if workplace != nil {
		mentor.Workplace = *workplace
	}
	if about != nil {
		mentor.About = *about
	}
	if description != nil {
		mentor.Description = *description
	}
	if competencies != nil {
		mentor.Competencies = *competencies
	}
	if experience != nil {
		mentor.Experience = *experience
	}
	if price != nil {
		mentor.Price = *price
	}
	if calendarURL != nil {
		mentor.CalendarURL = *calendarURL
	}
	if sortOrder != nil {
		mentor.SortOrder = *sortOrder
	}
	if expiresAt == nil {
		return nil, time.Time{}, fmt.Errorf("login token has no expiry")
	}

	// Return the token that was used to find this mentor (already validated by SQL query)
	return &mentor, *expiresAt, nil
}

// SetLoginToken sets the login token for a mentor
func (r *MentorRepository) SetLoginToken(ctx context.Context, mentorId string, token string, exp time.Time) error {
	query := `
		UPDATE mentors
		SET login_token = $1, login_token_expires_at = $2, updated_at = NOW()
		WHERE id = $3
	`
	// SECURITY: store the hash, never the plaintext token (L1).
	_, err := r.pool.Exec(ctx, query, HashLoginToken(token), exp, mentorId)
	return err
}

// ClearLoginToken clears the login token for a mentor
func (r *MentorRepository) ClearLoginToken(ctx context.Context, mentorId string) error {
	query := `
		UPDATE mentors
		SET login_token = NULL, login_token_expires_at = NULL, updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, query, mentorId)
	return err
}

// FetchAllMentorsFromDB retrieves all mentors from PostgreSQL for cache population
func (r *MentorRepository) FetchAllMentorsFromDB(ctx context.Context) ([]*models.Mentor, error) {
	query := `
		SELECT m.id, m.airtable_id, m.legacy_id, m.slug, m.name, m.job_title, m.workplace,
			m.about, m.details, m.competencies, m.experience, m.price, m.status,
			COALESCE(array_to_string(array_agg(t.name), ','), '') as tags,
			m.calendar_url, m.sort_order, m.created_at, m.updated_at,
			COALESCE(
				(SELECT COUNT(*)
				 FROM client_requests cr
				 WHERE cr.mentor_id = m.id
				 AND cr.status = 'done'),
				0
			) AS mentee_count,
			m.photo_style, m.moderation_note
		FROM mentors m
		LEFT JOIN mentor_tags mt ON mt.mentor_id = m.id
		LEFT JOIN tags t ON t.id = mt.tag_id
		WHERE m.status = 'active'
		GROUP BY m.id
		ORDER BY m.sort_order
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch mentors: %w", err)
	}

	return models.ScanMentors(rows)
}

// FetchSingleMentorFromDB retrieves a single mentor by slug from PostgreSQL
func (r *MentorRepository) FetchSingleMentorFromDB(ctx context.Context, mentorSlug string) (*models.Mentor, error) {
	query := `
		SELECT m.id, m.airtable_id, m.legacy_id, m.slug, m.name, m.job_title, m.workplace,
			m.about, m.details, m.competencies, m.experience, m.price, m.status,
			COALESCE(array_to_string(array_agg(t.name), ','), '') as tags,
			m.calendar_url, m.sort_order, m.created_at, m.updated_at,
			COALESCE(
				(SELECT COUNT(*)
				 FROM client_requests cr
				 WHERE cr.mentor_id = m.id
				 AND cr.status = 'done'),
				0
			) AS mentee_count,
			m.photo_style, m.moderation_note
		FROM mentors m
		LEFT JOIN mentor_tags mt ON mt.mentor_id = m.id
		LEFT JOIN tags t ON t.id = mt.tag_id
		WHERE m.slug = $1
		GROUP BY m.id
	`

	row := r.pool.QueryRow(ctx, query, mentorSlug)
	return models.ScanMentor(row)
}

// FetchAllTagsFromDB retrieves all tags from PostgreSQL for cache population
func (r *MentorRepository) FetchAllTagsFromDB(ctx context.Context) (map[string]string, error) {
	query := `SELECT id, name FROM tags ORDER BY name`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch tags: %w", err)
	}
	defer rows.Close()

	tags := make(map[string]string)
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("failed to scan tag: %w", err)
		}
		tags[name] = id
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tags: %w", err)
	}

	return tags, nil
}

// ListForModeration retrieves mentors for moderation tabs, sorted by created_at DESC.
func (r *MentorRepository) ListForModeration(ctx context.Context, statuses []string) ([]models.AdminMentorListItem, error) {
	query := `
		SELECT
			m.id,
			m.legacy_id,
			m.name,
			COALESCE(m.email::text, ''),
			COALESCE(m.preferred_contact, ''),
			COALESCE(m.job_title, ''),
			COALESCE(m.workplace, ''),
			COALESCE(m.price, ''),
			m.status,
			m.created_at
		FROM mentors m
		WHERE m.status = ANY($1)
		ORDER BY m.created_at DESC
	`

	rows, err := r.pool.Query(ctx, query, statuses)
	if err != nil {
		return nil, fmt.Errorf("failed to list mentors for moderation: %w", err)
	}
	defer rows.Close()

	result := make([]models.AdminMentorListItem, 0)
	for rows.Next() {
		var item models.AdminMentorListItem
		if err := rows.Scan(
			&item.MentorID,
			&item.LegacyID,
			&item.Name,
			&item.Email,
			&item.PreferredContact,
			&item.Job,
			&item.Workplace,
			&item.Price,
			&item.Status,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan moderation mentor row: %w", err)
		}
		result = append(result, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating moderation mentors: %w", err)
	}

	return result, nil
}

// GetForModerationByID retrieves extended mentor information for admin moderation UI.
func (r *MentorRepository) GetForModerationByID(ctx context.Context, mentorID string) (*models.AdminMentorDetails, error) {
	query := `
		SELECT
			m.id,
			m.legacy_id,
			m.slug,
			m.name,
			COALESCE(m.email::text, ''),
			COALESCE(m.preferred_contact, ''),
			COALESCE(m.job_title, ''),
			COALESCE(m.workplace, ''),
			COALESCE(m.experience, ''),
			COALESCE(m.price, ''),
			COALESCE(array_remove(array_agg(DISTINCT t.name), NULL), '{}'::text[]) AS tags,
			COALESCE(m.about, ''),
			COALESCE(m.details, ''),
			COALESCE(m.competencies, ''),
			COALESCE(m.calendar_url, ''),
			m.status,
			COALESCE(m.sort_order, 0),
			COALESCE(m.moderation_note, ''),
			m.photo_style,
			m.activated_at,
			m.created_at,
			m.updated_at
		FROM mentors m
		LEFT JOIN mentor_tags mt ON mt.mentor_id = m.id
		LEFT JOIN tags t ON t.id = mt.tag_id
		WHERE m.id = $1
		GROUP BY m.id
	`

	var mentor models.AdminMentorDetails
	var tags []string
	if err := r.pool.QueryRow(ctx, query, mentorID).Scan(
		&mentor.MentorID,
		&mentor.LegacyID,
		&mentor.Slug,
		&mentor.Name,
		&mentor.Email,
		&mentor.PreferredContact,
		&mentor.Job,
		&mentor.Workplace,
		&mentor.Experience,
		&mentor.Price,
		&tags,
		&mentor.About,
		&mentor.Description,
		&mentor.Competencies,
		&mentor.CalendarURL,
		&mentor.Status,
		&mentor.SortOrder,
		&mentor.ModerationNote,
		&mentor.PhotoStyle,
		&mentor.ActivatedAt,
		&mentor.CreatedAt,
		&mentor.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("failed to fetch mentor for moderation: %w", err)
	}

	mentor.Tags = tags
	return &mentor, nil
}

// SetMentorStatus updates a mentor's status. HARD GUARD: a mentor that has
// ever been activated (activated_at IS NOT NULL) can never be moved back
// to 'draft' — the WHERE clause blocks that transition on every write path.
func (r *MentorRepository) SetMentorStatus(ctx context.Context, mentorID, status string) error {
	query := `
		UPDATE mentors
		SET status = $1, updated_at = NOW()
		WHERE id = $2
			AND NOT ($1 = 'draft' AND activated_at IS NOT NULL)
	`
	commandTag, err := r.pool.Exec(ctx, query, status, mentorID)
	if err != nil {
		return fmt.Errorf("failed to update mentor status: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("mentor with ID %s not found (or transition to draft forbidden)", mentorID)
	}
	return nil
}

// ApproveMentorModeration activates a mentor: status 'active', first-time
// activation timestamp (kept on re-approves) and the moderation note from
// any previous 'return' is cleared.
func (r *MentorRepository) ApproveMentorModeration(ctx context.Context, mentorID string) error {
	query := `
		UPDATE mentors
		SET status = 'active',
			activated_at = COALESCE(activated_at, NOW()),
			moderation_note = NULL,
			updated_at = NOW()
		WHERE id = $1
	`
	commandTag, err := r.pool.Exec(ctx, query, mentorID)
	if err != nil {
		return fmt.Errorf("failed to approve mentor: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("mentor with ID %s not found", mentorID)
	}
	return nil
}

// ErrMentorWasActivated is returned when a moderation 'return' is attempted
// on a mentor that has already been active at least once (hard guard).
var ErrMentorWasActivated = fmt.Errorf("mentor has already been activated and cannot be returned to draft")

// ReturnMentorToDraft moves a pending mentor back to 'draft' with the
// reviewer's note. Guarded in SQL: never applies to a mentor that has ever
// been activated.
func (r *MentorRepository) ReturnMentorToDraft(ctx context.Context, mentorID, note string) error {
	query := `
		UPDATE mentors
		SET status = 'draft', moderation_note = $2, updated_at = NOW()
		WHERE id = $1 AND activated_at IS NULL
	`
	commandTag, err := r.pool.Exec(ctx, query, mentorID, note)
	if err != nil {
		return fmt.Errorf("failed to return mentor to draft: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return ErrMentorWasActivated
	}
	return nil
}

// SetEmailConfirmation stores a fresh email confirmation token and expiry.
func (r *MentorRepository) SetEmailConfirmation(ctx context.Context, mentorID, token string, expiresAt time.Time) error {
	query := `
		UPDATE mentors
		SET email_confirmation_token = $1, email_confirmation_expires_at = $2, updated_at = NOW()
		WHERE id = $3
	`
	_, err := r.pool.Exec(ctx, query, token, expiresAt, mentorID)
	if err != nil {
		return fmt.Errorf("failed to set email confirmation token: %w", err)
	}
	return nil
}

// GetByConfirmationToken looks a mentor up by email confirmation token
// (expired tokens included — the caller decides between confirm and
// resend). Returns (nil, nil) when no row matches.
func (r *MentorRepository) GetByConfirmationToken(ctx context.Context, token string) (*models.MentorConfirmation, error) {
	query := `
		SELECT id, name, COALESCE(email::text, ''), status,
			COALESCE(email_confirmation_expires_at, to_timestamp(0))
		FROM mentors
		WHERE email_confirmation_token = $1
		LIMIT 1
	`

	var mc models.MentorConfirmation
	err := r.pool.QueryRow(ctx, query, token).Scan(
		&mc.MentorID, &mc.Name, &mc.Email, &mc.Status, &mc.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to fetch mentor by confirmation token: %w", err)
	}
	return &mc, nil
}

// ConfirmMentorEmail finishes email confirmation: draft -> pending, the
// single-use token is cleared.
func (r *MentorRepository) ConfirmMentorEmail(ctx context.Context, mentorID string) error {
	query := `
		UPDATE mentors
		SET status = 'pending',
			email_confirmation_token = NULL,
			email_confirmation_expires_at = NULL,
			updated_at = NOW()
		WHERE id = $1
	`
	commandTag, err := r.pool.Exec(ctx, query, mentorID)
	if err != nil {
		return fmt.Errorf("failed to confirm mentor email: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("mentor with ID %s not found", mentorID)
	}
	return nil
}

// applyFilters applies filtering options to a mentor list
func (r *MentorRepository) applyFilters(mentors []*models.Mentor, opts models.FilterOptions) []*models.Mentor {
	result := make([]*models.Mentor, 0, len(mentors))

	for _, mentor := range mentors {
		filtered := r.applySingleMentorFilters(mentor, opts)
		if filtered != nil {
			result = append(result, filtered)
		}
	}

	return result
}

// applySingleMentorFilters applies filtering options to a single mentor
// Returns nil if mentor should be filtered out
func (r *MentorRepository) applySingleMentorFilters(mentor *models.Mentor, opts models.FilterOptions) *models.Mentor {
	// Filter out mentors with non-public statuses — only 'active' and
	// 'inactive' are valid on the public side of the app (draft/pending/
	// declined are visible only to their owner via AllowAnyStatus, which is
	// set exclusively by session-authenticated own-profile flows).
	if !opts.AllowAnyStatus && mentor.Status != "active" && mentor.Status != "inactive" {
		return nil
	}

	// Filter by visibility
	if opts.OnlyVisible && !mentor.IsVisible {
		return nil
	}

	// Only copy if modifications are needed
	if opts.DropLongFields || !opts.ShowHidden {
		m := *mentor // Copy only when necessary

		if opts.DropLongFields {
			m.About = ""
			m.Description = ""
		}

		if !opts.ShowHidden {
			m.CalendarURL = ""
			m.ModerationNote = ""
		}

		return &m
	}

	// Return original pointer if no modifications needed
	return mentor
}

// TouchUpdatedAt sets updated_at = NOW() for the given mentor without changing any other fields
func (r *MentorRepository) TouchUpdatedAt(ctx context.Context, mentorID string) error {
	_, err := r.pool.Exec(ctx, `UPDATE mentors SET updated_at = NOW() WHERE id = $1`, mentorID)
	return err
}

// InvalidateCache forces cache invalidation
func (r *MentorRepository) InvalidateCache() {
	r.mentorCache.Clear()
}

// UpdateSingleMentorCache updates a single mentor in cache
// Called by webhook or profile update flow
func (r *MentorRepository) UpdateSingleMentorCache(mentorSlug string) error {
	return r.mentorCache.UpdateSingleMentor(mentorSlug)
}

// RemoveMentorFromCache removes a mentor from cache
// Called when a mentor is deleted
func (r *MentorRepository) RemoveMentorFromCache(mentorSlug string) error {
	return r.mentorCache.RemoveMentor(mentorSlug)
}

// RefreshCache triggers a background cache refresh
func (r *MentorRepository) RefreshCache() error {
	_, err := r.mentorCache.ForceRefresh()
	return err
}
