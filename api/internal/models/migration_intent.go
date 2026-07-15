package models

// ScheduleMigrationRequest is the public /migrate page's "schedule my
// profile migration" submission: the mentor's getmentor.dev slug plus a
// Turnstile token. Slug shape (transliterated-name-legacyid) is validated
// in the service.
type ScheduleMigrationRequest struct {
	Slug         string `json:"slug" binding:"required,min=3,max=200"`
	CaptchaToken string `json:"captchaToken" binding:"required,min=20"`
}

// ScheduleMigrationResponse reports whether the intent was recorded.
// AlreadyScheduled is true when the slug was submitted before (the intent
// is kept, not duplicated).
type ScheduleMigrationResponse struct {
	Success          bool   `json:"success"`
	AlreadyScheduled bool   `json:"alreadyScheduled,omitempty"`
	Error            string `json:"error,omitempty"`
}
