export const DASHBOARD_SPEC_VERSION = "2026-07-08";
export const MANAGED_TAG = "managed:openmentor:posthog-dashboard";

const DISPLAY = {
    LINE: "ActionsLineGraph",
    BAR: "ActionsBar",
    STACKED_BAR: "ActionsStackedBar",
    NUMBER: "BoldNumber",
};

const DEFAULT_DATE_FROM = "-30d";

function eqEventProperty(key, value) {
    return {
        key,
        operator: "exact",
        type: "event",
        value,
    };
}

function inEventProperty(key, values) {
    return {
        key,
        operator: "in",
        type: "event",
        value: values,
    };
}

function mergeFilters(baseFilters, extraFilters) {
    return [...baseFilters, ...(extraFilters || [])];
}

function buildBaseFilters(environment, sourceSystem) {
    const filters = [];
    if (environment) {
        filters.push(eqEventProperty("environment", environment));
    }

    if (Array.isArray(sourceSystem) && sourceSystem.length > 0) {
        filters.push(inEventProperty("source_system", sourceSystem));
    } else if (typeof sourceSystem === "string" && sourceSystem.trim()) {
        filters.push(eqEventProperty("source_system", sourceSystem.trim()));
    }

    return filters;
}

function buildEventNode({ event, math, mathProperty, mathPropertyType }) {
    const node = {
        kind: "EventsNode",
        event,
    };

    if (mathProperty) {
        node.math = math || "avg";
        node.math_property = mathProperty;
        node.math_property_type = mathPropertyType || "event";
        return node;
    }

    if (math) {
        node.math = math;
    }

    return node;
}

function buildTrendsQuery({
    event,
    dateFrom = DEFAULT_DATE_FROM,
    interval = "day",
    display = DISPLAY.LINE,
    math = "total",
    mathProperty,
    mathPropertyType,
    filters = [],
    breakdownKey,
    breakdownType = "event",
    breakdownLimit = 12,
}) {
    const source = {
        kind: "TrendsQuery",
        dateRange: {
            date_from: dateFrom,
        },
        interval,
        series: [
            buildEventNode({
                event,
                math,
                mathProperty,
                mathPropertyType,
            }),
        ],
        trendsFilter: {
            display,
        },
    };

    if (filters.length > 0) {
        source.properties = filters;
    }

    if (breakdownKey) {
        source.breakdownFilter = {
            breakdown: breakdownKey,
            breakdown_limit: breakdownLimit,
            breakdown_type: breakdownType,
        };
    }

    return {
        kind: "InsightVizNode",
        source,
    };
}

function trendsInsight({
    key,
    name,
    description,
    event,
    sourceSystem,
    dateFrom,
    interval,
    display,
    math,
    mathProperty,
    mathPropertyType,
    filters,
    breakdownKey,
    breakdownType,
    breakdownLimit,
    tags = [],
}, environment) {
    const baseFilters = buildBaseFilters(environment, sourceSystem);
    return {
        key,
        name,
        description,
        tags,
        query: buildTrendsQuery({
            event,
            dateFrom,
            interval,
            display,
            math,
            mathProperty,
            mathPropertyType,
            filters: mergeFilters(baseFilters, filters),
            breakdownKey,
            breakdownType,
            breakdownLimit,
        }),
    };
}

function dashboard(key, name, description, tags, insights) {
    return {
        key,
        name,
        description,
        tags,
        insights,
    };
}

