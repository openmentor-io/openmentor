package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmentor-io/openmentor/api/internal/handlers"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
)

// stubConfirmationService returns whatever the test asked for.
type stubConfirmationService struct {
	already bool
	err     error
}

func (s stubConfirmationService) ConfirmEmail(_ context.Context, _ string) (bool, error) {
	return s.already, s.err
}

func (s stubConfirmationService) ResendConfirmation(_ context.Context, _ string) (bool, error) {
	return s.already, s.err
}

// TestResendConfirmationRateLimitedReturns429: the resend limit is enforced
// inside the service (that is where the mentor id exists), so it reaches the
// handler as an error and must not fall through to the 500 default.
func TestResendConfirmationRateLimitedReturns429(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := handlers.NewMentorConfirmationHandler(stubConfirmationService{err: services.ErrConfirmationResendLimited})

	router := gin.New()
	router.POST("/resend", handler.Resend)

	body, err := json.Marshal(models.ConfirmMentorEmailRequest{Token: "mcf_0123456789"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/resend", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	var resp models.ConfirmMentorEmailResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp.Success)
	assert.Equal(t, models.ConfirmationCodeRateLimited, resp.Code)
	assert.NotEmpty(t, resp.Error, "the client shows this text to the mentor")
}
