package models

// SubmitReviewRequest represents a review form submission from a mentee.
//
// SECURITY (H4): Token is the review capability. It is carried in the BODY and
// nowhere else — never a path segment, never a query parameter — so it cannot
// reach an access log, a span's url.path or PostHog's $current_url the way
// client_requests.id did. It is deliberately NOT `binding:"required"`: the
// token-bearing handler checks it itself so a missing and a malformed token get
// the same generic answer, and the legacy request-id handler ignores it entirely
// during the dual-read window.
type SubmitReviewRequest struct {
	Token          string `json:"token"`
	MentorReview   string `json:"mentorReview" binding:"required,min=10,max=5000"`
	PlatformReview string `json:"platformReview" binding:"max=5000"`
	Improvements   string `json:"improvements" binding:"max=5000"`
	CaptchaToken   string `json:"captchaToken" binding:"required"`
}

// CheckReviewRequest asks whether a capability can still be spent. A POST with a
// body rather than a GET with a query string, for the reason above.
type CheckReviewRequest struct {
	Token string `json:"token"`
}

// SubmitReviewResponse represents the response after submitting a review
type SubmitReviewResponse struct {
	Success  bool   `json:"success"`
	ReviewID string `json:"reviewId,omitempty"`
	Error    string `json:"error,omitempty"`
}

// ReviewCheckResponse represents the response for checking if a review can be
// submitted. MentorName is populated ONLY for a capability that is still
// spendable: a consumed, expired or unknown token must reveal nothing about the
// request behind it.
type ReviewCheckResponse struct {
	CanSubmit  bool   `json:"canSubmit"`
	Error      string `json:"error,omitempty"`
	MentorName string `json:"mentorName,omitempty"`
}
