package models_test

import (
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/openmentor-io/openmentor/api/internal/models"
)

// TestAllStatuses_MatchesDatabaseConstraint pins the Go status set to the
// client_requests_status_chk CHECK constraint. A status the database accepts
// but Go does not know (this is how 'reschedule' was missed) reaches the admin
// panel as an unrenderable value.
func TestAllStatuses_MatchesDatabaseConstraint(t *testing.T) {
	schema, err := os.ReadFile("../../../migrations/000001_initial_schema.up.sql")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}

	constraint := regexp.MustCompile(`(?s)CONSTRAINT client_requests_status_chk CHECK \(status IN \((.*?)\)\)`)
	match := constraint.FindSubmatch(schema)
	if match == nil {
		t.Fatal("could not find client_requests_status_chk in the initial schema")
	}

	fromSchema := []string{}
	for _, raw := range strings.Split(string(match[1]), ",") {
		value := strings.Trim(strings.TrimSpace(raw), "'\n\t ")
		if value != "" {
			fromSchema = append(fromSchema, value)
		}
	}

	fromCode := []string{}
	for _, status := range models.AllStatuses {
		fromCode = append(fromCode, string(status))
	}

	sort.Strings(fromSchema)
	sort.Strings(fromCode)
	if !reflect.DeepEqual(fromSchema, fromCode) {
		t.Errorf("AllStatuses does not match the DB CHECK constraint:\n  schema: %v\n  code:   %v", fromSchema, fromCode)
	}
}

// TestAdminUpdateStatusRequest_OneofCoversAllStatuses keeps the binding tag —
// which must be a literal — in step with AllStatuses. Without this, a status
// added to AllStatuses would still be rejected at bind time, before the
// service's IsValid() check ever runs.
func TestAdminUpdateStatusRequest_OneofCoversAllStatuses(t *testing.T) {
	field, ok := reflect.TypeOf(models.AdminUpdateStatusRequest{}).FieldByName("Status")
	if !ok {
		t.Fatal("AdminUpdateStatusRequest has no Status field")
	}

	binding := field.Tag.Get("binding")
	_, oneof, found := strings.Cut(binding, "oneof=")
	if !found {
		t.Fatalf("expected a oneof= rule in binding tag, got %q", binding)
	}

	fromTag := strings.Fields(oneof)
	fromCode := []string{}
	for _, status := range models.AllStatuses {
		fromCode = append(fromCode, string(status))
	}

	sort.Strings(fromTag)
	sort.Strings(fromCode)
	if !reflect.DeepEqual(fromTag, fromCode) {
		t.Errorf("binding oneof does not match AllStatuses:\n  tag:  %v\n  code: %v", fromTag, fromCode)
	}
}

// TestRequestStatus_IsValid covers the statuses the admin override accepts.
func TestRequestStatus_IsValid(t *testing.T) {
	for _, status := range models.AllStatuses {
		if !status.IsValid() {
			t.Errorf("expected %q to be valid", status)
		}
	}
	for _, status := range []models.RequestStatus{"", "archived", "Pending", "RESCHEDULE"} {
		if status.IsValid() {
			t.Errorf("expected %q to be invalid", status)
		}
	}
}

// TestRequestStatus_RescheduleIsNotMentorFacing documents the deliberate
// boundary: 'reschedule' is known to the admin panel but stays out of the
// mentor's own inbox, exactly as before it was named in Go.
func TestRequestStatus_RescheduleIsNotMentorFacing(t *testing.T) {
	for _, status := range models.ActiveStatuses {
		if status == models.StatusReschedule {
			t.Error("reschedule must not appear in ActiveStatuses")
		}
	}
	for _, status := range models.PastStatuses {
		if status == models.StatusReschedule {
			t.Error("reschedule must not appear in PastStatuses")
		}
	}
	// No mentor-side transition may target or leave 'reschedule'.
	for _, from := range models.AllStatuses {
		if from.CanTransitionTo(models.StatusReschedule) {
			t.Errorf("mentor flow must not allow %q -> reschedule", from)
		}
	}
	for _, to := range models.AllStatuses {
		if models.StatusReschedule.CanTransitionTo(to) {
			t.Errorf("mentor flow must not allow reschedule -> %q", to)
		}
	}
}
