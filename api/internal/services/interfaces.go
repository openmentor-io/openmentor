package services

import (
	"context"

	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/pkg/jwt"
)

// ContactServiceInterface defines the interface for contact service operations
type ContactServiceInterface interface {
	SubmitContactForm(ctx context.Context, req *models.ContactMentorRequest) (*models.ContactMentorResponse, error)
}

// MentorServiceInterface defines the interface for mentor service operations
type MentorServiceInterface interface {
	GetAllMentors(ctx context.Context, opts models.FilterOptions) ([]*models.Mentor, error)
	GetMentorByID(ctx context.Context, id int, opts models.FilterOptions) (*models.Mentor, error)
	GetMentorBySlug(ctx context.Context, slug string, opts models.FilterOptions) (*models.Mentor, error)
	GetMentorByMentorId(ctx context.Context, mentorId string, opts models.FilterOptions) (*models.Mentor, error)
}

// ProfileServiceInterface defines the interface for profile service operations
type ProfileServiceInterface interface {
	SaveProfileByMentorId(ctx context.Context, mentorId string, req *models.SaveProfileRequest) error
	UploadPictureByMentorId(ctx context.Context, mentorId string, req *models.UploadProfilePictureRequest) (string, error)
	SetProfileStatusByMentorId(ctx context.Context, mentorId string, status string) error
	SubmitProfileByMentorId(ctx context.Context, mentorId string) error
	DeleteProfileByMentorId(ctx context.Context, mentorId string, confirmUsername string) error
	// StashDeletedProfileImages / RestoreDeletedProfileImages move a profile's
	// images into and out of the S3 trash prefix (D70). On the interface so the
	// admin deletion and restore drive the SAME storage moves as the mentor's
	// own deletion — the admin service holds no storage client of its own.
	StashDeletedProfileImages(ctx context.Context, mentorId string)
	RestoreDeletedProfileImages(ctx context.Context, mentorId string)
}

// MentorConfirmationServiceInterface defines the public email-confirmation
// flow of the draft-status registration workflow.
type MentorConfirmationServiceInterface interface {
	ConfirmEmail(ctx context.Context, token string) (already bool, err error)
	ResendConfirmation(ctx context.Context, token string) (already bool, err error)
}

// RegistrationServiceInterface defines the interface for registration service operations
type RegistrationServiceInterface interface {
	RegisterMentor(ctx context.Context, req *models.RegisterMentorRequest) (*models.RegisterMentorResponse, error)
}

// MentorAuthServiceInterface defines the interface for mentor authentication
type MentorAuthServiceInterface interface {
	RequestLogin(ctx context.Context, email string) (*models.RequestLoginResponse, error)
	VerifyLogin(ctx context.Context, token string) (*models.MentorSession, string, error)
	RevokeSession(ctx context.Context, sessionToken string) error
	GetSessionTTL() int
	GetCookieDomain() string
	GetCookieSecure() bool
	GetTokenManager() *jwt.TokenManager
}

// AdminAuthServiceInterface defines one-time login flow for moderators/admins.
type AdminAuthServiceInterface interface {
	RequestLogin(ctx context.Context, email string) (*models.AdminRequestLoginResponse, error)
	VerifyLogin(ctx context.Context, token string) (*models.AdminSession, string, error)
	RevokeSession(ctx context.Context, sessionToken string) error
	GetSessionTTL() int
	GetCookieDomain() string
	GetCookieSecure() bool
	GetTokenManager() *jwt.TokenManager
}

// MentorRequestsServiceInterface defines the interface for mentor request management
type MentorRequestsServiceInterface interface {
	GetRequests(ctx context.Context, mentorId string, group string) (*models.ClientRequestsResponse, error)
	GetRequestByID(ctx context.Context, mentorId string, requestID string) (*models.MentorClientRequest, error)
	UpdateStatus(ctx context.Context, mentorId string, requestID string, newStatus models.RequestStatus) (*models.MentorClientRequest, error)
	DeclineRequest(ctx context.Context, mentorId string, requestID string, payload *models.DeclineRequestPayload) (*models.MentorClientRequest, error)
}