function buildExecutiveHealthDashboard(environment) {
    return dashboard(
        "executive_health",
        "OpenMentor - Executive Health",
        "Top-level business health: demand, delivery, supply, and quality outcomes.",
        ["openmentor", "executive", "health"],
        [
            trendsInsight({
                key: "requests_submitted_7d",
                name: "Requests Submitted (7d)",
                description: "Successful mentee contact submissions in the last 7 days.",
                event: "mentee_contact_submitted",
                sourceSystem: "api",
                dateFrom: "-7d",
                display: DISPLAY.NUMBER,
                filters: [eqEventProperty("outcome", "success")],
            }, environment),
            trendsInsight({
                key: "requests_done_7d",
                name: "Requests Done (7d)",
                description: "Requests marked as done in the last 7 days.",
                event: "mentor_request_status_updated",
                sourceSystem: "api",
                dateFrom: "-7d",
                display: DISPLAY.NUMBER,
                filters: [
                    eqEventProperty("outcome", "success"),
                    eqEventProperty("status", "done"),
                ],
            }, environment),
            trendsInsight({
                key: "mentor_registrations_7d",
                name: "Mentor Registrations (7d)",
                description: "Successful mentor registration submissions in the last 7 days.",
                event: "mentor_registration_submitted",
                sourceSystem: "api",
                dateFrom: "-7d",
                display: DISPLAY.NUMBER,
                filters: [eqEventProperty("outcome", "success")],
            }, environment),
            trendsInsight({
                key: "reviews_submitted_7d",
                name: "Reviews Submitted (7d)",
                description: "Successful review submissions in the last 7 days.",
                event: "review_submitted",
                sourceSystem: "api",
                dateFrom: "-7d",
                display: DISPLAY.NUMBER,
                filters: [eqEventProperty("outcome", "success")],
            }, environment),
            trendsInsight({
                key: "mentor_logins_verified_7d",
                name: "Mentor Logins Verified (7d)",
                description: "Successful mentor login verifications across frontend and API.",
                event: "mentor_auth_login_verified",
                sourceSystem: ["frontend", "api"],
                dateFrom: "-7d",
                display: DISPLAY.NUMBER,
                filters: [eqEventProperty("outcome", "success")],
            }, environment),
            trendsInsight({
                key: "admin_logins_verified_7d",
                name: "Admin Logins Verified (7d)",
                description: "Successful admin login verifications across frontend and API.",
                event: "admin_auth_login_verified",
                sourceSystem: ["frontend", "api"],
                dateFrom: "-7d",
                display: DISPLAY.NUMBER,
                filters: [eqEventProperty("outcome", "success")],
            }, environment),
            trendsInsight({
                key: "request_status_mix_30d",
                name: "Request Status Mix (30d)",
                description: "Distribution of successful request status transitions.",
                event: "mentor_request_status_updated",
                sourceSystem: "api",
                display: DISPLAY.STACKED_BAR,
                breakdownKey: "status",
                filters: [eqEventProperty("outcome", "success")],
            }, environment),
            trendsInsight({
                key: "contact_submission_outcomes_30d",
                name: "Contact Submission Outcomes (30d)",
                description: "Success/error mix for mentee contact submissions.",
                event: "mentee_contact_submitted",
                sourceSystem: "api",
                display: DISPLAY.BAR,
                breakdownKey: "outcome",
            }, environment),
            trendsInsight({
                key: "review_submission_outcomes_30d",
                name: "Review Submission Outcomes (30d)",
                description: "Success/error mix for review submissions.",
                event: "review_submitted",
                sourceSystem: "api",
                display: DISPLAY.BAR,
                breakdownKey: "outcome",
            }, environment),
            trendsInsight({
                key: "moderation_actions_by_action_30d",
                name: "Moderation Actions by Type (30d)",
                description: "Successful moderation actions by decision type.",
                event: "admin_mentor_moderation_action",
                sourceSystem: "api",
                display: DISPLAY.BAR,
                breakdownKey: "action",
                filters: [eqEventProperty("outcome", "success")],
            }, environment),
            trendsInsight({
                key: "request_decline_reasons_30d",
                name: "Request Decline Reasons (30d)",
                description: "Reasons used when mentors decline requests.",
                event: "mentor_request_declined",
                sourceSystem: "api",
                display: DISPLAY.BAR,
                breakdownKey: "reason",
                filters: [eqEventProperty("outcome", "success")],
            }, environment),
        ],
    );
}

