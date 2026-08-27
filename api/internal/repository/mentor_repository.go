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

// IsNoRows reports whether err means the query matched nothing, as opposed to
// a row that matched but could not be read. Services that collapse both into
// "not found" report a broken row as a typo'd input — see MentorAuthService.
func IsNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// MentorRepository handles mentor data access with PostgreSQL.
// Mentor reads always hit the database (the previous in-memory mentor cache
// was removed — it was always disabled in production and only added staleness).
// The lightweight tags lookup keeps its own cache.
type MentorRepository struct {
	pool      *pgxpool.Pool
	tagsCache *cache.TagsCache
}

// NewMentorRepository creates a new PostgreSQL-based mentor repository
func NewMentorRepository(pool *pgxpool.Pool, tagsCache *cache.TagsCache) *MentorRepository {
	return &MentorRepository{
		pool:      pool,
		tagsCache: tagsCache,
	}
}

// GetAll retrieves all mentors with optional filtering
func (r *MentorRepository) GetAll(ctx context.Context, opts models.FilterOptions) ([]*models.Mentor, error) {
	mentors, err := r.FetchAllMentorsFromDB(ctx)
	if err != nil {
		logger.Error("Failed to fetch mentors from database", zap.Error(err))
		return nil, err
	}

	// Apply filters
	filtered := r.applyFilters(mentors, opts)

	return filtered, nil
}

// GetByID retrieves a mentor by legacy numeric ID off mentors_legacy_id_uniq.
//
// It used to call GetAll and scan the returned slice: the whole catalog
// aggregate — every active mentor, their tags and a COUNT(*) subquery per row —
// materialized to answer a single-row question, on an endpoint rate-limited at
// 100 r/s (audit C4).
//
// The query keeps GetAll's `status = 'active' AND deleted_at IS NULL`
// predicate rather than resolving any mentor by numeric ID. That is not
// caution for its own sake: because the old implementation read the catalog,
// a draft, pending, declined or inactive mentor was NEVER reachable by legacy
// ID whatever FilterOptions the caller passed, and both callers (the public
// by-id endpoint and the internal one, which takes its options from a request
// body) shipped against that behavior.
func (r *MentorRepository) GetByID(ctx context.Context, id int, opts models.FilterOptions) (*models.Mentor, error) {
	mentor, err := r.fetchMentorByLegacyIDFromDB(ctx, id)
	if err != nil {
		// A dead pool or an unreadable row is not a typo'd URL, and the handler
		// flattens every error here into a 404 — so say so once, here, the way
		// GetAll used to.
		if !IsNoRows(err) {
			logger.Error("Failed to fetch mentor by legacy ID", logger.RedactedError(err), zap.Int("legacy_id", id))
		}
		return nil, fmt.Errorf("mentor with ID %d not found: %w", id, err)
	}

	filtered := r.applySingleMentorFilters(mentor, opts)
	if filtered == nil {
		return nil, fmt.Errorf("mentor with ID %d not found or filtered out", id)
	}

	return filtered, nil
}

