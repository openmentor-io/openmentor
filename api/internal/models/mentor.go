package models

import (
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Mentor represents a mentor in the system
type Mentor struct {
	MentorID     string  `json:"mentorId"` // UUID primary key
	LegacyID     int     `json:"id"`       // Old integer ID (maps to legacy_id column)
	AirtableID   *string `json:"-"`        // Internal only - not exposed in API
	Slug         string  `json:"slug"`
	Name         string  `json:"name"`
	Job          string  `json:"job"`
	Workplace    string  `json:"workplace"`
	Description  string  `json:"description"`
	About        string  `json:"about"`
	Competencies string  `json:"competencies"`
	Experience   string  `json:"experience"`
	Price        string  `json:"price"`
	MenteeCount  int     `json:"menteeCount"`
	// SessionsCount is the number of completed sessions: client_requests rows
	// with status = 'done' PLUS LegacySessionsCount. It is loaded by the same
	// aggregate that backs MenteeCount in the mentor scan queries.
	SessionsCount int `json:"sessionsCount"`
	// LegacySessionsCount is the share of SessionsCount that happened on
	// getmentor.dev before the profile was migrated (D28). Zero for mentors
	// who registered on OpenMentor. Exposed so the profile page can disclose
	// where the history comes from instead of passing it off as native.
	LegacySessionsCount int       `json:"legacySessionsCount"`
	Tags                []string  `json:"tags"`
	SortOrder           int       `json:"sortOrder"`
	IsVisible           bool      `json:"isVisible"` // Computed: status = 'active'
	CalendarType        string    `json:"calendarType"`
	IsNew               bool      `json:"isNew"`     // Computed: created_at > NOW() - 14 days
	UpdatedAt           time.Time `json:"updatedAt"` // Used for profile image cache invalidation

	// Status field for login eligibility checks
	Status string `json:"status"`

	// DeletedAt marks a deleted profile (D70). Non-nil means the profile is
	// gone for every purpose except an admin's view of it and the purge job:
	// no public page, no login link, no live review invitations. Deletion also
	// sets Status to "inactive", so code that predates deletion sees a hidden
	// profile rather than a live one — but "hidden" is not "gone", which is
	// what the DeletedAt checks in the repository add.
	DeletedAt *time.Time `json:"deletedAt,omitempty"`

	// PhotoStyle is the auto-detected profile picture display style
	// ('hero' for light uniform backgrounds, 'frame' otherwise).
	PhotoStyle string `json:"photoStyle"`

	// Secure fields (cleared by repository unless ShowHidden is true)
	CalendarURL string `json:"calendarUrl"`
	// ModerationNote is the reviewer note left when a profile is returned
	// to draft. Exposed only on the authenticated own-profile payload
	// (ShowHidden) — never on public payloads.
	ModerationNote string `json:"moderationNote,omitempty"`

	// Internal fields (not exposed in JSON)
	CreatedAt time.Time `json:"-"` // Used for IsNew computation
}

// PublicMentorResponse represents the public API response format
type PublicMentorResponse struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Title         string `json:"title"`
	Workplace     string `json:"workplace"`
	About         string `json:"about"`
	Description   string `json:"description"`
	Competencies  string `json:"competencies"`
	Experience    string `json:"experience"`
	Price         string `json:"price"`
	DoneSessions  int    `json:"doneSessions"`
	SessionsCount int    `json:"sessionsCount"`
	// LegacySessionsCount is the getmentor.dev share of SessionsCount (D28).
	LegacySessionsCount int       `json:"legacySessionsCount"`
	Tags                string    `json:"tags"`
	Link                string    `json:"link"`
	PhotoStyle          string    `json:"photoStyle"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

// ToPublicResponse converts a Mentor to PublicMentorResponse
func (m *Mentor) ToPublicResponse(baseURL string) PublicMentorResponse {
	return PublicMentorResponse{
		ID:                  m.LegacyID, // Use LegacyID for backwards compatibility
		Name:                m.Name,
		Title:               m.Job,
		Workplace:           m.Workplace,
		About:               m.About,
		Description:         m.Description,
		Competencies:        m.Competencies,
		Experience:          m.Experience,
		Price:               m.Price,
		DoneSessions:        m.MenteeCount,
		SessionsCount:       m.SessionsCount,
		LegacySessionsCount: m.LegacySessionsCount,
		Tags:                strings.Join(m.Tags, ","),
		Link:                baseURL + "/mentor/" + m.Slug,
		PhotoStyle:          m.PhotoStyle,
		UpdatedAt:           m.UpdatedAt,
	}
}

// FilterOptions represents options for filtering mentors
type FilterOptions struct {
	OnlyVisible    bool
	ShowHidden     bool
	DropLongFields bool
	// AllowAnyStatus disables the public-side status filter (which hides
	// everything but active/inactive). Used only by session-authenticated
	// own-profile flows so draft/pending mentors can access their profile.
	AllowAnyStatus bool
	// IncludeDeleted keeps deleted profiles (D70) in the result. Deliberately
	// separate from AllowAnyStatus, which the mentor's OWN-profile flows set:
	// a deleted profile must stay invisible to its owner too, so only
	// admin-side reads set this.
	IncludeDeleted bool
}

// orZero unwraps a column scanned through a pointer: NULL becomes the field's
// zero value, which is what every consumer of Mentor expects from an unset text
// field. Nullable columns go through a pointer because pgx fails the whole row
// scan on a NULL in a non-pointer destination — see ScanMentor.
func orZero[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// ScanMentor scans a single PostgreSQL row into a Mentor struct
func ScanMentor(row pgx.Row) (*Mentor, error) {
	var m Mentor
	var tagsStr *string
	var airtableID *string
	var calendarURL *string
	var job *string
	var workplace *string
	var about *string
	var description *string
	var competencies *string
	var moderationNote *string
	var experience *string
	var price *string
	// Every nullable mentors column is scanned through a pointer, even though the
	// queries also COALESCE it (see mentorSelect). pgx fails the WHOLE row scan on
	// a NULL in a non-pointer destination, so without this a single query missing
	// a COALESCE makes that mentor unreadable everywhere: no login, no profile
	// page, and — because ScanMentors shares this function — a broken catalog for
	// everyone. Belt and braces is cheap; a locked-out mentor is not.
	var sortOrder *int

	err := row.Scan(
		&m.MentorID,
		&airtableID,
		&m.LegacyID,
		&m.Slug,
		&m.Name,
		&job,
		&workplace,
		&about,
		&description,
		&competencies,
		&experience,
		&price,
		&m.Status,
		&tagsStr,
		&calendarURL,
		&sortOrder,
		&m.CreatedAt,
		&m.UpdatedAt,
		&m.MenteeCount,
		&m.LegacySessionsCount,
		&m.PhotoStyle,
		&moderationNote,
		&m.DeletedAt,
	)
	if err != nil {
		return nil, err
	}

	// Set nullable fields. AirtableID stays a pointer: nil is meaningful there
	// (it marks a mentor who registered natively rather than being imported).
	m.AirtableID = airtableID
	m.SortOrder = orZero(sortOrder)
	m.CalendarURL = orZero(calendarURL)
	m.ModerationNote = orZero(moderationNote)
	m.Job = orZero(job)
	m.Workplace = orZero(workplace)
	m.About = orZero(about)
	m.Description = orZero(description)
	m.Competencies = orZero(competencies)
	m.Experience = orZero(experience)
	m.Price = orZero(price)

	// Parse tags from comma-separated string
	m.Tags = []string{}
	if tagsStr != nil && *tagsStr != "" {
		for _, tag := range strings.Split(*tagsStr, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				m.Tags = append(m.Tags, tag)
			}
		}
	}

	// SessionsCount mirrors the mentee_count column, which is the aggregate
	// count of client_requests with status = 'done' for this mentor plus any
	// sessions carried over from getmentor.dev (D28)
	m.SessionsCount = m.MenteeCount

	// Compute IsVisible: status = 'active'
	m.IsVisible = m.Status == "active"

	// Compute IsNew: created_at > NOW() - 14 days. A migrated mentor's row is
	// days old but their mentoring is not, so carried-over history disqualifies
	// them from the NEW badge — it would otherwise hide their session count,
	// which takes second place to the badge on the catalog card (D28).
	fourteenDaysAgo := time.Now().AddDate(0, 0, -14)
	m.IsNew = m.CreatedAt.After(fourteenDaysAgo) && m.LegacySessionsCount == 0

	// Determine calendar type
	m.CalendarType = GetCalendarType(m.CalendarURL)

	return &m, nil
}

// ScanMentors scans multiple PostgreSQL rows into a slice of Mentor structs
func ScanMentors(rows pgx.Rows) ([]*Mentor, error) {
	defer rows.Close()

	mentors := []*Mentor{}
	for rows.Next() {
		mentor, err := ScanMentor(rows)
		if err != nil {
			return nil, err
		}
		mentors = append(mentors, mentor)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return mentors, nil
}

// GetCalendarType determines the calendar service type from URL
func GetCalendarType(url string) string {
	if url == "" {
		return "none"
	}

	url = strings.ToLower(url)

	switch {
	case strings.Contains(url, "calendly.com"):
		return "calendly"
	case strings.Contains(url, "koalendar.com"):
		return "koalendar"
	case strings.Contains(url, "calendlab.com"):
		return "calendlab"
	default:
		return "url"
	}
}