function buildAcquisitionDashboard(environment) {
    return dashboard(
        "acquisition_discovery",
        "OpenMentor - Acquisition & Discovery",
        "Public-side demand funnel from landing and discovery through request creation.",
        ["openmentor", "acquisition", "discovery"],
        [
            trendsInsight({
                key: "home_page_views_30d",
                name: "Home Page Views (30d)",
                description: "Visits to the main landing page.",
                event: "home_page_viewed",
                sourceSystem: "frontend",
            }, environment),
            trendsInsight({
                key: "mentor_profile_views_30d",
                name: "Mentor Profile Views (30d)",
                description: "Mentor detail page views.",
                event: "mentor_profile_viewed",
                sourceSystem: "frontend",
            }, environment),
            trendsInsight({
                key: "mentor_contact_page_views_30d",
                name: "Mentor Contact Page Views (30d)",
                description: "Contact form page views before submission.",
                event: "mentor_contact_page_viewed",
                sourceSystem: "frontend",
            }, environment),
            trendsInsight({
                key: "contact_submissions_success_frontend_30d",
                name: "Contact Submissions Success (30d)",
                description: "Successful contact submits tracked on frontend.",
                event: "mentee_contact_submitted",
                sourceSystem: "frontend",
                filters: [eqEventProperty("outcome", "success")],
            }, environment),
            trendsInsight({
                key: "mentor_search_usage_30d",
                name: "Mentor Search Usage (30d)",
                description: "Search interactions on mentor listing pages.",
                event: "mentors_search_used",
                sourceSystem: "frontend",
            }, environment),
            trendsInsight({
                key: "mentor_filter_changes_by_type_30d",
                name: "Filter Changes by Type (30d)",
                description: "Which filters are interacted with the most.",
                event: "mentor_filter_changed",
                sourceSystem: "frontend",
                display: DISPLAY.BAR,
                breakdownKey: "filter_type",
            }, environment),
            trendsInsight({
                key: "load_more_clicks_30d",
                name: "Load More Clicks (30d)",
                description: "Mentor list pagination engagement.",
                event: "mentors_list_load_more_clicked",
                sourceSystem: "frontend",
            }, environment),
            trendsInsight({
                key: "mentor_profile_views_by_slug_30d",
                name: "Profile Views by Mentor (30d)",
                description: "Top viewed mentor profiles by slug.",
                event: "mentor_profile_viewed",
                sourceSystem: "frontend",
                display: DISPLAY.BAR,
                breakdownKey: "mentor_slug",
                breakdownLimit: 20,
            }, environment),
            trendsInsight({
                key: "contact_submissions_by_mentor_slug_30d",
                name: "Successful Requests by Mentor (30d)",
                description: "Which mentor pages generate successful requests.",
                event: "mentee_contact_submitted",
                sourceSystem: "frontend",
                display: DISPLAY.BAR,
                breakdownKey: "mentor_slug",
                breakdownLimit: 20,
                filters: [eqEventProperty("outcome", "success")],
            }, environment),
        ],
    );
}

