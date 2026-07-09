package models

// SubmitReviewRequest represents a review form submission from a mentee
type SubmitReviewRequest struct {
	MentorReview   string `json:"mentorReview" binding:"required,min=10,max=5000"`
	PlatformReview string `json:"platformReview" binding:"max=5000"`
	Improvements   string `json:"improvements" binding:"max=5000"`
	RecaptchaToken string `json:"recaptchaToken" binding:"required"`
}

// SubmitReviewResponse represents the response after submitting a review
type SubmitReviewResponse struct {
	Success  bool   `json:"success"`
	ReviewID string `json:"reviewId,omitempty"`
	Error    string `json:"error,omitempty"`
}

// ReviewCheckResponse represents the response for checking if a review can be submitted
type ReviewCheckResponse struct {
	CanSubmit  bool   `json:"canSubmit"`
	Error      string `json:"error,omitempty"`
	MentorName string `json:"mentorName,omitempty"`
}
