package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/openmentor-io/openmentor/api/internal/services"
)

// TestRespondServiceErrorMapsTheConcurrencyConflict pins the status code for a
// lost compare-and-set: an admin acting on a stale list is an EXPECTED outcome
// of two people touching one request, so it must answer 409 — falling through
// to 500 would page on-call for a race the service refused correctly.
func TestRespondServiceErrorMapsTheConcurrencyConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewAdminMentorRequestsHandler(nil)

	cases := []struct {
		name string
		err  error
		want int
	}{
		{
			"lost compare-and-set answers 409",
			fmt.Errorf("%w: request is no longer 'pending'", services.ErrInvalidStatusTransition),
			http.StatusConflict,
		},
		{"forbidden stays 403", services.ErrAdminForbiddenAction, http.StatusForbidden},
		{"not found stays 404", services.ErrRequestNotFound, http.StatusNotFound},
		{"unsupported transition stays 400", fmt.Errorf("unsupported admin transition"), http.StatusBadRequest},
		{"unknown errors stay 500", fmt.Errorf("connection refused"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/mentors/requests/1/status", http.NoBody)
			h.respondServiceError(c, tc.err)
			if recorder.Code != tc.want {
				t.Errorf("respondServiceError(%v) = %d, want %d", tc.err, recorder.Code, tc.want)
			}
		})
	}
}