function buildSupplyDashboard(environment) {
    return dashboard(
        "mentor_supply_moderation",
        "OpenMentor - Mentor Supply & Moderation",
        "Mentor onboarding and moderation throughput across API and worker touchpoints.",
        ["openmentor", "mentor", "moderation"],
        [
            trendsInsight({
                key: "mentor_registration_outcomes_30d",
                name: "Mentor Registration Outcomes (30d)",
                description: "Success/error outcomes on mentor registration submissions.",
                event: "mentor_registration_submitted",
                sourceSystem: "api",
                display: DISPLAY.BAR,
                breakdownKey: "outcome",
            }, environment),
            trendsInsight({
                key: "new_mentor_watcher_outcomes_30d",
                name: "New Mentor Watcher Outcomes (30d)",
                description: "Worker processing outcomes for new mentor onboarding.",
                event: "new_mentor_watcher_processed",
                sourceSystem: "worker",
                display: DISPLAY.BAR,
                breakdownKey: "outcome",
            }, environment),
            trendsInsight({
                key: "admin_moderation_outcomes_30d",
                name: "Admin Moderation Outcomes (30d)",
                description: "Success/error outcomes for moderation actions.",
                event: "admin_mentor_moderation_action",
                sourceSystem: "api",
                display: DISPLAY.BAR,
                breakdownKey: "outcome",
            }, environment),
            trendsInsight({
                key: "admin_moderation_action_mix_30d",
                name: "Moderation Action Mix (30d)",
                description: "Approve/decline mix for successful moderation events.",
                event: "admin_mentor_moderation_action",
                sourceSystem: "api",
                display: DISPLAY.BAR,
                breakdownKey: "action",
                filters: [eqEventProperty("outcome", "success")],
            }, environment),
            trendsInsight({
                key: "admin_mentor_status_to_status_30d",
                name: "Mentor Status Changes by Target Status (30d)",
                description: "Successful status updates broken down by target status.",
                event: "admin_mentor_status_updated",
                sourceSystem: "api",
                display: DISPLAY.BAR,
                breakdownKey: "to_status",
                filters: [eqEventProperty("outcome", "success")],
            }, environment),
            trendsInsight({
                key: "mentor_profile_updates_outcomes_30d",
                name: "Mentor Profile Update Outcomes (30d)",
                description: "Success/error profile edits by mentors.",
                event: "mentor_profile_updated",
                sourceSystem: "api",
                display: DISPLAY.BAR,
                breakdownKey: "outcome",
            }, environment),
            trendsInsight({
                key: "mentor_profile_picture_upload_outcomes_30d",
                name: "Mentor Profile Picture Upload Outcomes (30d)",
                description: "Success/error picture upload operations by mentors.",
                event: "mentor_profile_picture_uploaded",
                sourceSystem: "api",
                display: DISPLAY.BAR,
                breakdownKey: "outcome",
            }, environment),
            trendsInsight({
                key: "admin_mentor_picture_upload_outcomes_30d",
                name: "Admin Mentor Picture Upload Outcomes (30d)",
                description: "Success/error outcomes for admin-side mentor picture uploads.",
                event: "admin_mentor_picture_uploaded",
                sourceSystem: "api",
                display: DISPLAY.BAR,
                breakdownKey: "outcome",
            }, environment),
            trendsInsight({
                key: "mentor_status_update_reminder_runs_30d",
                name: "Status Update Reminder Runs (30d)",
                description: "Completed reminder cron runs in the worker.",
                event: "mentor_status_update_reminded",
                sourceSystem: "worker",
                display: DISPLAY.NUMBER,
                filters: [eqEventProperty("outcome", "run_completed")],
            }, environment),
        ],
    );
}

function buildRequestOpsDashboard(environment) {
    return dashboard(
        "request_lifecycle_delivery",
        "OpenMentor - Request Lifecycle & Delivery",
        "Request processing, status transitions, and delivery notifications across systems.",
        ["openmentor", "requests", "operations"],
        [
            trendsInsight({
                key: "new_request_watcher_outcomes_30d",
                name: "New Request Watcher Outcomes (30d)",
                description: "Worker processing outcomes for new request ingestion.",
                event: "new_request_watcher_processed",
                sourceSystem: "worker",
                display: DISPLAY.BAR,
                breakdownKey: "outcome",
            }, environment),
            trendsInsight({
                key: "request_status_updates_over_time_30d",
                name: "Request Status Updates Over Time (30d)",
                description: "Successful API status transitions broken down by status.",
                event: "mentor_request_status_updated",
                sourceSystem: "api",
                display: DISPLAY.STACKED_BAR,
                breakdownKey: "status",
                filters: [eqEventProperty("outcome", "success")],
            }, environment),
            trendsInsight({
                key: "request_decline_reasons_ops_30d",
                name: "Request Decline Reasons (Ops, 30d)",
                description: "Successful decline events broken down by reason.",
                event: "mentor_request_declined",
                sourceSystem: "api",
                display: DISPLAY.BAR,
                breakdownKey: "reason",
                filters: [eqEventProperty("outcome", "success")],
            }, environment),
            trendsInsight({
                key: "request_finished_notification_outcomes_30d",
                name: "Request Finished Notification Outcomes (30d)",
                description: "Success/error outcomes for completion/decline notifications to mentees.",
                event: "request_process_finished_notified",
                sourceSystem: "worker",
                display: DISPLAY.BAR,
                breakdownKey: "outcome",
            }, environment),
            trendsInsight({
                key: "request_finished_success_by_status_30d",
                name: "Request Finished Notifications by Status (30d)",
                description: "Successful finish notifications split by final request status.",
                event: "request_process_finished_notified",
                sourceSystem: "worker",
                display: DISPLAY.BAR,
                breakdownKey: "status",
                filters: [eqEventProperty("outcome", "success")],
            }, environment),
            trendsInsight({
                key: "pending_requests_reminder_runs_30d",
                name: "Pending Requests Reminder Runs (30d)",
                description: "Completed scheduler runs for pending request reminders.",
                event: "mentor_pending_requests_reminded",
                sourceSystem: "worker",
                filters: [eqEventProperty("outcome", "run_completed")],
            }, environment),
            trendsInsight({
                key: "pending_requests_reminder_successes_30d",
                name: "Pending Requests Reminder Success Events (30d)",
                description: "Per-mentor reminder sends for pending requests.",
                event: "mentor_pending_requests_reminded",
                sourceSystem: "worker",
                filters: [eqEventProperty("outcome", "success")],
            }, environment),
            trendsInsight({
                key: "status_update_reminder_runs_ops_30d",
                name: "Status Update Reminder Runs (Ops, 30d)",
                description: "Completed scheduler runs for stale status reminders.",
                event: "mentor_status_update_reminded",
                sourceSystem: "worker",
                filters: [eqEventProperty("outcome", "run_completed")],
            }, environment),
            trendsInsight({
                key: "request_duration_avg_seconds_30d",
                name: "Average Request Duration Until Done (seconds, 30d)",
                description: "Average request duration measured when status reaches done.",
                event: "mentor_request_status_updated",
                sourceSystem: "api",
                display: DISPLAY.NUMBER,
                math: "avg",
                mathProperty: "request_duration_seconds",
                mathPropertyType: "event",
                filters: [
                    eqEventProperty("outcome", "success"),
                    eqEventProperty("status", "done"),
                ],
            }, environment),
        ],
    );
}

