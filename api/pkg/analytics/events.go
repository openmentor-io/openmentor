package analytics

const (
	EventMenteeContactSubmitted      = "mentee_contact_submitted"
	EventMentorRegistrationSubmitted = "mentor_registration_submitted"
	EventReviewEligibilityChecked    = "review_eligibility_checked"
	EventReviewSubmitted             = "review_submitted"

	EventMentorAuthLoginRequested = "mentor_auth_login_requested"
	EventMentorAuthLoginVerified  = "mentor_auth_login_verified"
	EventAdminAuthLoginRequested  = "admin_auth_login_requested"
	EventAdminAuthLoginVerified   = "admin_auth_login_verified"

	EventMentorProfileUpdated         = "mentor_profile_updated"
	EventMentorProfilePictureUploaded = "mentor_profile_picture_uploaded"
	EventMentorProfileStatusChanged   = "mentor_profile_status_changed"
	EventMentorRequestStatusUpdated   = "mentor_request_status_updated"
	EventMentorRequestDeclined        = "mentor_request_declined"

	EventAdminMentorModerationAction = "admin_mentor_moderation_action"
	EventAdminMentorStatusUpdated    = "admin_mentor_status_updated"
	EventAdminMentorProfileUpdated   = "admin_mentor_profile_updated"
	EventAdminMentorPictureUploaded  = "admin_mentor_picture_uploaded"

	// Worker job events (ported from openmentor-func's legacy analytics
	// event catalog; names kept verbatim).
	EventNewMentorWatcherProcessed      = "new_mentor_watcher_processed"
	EventNewRequestWatcherProcessed     = "new_request_watcher_processed"
	EventMentorAuthLoginEmailSent       = "mentor_auth_login_email_sent"
	EventAdminAuthLoginEmailSent        = "admin_auth_login_email_sent"
	EventRequestProcessFinishedNotified = "request_process_finished_notified"
	EventMentorPendingRequestsReminded  = "mentor_pending_requests_reminded"
	EventMentorStatusUpdateReminded     = "mentor_status_update_reminded"

	// Migration tooling events (getmentor.dev -> openmentor.io imports).
	// EventMentorProfileMigrated fires from the worker's
	// /jobs/profile-migrated endpoint; EventMigrationIntentScheduled from
	// the public /migrate page's opt-in endpoint.
	EventMentorProfileMigrated    = "mentor_profile_migrated"
	EventMigrationIntentScheduled = "migration_intent_scheduled"
)