// ReviewServiceInterface defines the interface for review service operations.
// The token pair is the H4 capability path; the requestID pair is the legacy path
// kept alive for the dual-read window and deleted at the cutover.
type ReviewServiceInterface interface {
	CheckReviewByToken(ctx context.Context, rawToken string) (*models.ReviewCheckResponse, error)
	SubmitReviewWithToken(ctx context.Context, rawToken string, req *models.SubmitReviewRequest) (*models.SubmitReviewResponse, error)

	CheckReview(ctx context.Context, requestID string) (*models.ReviewCheckResponse, error)
	SubmitReview(ctx context.Context, requestID string, req *models.SubmitReviewRequest) (*models.SubmitReviewResponse, error)
}

type AdminMentorsServiceInterface interface {
	ListMentors(ctx context.Context, session *models.AdminSession, filter models.MentorModerationFilter) ([]models.AdminMentorListItem, error)
	GetMentor(ctx context.Context, session *models.AdminSession, mentorID string) (*models.AdminMentorDetails, error)
	UpdateMentorProfile(ctx context.Context, session *models.AdminSession, mentorID string, req *models.AdminMentorProfileUpdateRequest) (*models.AdminMentorDetails, error)
	ApproveMentor(ctx context.Context, session *models.AdminSession, mentorID string) (*models.AdminMentorDetails, error)
	DeclineMentor(ctx context.Context, session *models.AdminSession, mentorID string) (*models.AdminMentorDetails, error)
	ReturnMentor(ctx context.Context, session *models.AdminSession, mentorID string, reason string) (*models.AdminMentorDetails, error)
	UpdateMentorStatus(ctx context.Context, session *models.AdminSession, mentorID string, status string) (*models.AdminMentorDetails, error)
	UploadMentorPicture(ctx context.Context, session *models.AdminSession, mentorID string, req *models.UploadProfilePictureRequest) (string, error)
	DeleteMentor(ctx context.Context, session *models.AdminSession, mentorID string, confirmUsername string) (*models.AdminMentorDetails, error)
	RestoreMentor(ctx context.Context, session *models.AdminSession, mentorID string) (*models.AdminMentorDetails, error)
}

// AdminRequestsServiceInterface defines admin access to the requests a mentor
// received, including the unrestricted status override.
type AdminRequestsServiceInterface interface {
	ListMentorRequests(ctx context.Context, session *models.AdminSession, mentorID string, statusFilter models.RequestStatus) (*models.ClientRequestsResponse, error)
	GetMentorRequest(ctx context.Context, session *models.AdminSession, mentorID string, requestID string) (*models.MentorClientRequest, error)
	UpdateRequestStatus(ctx context.Context, session *models.AdminSession, mentorID string, requestID string, newStatus models.RequestStatus) (*models.MentorClientRequest, error)
}

// Ensure services implement their interfaces
var _ ContactServiceInterface = (*ContactService)(nil)
var _ MentorServiceInterface = (*MentorService)(nil)
var _ ProfileServiceInterface = (*ProfileService)(nil)
var _ RegistrationServiceInterface = (*RegistrationService)(nil)
var _ MentorAuthServiceInterface = (*MentorAuthService)(nil)
var _ AdminAuthServiceInterface = (*AdminAuthService)(nil)
var _ MentorRequestsServiceInterface = (*MentorRequestsService)(nil)
var _ ReviewServiceInterface = (*ReviewService)(nil)
var _ AdminMentorsServiceInterface = (*AdminMentorsService)(nil)
var _ AdminRequestsServiceInterface = (*AdminRequestsService)(nil)
var _ MentorConfirmationServiceInterface = (*MentorConfirmationService)(nil)
