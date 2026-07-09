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
	UploadPictureByMentorId(ctx context.Context, mentorId string, mentorSlug string, req *models.UploadProfilePictureRequest) (string, error)
	SetProfileStatusByMentorId(ctx context.Context, mentorId string, status string) error
}

// RegistrationServiceInterface defines the interface for registration service operations
type RegistrationServiceInterface interface {
	RegisterMentor(ctx context.Context, req *models.RegisterMentorRequest) (*models.RegisterMentorResponse, error)
}

// MentorAuthServiceInterface defines the interface for mentor authentication
type MentorAuthServiceInterface interface {
	RequestLogin(ctx context.Context, email string) (*models.RequestLoginResponse, error)
	VerifyLogin(ctx context.Context, token string) (*models.MentorSession, string, error)
	GetSessionTTL() int
	GetCookieDomain() string
	GetCookieSecure() bool
	GetTokenManager() *jwt.TokenManager
}

// AdminAuthServiceInterface defines one-time login flow for moderators/admins.
type AdminAuthServiceInterface interface {
	RequestLogin(ctx context.Context, email string) (*models.AdminRequestLoginResponse, error)
	VerifyLogin(ctx context.Context, token string) (*models.AdminSession, string, error)
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

// ReviewServiceInterface defines the interface for review service operations
type ReviewServiceInterface interface {
	CheckReview(ctx context.Context, requestID string) (*models.ReviewCheckResponse, error)
	SubmitReview(ctx context.Context, requestID string, req *models.SubmitReviewRequest) (*models.SubmitReviewResponse, error)
}

type AdminMentorsServiceInterface interface {
	ListMentors(ctx context.Context, session *models.AdminSession, filter models.MentorModerationFilter) ([]models.AdminMentorListItem, error)
	GetMentor(ctx context.Context, session *models.AdminSession, mentorID string) (*models.AdminMentorDetails, error)
	UpdateMentorProfile(ctx context.Context, session *models.AdminSession, mentorID string, req *models.AdminMentorProfileUpdateRequest) (*models.AdminMentorDetails, error)
	ApproveMentor(ctx context.Context, session *models.AdminSession, mentorID string) (*models.AdminMentorDetails, error)
	DeclineMentor(ctx context.Context, session *models.AdminSession, mentorID string) (*models.AdminMentorDetails, error)
	UpdateMentorStatus(ctx context.Context, session *models.AdminSession, mentorID string, status string) (*models.AdminMentorDetails, error)
	UploadMentorPicture(ctx context.Context, session *models.AdminSession, mentorID string, req *models.UploadProfilePictureRequest) (string, error)
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