function buildAuthDashboard(environment) {
    return dashboard(
        "auth_access",
        "OpenMentor - Auth & Access",
        "Passwordless login health for mentors/admins across frontend, API, and worker.",
        ["openmentor", "auth", "access"],
        [
            trendsInsight({
                key: "mentor_login_requested_outcomes_30d",
                name: "Mentor Login Requested Outcomes (30d)",
                description: "Mentor login request flow outcomes.",
                event: "mentor_auth_login_requested",
                sourceSystem: ["frontend", "api"],
                display: DISPLAY.BAR,
                breakdownKey: "outcome",
            }, environment),
            trendsInsight({
                key: "mentor_login_email_sent_outcomes_30d",
                name: "Mentor Login Email Sent Outcomes (30d)",
                description: "Mentor login email delivery outcomes from the worker.",
                event: "mentor_auth_login_email_sent",
                sourceSystem: "worker",
                display: DISPLAY.BAR,
                breakdownKey: "outcome",
            }, environment),
            trendsInsight({
                key: "mentor_login_verified_by_source_30d",
                name: "Mentor Login Verified by Source (30d)",
                description: "Successful mentor login verification split by frontend/api source.",
                event: "mentor_auth_login_verified",
                sourceSystem: ["frontend", "api"],
                display: DISPLAY.BAR,
                breakdownKey: "source_system",
                filters: [eqEventProperty("outcome", "success")],
            }, environment),
            trendsInsight({
                key: "admin_login_requested_outcomes_30d",
                name: "Admin Login Requested Outcomes (30d)",
                description: "Admin login request flow outcomes.",
                event: "admin_auth_login_requested",
                sourceSystem: ["frontend", "api"],
                display: DISPLAY.BAR,
                breakdownKey: "outcome",
            }, environment),
            trendsInsight({
                key: "admin_login_email_sent_outcomes_30d",
                name: "Admin Login Email Sent Outcomes (30d)",
                description: "Admin login email delivery outcomes from the worker.",
                event: "admin_auth_login_email_sent",
                sourceSystem: "worker",
                display: DISPLAY.BAR,
                breakdownKey: "outcome",
            }, environment),
            trendsInsight({
                key: "admin_login_verified_by_source_30d",
                name: "Admin Login Verified by Source (30d)",
                description: "Successful admin login verification split by frontend/api source.",
                event: "admin_auth_login_verified",
                sourceSystem: ["frontend", "api"],
                display: DISPLAY.BAR,
                breakdownKey: "source_system",
                filters: [eqEventProperty("outcome", "success")],
            }, environment),
            trendsInsight({
                key: "mentor_auth_errors_by_type_30d",
                name: "Mentor Auth Errors by Type (30d)",
                description: "Error-type breakdown for mentor login verification failures.",
                event: "mentor_auth_login_verified",
                sourceSystem: ["frontend", "api"],
                display: DISPLAY.BAR,
                breakdownKey: "error_type",
                filters: [eqEventProperty("outcome", "error")],
            }, environment),
            trendsInsight({
                key: "admin_auth_errors_by_type_30d",
                name: "Admin Auth Errors by Type (30d)",
                description: "Error-type breakdown for admin login verification failures.",
                event: "admin_auth_login_verified",
                sourceSystem: ["frontend", "api"],
                display: DISPLAY.BAR,
                breakdownKey: "error_type",
                filters: [eqEventProperty("outcome", "error")],
            }, environment),
            trendsInsight({
                key: "mentor_logout_events_30d",
                name: "Mentor Logout Events (30d)",
                description: "Mentor logout submissions from frontend sessions.",
                event: "mentor_auth_logout",
                sourceSystem: "frontend",
            }, environment),
            trendsInsight({
                key: "admin_logout_events_30d",
                name: "Admin Logout Events (30d)",
                description: "Admin logout submissions from frontend sessions.",
                event: "admin_auth_logout",
                sourceSystem: "frontend",
            }, environment),
        ],
    );
}