// GetBySlug retrieves a mentor by slug directly from the database.
// When the slug is not current, it falls back to mentor_slug_history so
// retired slugs still resolve — the returned mentor carries its CURRENT slug,
// and callers compare it against the requested one to issue a 301 redirect.
func (r *MentorRepository) GetBySlug(ctx context.Context, mentorSlug string, opts models.FilterOptions) (*models.Mentor, error) {
	mentor, err := r.FetchSingleMentorFromDB(ctx, mentorSlug)
	if err != nil {
		// Direct miss: resolve via slug history (old username → redirect).
		entry, histErr := r.GetSlugHistoryBySlug(ctx, mentorSlug)
		if histErr != nil || entry == nil {
			return nil, err
		}
		mentor, err = r.fetchMentorByUUIDFromDB(ctx, entry.MentorID)
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

// GetByMentorId retrieves a mentor by UUID directly from the database.
func (r *MentorRepository) GetByMentorId(ctx context.Context, mentorId string, opts models.FilterOptions) (*models.Mentor, error) {
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

// mentorSelect is the column list and join shape every full mentor read shares,
// in the order models.ScanMentor expects. It exists as ONE constant because it
// used to be pasted into three queries verbatim: sort_order was COALESCEd in all
// three but a nullable column can just as easily be COALESCEd in two of them and
// forgotten in the third, which is a mentor nobody can read.
//
// RULE for anyone adding a column here: mentors is nullable almost everywhere
// and pgx fails the WHOLE row scan on a NULL in a non-pointer destination — so
// every nullable column must either be COALESCEd here or land in a *pointer*
// field of models.Mentor. airtable_id is the only member of the second group
// (nil is meaningful: it marks a natively registered mentor).
// api/internal/repository/mentor_nullable_columns_db_test.go enforces this
// against a real database, per column.
const mentorSelect = `
	SELECT m.id, m.airtable_id, m.legacy_id, m.slug, m.name,
		COALESCE(m.job_title, ''), COALESCE(m.workplace, ''), COALESCE(m.about, ''),
		COALESCE(m.details, ''), COALESCE(m.competencies, ''),
		COALESCE(m.experience, ''), COALESCE(m.price, ''), m.status,
		COALESCE(array_to_string(array_agg(t.name), ','), '') as tags,
		COALESCE(m.calendar_url, ''), COALESCE(m.sort_order, 0),
		m.created_at, m.updated_at,
		-- sessions on OpenMentor + sessions carried over from
		-- getmentor.dev at migration time (D28)
		COALESCE(
			(SELECT COUNT(*)
			 FROM client_requests cr
			 WHERE cr.mentor_id = m.id
			 AND cr.status = 'done'),
			0
		) + m.legacy_sessions_count AS mentee_count,
		m.legacy_sessions_count,
		m.photo_style, COALESCE(m.moderation_note, ''),
		m.deleted_at
	FROM mentors m
	LEFT JOIN mentor_tags mt ON mt.mentor_id = m.id
	LEFT JOIN tags t ON t.id = mt.tag_id
`

// mentorCatalogQuery is the public catalog read: every active, non-deleted
// mentor, tags aggregated, ordered for display.
//
// deleted_at IS NULL is redundant with status = 'active' today (deletion sets
// 'inactive'), and stays anyway: this is the query behind the whole public
// catalog, and it should not depend on a rule enforced elsewhere.
//
// A const rather than a local string so a test can EXPLAIN exactly what
// production runs — the C4 measurement compares this plan against
// mentorByLegacyIDQuery's.
const mentorCatalogQuery = mentorSelect + `
	WHERE m.status = 'active' AND m.deleted_at IS NULL
	GROUP BY m.id
	ORDER BY m.sort_order
`

// mentorByLegacyIDQuery is mentorCatalogQuery narrowed to one row via
// mentors_legacy_id_uniq. Same visibility predicate, so the two answer the same
// question about the same set of mentors — see GetByID.
const mentorByLegacyIDQuery = mentorSelect + `
	WHERE m.legacy_id = $1 AND m.status = 'active' AND m.deleted_at IS NULL
	GROUP BY m.id
`

// fetchMentorByUUIDFromDB retrieves a single mentor by UUID from PostgreSQL
func (r *MentorRepository) fetchMentorByUUIDFromDB(ctx context.Context, mentorId string) (*models.Mentor, error) {
	query := mentorSelect + `
		WHERE m.id = $1
		GROUP BY m.id
	`

	row := r.pool.QueryRow(ctx, query, mentorId)
	return models.ScanMentor(row)
}

// fetchMentorByLegacyIDFromDB retrieves a single mentor by legacy numeric ID.
func (r *MentorRepository) fetchMentorByLegacyIDFromDB(ctx context.Context, id int) (*models.Mentor, error) {
	row := r.pool.QueryRow(ctx, mentorByLegacyIDQuery, id)
	return models.ScanMentor(row)
}

// allowedUpdateColumns defines the columns that can be updated via the Update
// method.
//
// updated_at is deliberately NOT a member: Update always appends
// `updated_at = NOW()` itself, so accepting it from a caller built
// `SET updated_at = $1, updated_at = NOW()`, which Postgres rejects outright
// ("multiple assignments to same column") — a 500 on a save. Rejecting the
// column name says so instead.
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
}

// buildMentorUpdate turns a caller-supplied column map into the one UPDATE every
// mentor write shares. The values are parameterized but the column NAMES are
// concatenated, so allowedUpdateColumns is the only thing between a caller and
// arbitrary SQL — which is why this validation and the query construction live
// together in one place rather than once per write path.
func buildMentorUpdate(mentorId string, updates map[string]interface{}) (string, []interface{}, error) {
	for key := range updates {
		if !allowedUpdateColumns[key] {
			return "", nil, fmt.Errorf("invalid column name: %s", key)
		}
	}

	query := `UPDATE mentors SET `
	args := make([]interface{}, 0, len(updates)+1)
	argPos := 1

	for key, value := range updates {
		if argPos > 1 {
			query += ", "
		}
		query += fmt.Sprintf("%s = $%d", key, argPos)
		args = append(args, value)
		argPos++
	}
	if argPos > 1 {
		query += ", "
	}

	// A deleted profile (D70) accepts no edits, from its owner or from an admin.
	// The services check this first and answer with a proper error; the guard
	// here is what makes "unavailable for any action" true of the WRITE itself,
	// for any caller that reaches a write without checking.
	query += fmt.Sprintf("updated_at = NOW() WHERE id = $%d AND deleted_at IS NULL", argPos)
	args = append(args, mentorId)

	return query, args, nil
}

// Update updates a mentor in PostgreSQL.
//
// An empty updates map is a no-op rather than an error: it used to build
// `UPDATE mentors SET , updated_at = NOW() WHERE ...` — a syntax error — so a
// caller whose diff came out empty got a failed save instead of nothing to do.
//
// Zero affected rows is deliberately NOT an error here (a missing or deleted
// mentor writes nothing, silently). Callers that need to know whether the write
// landed — every save that must stay consistent with a second write — use
// UpdateProfileWithTags instead.
func (r *MentorRepository) Update(ctx context.Context, mentorId string, updates map[string]interface{}) error {
	if len(updates) == 0 {
		// Validate before short-circuiting so a bad column name is still
		// reported, then leave updated_at alone: it busts the OG/image caches.
		if _, _, err := buildMentorUpdate(mentorId, updates); err != nil {
			return err
		}
		return nil
	}

	query, args, err := buildMentorUpdate(mentorId, updates)
	if err != nil {
		return err
	}

	if _, err := r.pool.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("failed to update mentor: %w", err)
	}

	return nil
}

// ErrMentorNotWritable reports that a profile write matched no writable mentor
// row — the profile was deleted (D70), or purged, between the service's read and
// the write. Nothing was written: the transaction is rolled back, so the row and
// the tag set stay as they were rather than half-updated.
var ErrMentorNotWritable = errors.New("mentor row is not writable (missing or deleted)")

// UpdateProfileWithTags writes the mentor row and REPLACES its tag set in one
// transaction (C1).
//
// The two used to be separate operations, and on the mentor's own save path a
// tags failure was logged while success was still returned — so the profile text
// and the tag set could silently disagree, with nothing in the response saying
// so. Tags are a full replacement (DELETE then INSERT), which makes a failure
// between the two writes worse than a no-op: the mentor can end up with FEWER
// tags than they submitted and a save that reported success.
//
// Zero affected rows on the row UPDATE aborts the whole transaction with
// ErrMentorNotWritable, which is what closes the soft-delete race: a delete
// committing between the service's existence check and this write turns the
// UPDATE into a no-op (deleted_at IS NULL), and without the check the tag
// replacement would still have rewritten the tags of a deleted profile.
func (r *MentorRepository) UpdateProfileWithTags(
	ctx context.Context,
	mentorID string,
	updates map[string]interface{},
	tagIDs []string,
) error {

	query, args, err := buildMentorUpdate(mentorID, updates)
	if err != nil {
		return err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		// Rollback is safe to call even after Commit
		_ = tx.Rollback(ctx) //nolint:errcheck
	}()

	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update mentor: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrMentorNotWritable
	}

	if err := replaceMentorTags(ctx, tx, mentorID, tagIDs); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// CreateMentor creates a new mentor record in PostgreSQL, with its tag
// associations, in ONE transaction.
// Returns: mentorId (UUID), legacyId (int), error
// Note: slug is generated automatically using pre-fetched legacy_id
//
// tagIDs are written here rather than by a follow-up call for the same reason
// UpdateProfileWithTags exists (C1): the registration service used to insert the
// row, then set the tags, then only LOG a tags failure — so a mentor could
// register with tags and land in the catalog with none of them, which is exactly
// how the "Security" tag went missing from live profiles (migration 000009).
// Failing the whole registration instead lets the registrant retry into a clean
// state; a committed row with the wrong tags cannot be retried at all.
//
// nolint:gocyclo // one transaction with ordered steps (sequence -> slug claim
// -> insert -> tags); splitting it would hide the ordering the correctness
// depends on.
func (r *MentorRepository) CreateMentor(ctx context.Context, fields map[string]interface{}, tagIDs []string) (string, int, string, error) {
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

	// Slug: use the caller-chosen username when provided (already normalized
	// and validated by the registration service), else generate the legacy
	// name-<id> form. A chosen slug must also not be an active redirect.
	name, ok := fields["name"].(string)
	if !ok || name == "" {
		return "", 0, "", fmt.Errorf("name is required")
	}
	// claimSlug takes the shared per-slug advisory lock and reports whether the
	// slug is spoken for — as a live profile or as an active redirect.
	// Serializing here (and in ChangeSlug) stops a registration from claiming a
	// slug as its current name while a concurrent rename is retiring that same
	// slug into history — which would leave it both a live profile and another
	// mentor's redirect.
	//
	// Live slugs are checked here and not left to the unique constraint because
	// the generated branch below can only retry what this reports: a generated
	// name-<id> that lands on a username somebody chose would otherwise abort
	// the whole registration with ErrSlugTaken, blaming a caller who never
	// chose a username. The constraint remains the backstop for the race.
	claimSlug := func(candidate string) (bool, error) {
		if lockErr := lockSlugs(ctx, tx, candidate); lockErr != nil {
			return false, lockErr
		}
		var taken bool
		if qErr := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM mentors WHERE slug = $1)
			     OR EXISTS (SELECT 1 FROM mentor_slug_history WHERE slug = $1)`,
			candidate,
		).Scan(&taken); qErr != nil {
			return false, fmt.Errorf("failed to check slug availability: %w", qErr)
		}
		return taken, nil
	}

	mentorSlug, hasChosenSlug := fields["slug"].(string)
	hasChosenSlug = hasChosenSlug && mentorSlug != ""
	if hasChosenSlug {
		// Caller-chosen username (already normalized+validated). A clash with a
		// live profile or a redirect is the user's problem — surface it so they
		// pick another.
		taken, claimErr := claimSlug(mentorSlug)
		if claimErr != nil {
			return "", 0, "", claimErr
		}
		if taken {
			return "", 0, "", ErrSlugTaken
		}
	} else {
		// Generated name-<id> slug. The fresh legacy_id makes a clash nearly
		// impossible, but a mentor could have squatted a future-looking slug as
		// their username or as a redirect — so verify (under the same lock) and
		// disambiguate with a numeric suffix rather than failing a registration
		// whose caller never picked a name.
		base := slug.GenerateMentorSlug(name, nextLegacyID)
		mentorSlug = base
		for attempt := 2; ; attempt++ {
			taken, claimErr := claimSlug(mentorSlug)
			if claimErr != nil {
				return "", 0, "", claimErr
			}
			if !taken {
				break
			}
			if attempt > 6 {
				return "", 0, "", fmt.Errorf("could not derive a free slug for %q", name)
			}
			mentorSlug = fmt.Sprintf("%s-%d", base, attempt)
		}
	}

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
		if isSlugUniqueViolation(err) {
			return "", 0, "", ErrSlugTaken
		}
		return "", 0, "", fmt.Errorf("failed to create mentor: %w", err)
	}

	if tagsErr := replaceMentorTags(ctx, tx, mentorId, tagIDs); tagsErr != nil {
		return "", 0, "", tagsErr
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

// replaceMentorTags swaps a mentor's whole tag set inside the caller's
// transaction. Every tag write in this repository goes through it, so the
// replace-the-set semantics resolveTagsStrict documents cannot be implemented
// two different ways on two write paths.
func replaceMentorTags(ctx context.Context, tx pgx.Tx, mentorID string, tagIDs []string) error {
	if _, err := tx.Exec(ctx, "DELETE FROM mentor_tags WHERE mentor_id = $1", mentorID); err != nil {
		return fmt.Errorf("failed to delete existing tags: %w", err)
	}

	for _, tagID := range tagIDs {
		if _, err := tx.Exec(ctx,
			"INSERT INTO mentor_tags (mentor_id, tag_id) VALUES ($1, $2)",
			mentorID, tagID); err != nil {
			return fmt.Errorf("failed to insert tag: %w", err)
		}
	}

	return nil
}

// There is deliberately NO standalone "set this mentor's tags" method. Tags are
// only ever written next to the row they belong to — by CreateMentor on
// registration and UpdateProfileWithTags on a save — because a tag set written
// on its own is a tag set that can land while the row write fails, or fail while
// the row write lands. That was C1.

// GetAllTags retrieves all tags
func (r *MentorRepository) GetAllTags(ctx context.Context) (map[string]string, error) {
	return r.tagsCache.Get()
}

// GetByEmail retrieves a mentor by email address. Draft and pending
// mentors can log in too (to finish/fix their profile); declined mentors
// stay excluded. When several rows share an email (only active emails are
// unique), the most "advanced" profile wins.
//
// It cannot share mentorSelect (no tag join, and the session counts are
// deliberately not computed for a login), but it obeys the same rule: every
// nullable column is COALESCEd or scanned into a pointer field. A NULL that
// fails the scan surfaces here as "unknown email" — i.e. a mentor locked out of
// their own account by an empty profile field.
func (r *MentorRepository) GetByEmail(ctx context.Context, email string) (*models.Mentor, error) {
	query := `
		SELECT id, airtable_id, legacy_id, slug, name, COALESCE(job_title, ''),
			COALESCE(workplace, ''), COALESCE(about, ''), COALESCE(details, ''),
			COALESCE(competencies, ''), COALESCE(experience, ''), COALESCE(price, ''),
			status, ''::text as tags, COALESCE(calendar_url, ''),
			COALESCE(sort_order, 0), created_at, updated_at, 0 as mentee_count,
			0 as legacy_sessions_count, photo_style, COALESCE(moderation_note, ''),
			deleted_at
		FROM mentors
		WHERE email = $1 AND status IN ('active', 'inactive', 'pending', 'draft')
			-- A deleted profile (D70) is not a login candidate. Excluding it HERE,
			-- rather than after the read, is what stops the magic-link email from
			-- ever being generated: RequestLogin cannot mail a link for a mentor
			-- it never found, and the caller gets the same enumeration-safe 200 an
			-- unknown address gets.
			AND deleted_at IS NULL
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

// SetLoginToken sets the login token for a mentor. Consumption is a single
// atomic UPDATE — see ConsumeLoginToken in session.go; there is deliberately no
// read-by-token and no separate clear.
func (r *MentorRepository) SetLoginToken(ctx context.Context, mentorId string, token string, exp time.Time) error {
	query := `
		UPDATE mentors
		SET login_token = $1, login_token_expires_at = $2, updated_at = NOW()
		WHERE id = $3
	`
	// SECURITY: store the hash, never the plaintext token (L1).
	_, err := r.pool.Exec(ctx, query, HashOneTimeToken(token), exp, mentorId)
	return err
}

// FetchAllMentorsFromDB retrieves all mentors from PostgreSQL for cache population
func (r *MentorRepository) FetchAllMentorsFromDB(ctx context.Context) ([]*models.Mentor, error) {
	rows, err := r.pool.Query(ctx, mentorCatalogQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch mentors: %w", err)
	}

	return models.ScanMentors(rows)
}

// FetchSingleMentorFromDB retrieves a single mentor by slug from PostgreSQL
func (r *MentorRepository) FetchSingleMentorFromDB(ctx context.Context, mentorSlug string) (*models.Mentor, error) {
	query := mentorSelect + `
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

// moderationListSelect is the column list both moderation list queries share,
// in the order scanModerationList expects.
const moderationListSelect = `
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
		m.deleted_at,
		m.created_at
	FROM mentors m
`

// ListForModeration retrieves mentors for moderation tabs, sorted by created_at DESC.
// Deleted profiles (D70) are excluded from every status tab — they live in their
// own tab, via ListDeletedForModeration, so an admin cannot act on one by
// reaching it through "Approved".
func (r *MentorRepository) ListForModeration(ctx context.Context, statuses []string) ([]models.AdminMentorListItem, error) {
	query := moderationListSelect + `
		WHERE m.status = ANY($1) AND m.deleted_at IS NULL
		ORDER BY m.created_at DESC
	`

	rows, err := r.pool.Query(ctx, query, statuses)
	if err != nil {
		return nil, fmt.Errorf("failed to list mentors for moderation: %w", err)
	}
	return scanModerationList(rows)
}

// ListDeletedForModeration retrieves the deleted profiles for the admin-only
// "Deleted" tab, most recently deleted first — which is also the order in which
// a restore is most likely to be wanted, and the reverse of the order the purge
// job erases them in.
func (r *MentorRepository) ListDeletedForModeration(ctx context.Context) ([]models.AdminMentorListItem, error) {
	query := moderationListSelect + `
		WHERE m.deleted_at IS NOT NULL
		ORDER BY m.deleted_at DESC
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list deleted mentors: %w", err)
	}
	return scanModerationList(rows)
}

func scanModerationList(rows pgx.Rows) ([]models.AdminMentorListItem, error) {
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
			&item.DeletedAt,
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
			m.deleted_at,
			(SELECT COUNT(*) FROM client_requests cr WHERE cr.mentor_id = m.id) AS requests_count,
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
		&mentor.DeletedAt,
		&mentor.RequestsCount,
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
// A deleted profile (D70) is not moderatable at all; only RestoreMentor may
// change its state.
func (r *MentorRepository) SetMentorStatus(ctx context.Context, mentorID, status string) error {
	query := `
		UPDATE mentors
		SET status = $1, updated_at = NOW()
		WHERE id = $2
			AND deleted_at IS NULL
			AND NOT ($1 = 'draft' AND activated_at IS NOT NULL)
	`
	commandTag, err := r.pool.Exec(ctx, query, status, mentorID)
	if err != nil {
		return fmt.Errorf("failed to update mentor status: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("mentor with ID %s not found (or deleted, or transition to draft forbidden)", mentorID)
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
		WHERE id = $1 AND deleted_at IS NULL
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
		WHERE id = $1 AND activated_at IS NULL AND deleted_at IS NULL
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

// GetByConfirmationToken looks a mentor up by email confirmation token
// (expired tokens included — the caller decides between confirm and resend).
// Returns (nil, nil) when no row matches.
//
// This read CLASSIFIES; it does not authorize. Both callers follow it with the
// atomic compare-and-swap in session.go, which is what makes the state change
// exactly-once. Keeping the read is what preserves the invalid / expired /
// already three-way answer the web client branches on.
func (r *MentorRepository) GetByConfirmationToken(ctx context.Context, token string) (*models.MentorConfirmation, error) {
	hash, legacy := confirmationTokenArgs(token)
	query := `
		SELECT id, name, COALESCE(email::text, ''), status,
			COALESCE(email_confirmation_expires_at, to_timestamp(0))
		FROM mentors
		WHERE ` + confirmationTokenPredicate(1, 2) + `
		LIMIT 1
	`

	var mc models.MentorConfirmation
	// SECURITY (D57): confirmation tokens are stored hashed like login tokens;
	// match by hash, with the pre-D57 plaintext fallback described on
	// confirmationTokenArgs.
	err := r.pool.QueryRow(ctx, query, hash, legacy).Scan(
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
	// Deleted profiles (D70) are gone for everyone but admin reads. This check
	// comes FIRST and is not covered by AllowAnyStatus on purpose: that option
	// exists so a draft/pending mentor can reach their own profile, and a
	// deleted profile must not be reachable by its owner either. Every full
	// mentor read in this repository funnels through here, so a caller cannot
	// forget it — GetBySlug (the public page, hence the 404), GetByMentorId
	// (the portal) and GetAll (the catalog) all pass through this one filter.
	if !opts.IncludeDeleted && mentor.DeletedAt != nil {
		return nil
	}

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
