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
	// SessionsCount is the number of completed sessions (client_requests rows
	// with status = 'done'). It is loaded by the same aggregate that backs
	// MenteeCount in the mentor scan queries.
	SessionsCount int       `json:"sessionsCount"`
	Tags          []string  `json:"tags"`
	SortOrder     int       `json:"sortOrder"`
	IsVisible     bool      `json:"isVisible"` // Computed: status = 'active'
	CalendarType  string    `json:"calendarType"`
	IsNew         bool      `json:"isNew"`     // Computed: created_at > NOW() - 14 days
	UpdatedAt     time.Time `json:"updatedAt"` // Used for profile image cache invalidation

	// Status field for login eligibility checks
	Status string `json:"status"`

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
	ID            int       `json:"id"`
	Name          string    `json:"name"`
	Title         string    `json:"title"`
	Workplace     string    `json:"workplace"`
	About         string    `json:"about"`
	Description   string    `json:"description"`
	Competencies  string    `json:"competencies"`
	Experience    string    `json:"experience"`
	Price         string    `json:"price"`
	DoneSessions  int       `json:"doneSessions"`
	SessionsCount int       `json:"sessionsCount"`
	Tags          string    `json:"tags"`
	Link          string    `json:"link"`
	PhotoStyle    string    `json:"photoStyle"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// ToPublicResponse converts a Mentor to PublicMentorResponse
func (m *Mentor) ToPublicResponse(baseURL string) PublicMentorResponse {
	return PublicMentorResponse{
		ID:            m.LegacyID, // Use LegacyID for backwards compatibility
		Name:          m.Name,
		Title:         m.Job,
		Workplace:     m.Workplace,
		About:         m.About,
		Description:   m.Description,
		Competencies:  m.Competencies,
		Experience:    m.Experience,
		Price:         m.Price,
		DoneSessions:  m.MenteeCount,
		SessionsCount: m.SessionsCount,
		Tags:          strings.Join(m.Tags, ","),
		Link:          baseURL + "/mentor/" + m.Slug,
		PhotoStyle:    m.PhotoStyle,
		UpdatedAt:     m.UpdatedAt,
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
		&m.Experience,
		&m.Price,
		&m.Status,
		&tagsStr,
		&calendarURL,
		&m.SortOrder,
		&m.CreatedAt,
		&m.UpdatedAt,
		&m.MenteeCount,
		&m.PhotoStyle,
		&moderationNote,
	)
	if err != nil {
		return nil, err
	}

	// Set nullable fields
	m.AirtableID = airtableID
	if calendarURL != nil {
		m.CalendarURL = *calendarURL
	}
	if moderationNote != nil {
		m.ModerationNote = *moderationNote
	}
	if job != nil {
		m.Job = *job
	}
	if workplace != nil {
		m.Workplace = *workplace
	}
	if about != nil {
		m.About = *about
	}
	if description != nil {
		m.Description = *description
	}
	if competencies != nil {
		m.Competencies = *competencies
	}

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
	// count of client_requests with status = 'done' for this mentor
	m.SessionsCount = m.MenteeCount

	// Compute IsVisible: status = 'active'
	m.IsVisible = m.Status == "active"

	// Compute IsNew: created_at > NOW() - 14 days
	fourteenDaysAgo := time.Now().AddDate(0, 0, -14)
	m.IsNew = m.CreatedAt.After(fourteenDaysAgo)

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