function buildWorkerNotificationsDashboard(environment) {
    return dashboard(
        "worker_notifications",
        "OpenMentor - Worker & Notification Reliability",
        "Operational reliability of the background worker's async notification pipelines.",
        ["openmentor", "worker", "notifications"],
        [
            trendsInsight({
                key: "request_finished_notifications_success_30d",
                name: "Request Finished Notifications Success (30d)",
                description: "Successful completion/decline notifications sent to mentees.",
                event: "request_process_finished_notified",
                sourceSystem: "worker",
                display: DISPLAY.NUMBER,
                filters: [eqEventProperty("outcome", "success")],
            }, environment),
            trendsInsight({
                key: "review_notifications_success_30d",
                name: "Review Notifications Success (30d)",
                description: "Successful review notification processing from the worker.",
                event: "review_submitted",
                sourceSystem: "worker",
                display: DISPLAY.NUMBER,
                filters: [eqEventProperty("outcome", "success")],
            }, environment),
            trendsInsight({
                key: "new_request_watcher_errors_30d",
                name: "New Request Watcher Errors (30d)",
                description: "Error outcomes in the request ingestion job.",
                event: "new_request_watcher_processed",
                sourceSystem: "worker",
                display: DISPLAY.BAR,
                breakdownKey: "error_type",
                filters: [eqEventProperty("outcome", "error")],
            }, environment),
            trendsInsight({
                key: "new_mentor_watcher_errors_30d",
                name: "New Mentor Watcher Errors (30d)",
                description: "Error outcomes in the mentor onboarding job.",
                event: "new_mentor_watcher_processed",
                sourceSystem: "worker",
                display: DISPLAY.BAR,
                breakdownKey: "error_type",
                filters: [eqEventProperty("outcome", "error")],
            }, environment),
            trendsInsight({
                key: "pending_requests_reminder_errors_30d",
                name: "Pending Requests Reminder Errors (30d)",
                description: "Errors in the pending requests reminder cron job.",
                event: "mentor_pending_requests_reminded",
                sourceSystem: "worker",
                display: DISPLAY.BAR,
                breakdownKey: "error_type",
                filters: [eqEventProperty("outcome", "error")],
            }, environment),
            trendsInsight({
                key: "status_update_reminder_errors_30d",
                name: "Status Update Reminder Errors (30d)",
                description: "Errors in the stale status reminder cron job.",
                event: "mentor_status_update_reminded",
                sourceSystem: "worker",
                display: DISPLAY.BAR,
                breakdownKey: "error_type",
                filters: [eqEventProperty("outcome", "error")],
            }, environment),
        ],
    );
}

export function buildDashboardSpecs(options = {}) {
    const environment = (options.environment || "production").trim() || "production";

    return [
        buildExecutiveHealthDashboard(environment),
        buildAcquisitionDashboard(environment),
        buildSupplyDashboard(environment),
        buildRequestOpsDashboard(environment),
        buildAuthDashboard(environment),
        buildWorkerNotificationsDashboard(environment),
    ];
}

