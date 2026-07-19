package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmentor-io/openmentor/api/pkg/httpclient"
)

// newTestInviter wires an Inviter against a stub Slack API returning the
// given response body, and captures the request it receives.
func newTestInviter(t *testing.T, responseBody string) (*Inviter, *http.Request, *[]byte) {
	t.Helper()

	var captured http.Request
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = *r
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responseBody))
	}))
	t.Cleanup(server.Close)

	inviter := NewInviter(Config{
		Token:      "xoxp-test-token",
		TeamID:     "T0123456789",
		ChannelIDs: []string{"C111", "C222"},
		BaseURL:    server.URL,
	}, httpclient.NewStandardClient())
	return inviter, &captured, &capturedBody
}

func TestInviteByEmailSendsExpectedRequest(t *testing.T) {
	inviter, req, body := newTestInviter(t, `{"ok":true}`)

	err := inviter.InviteByEmail(context.Background(), "john@example.com")
	require.NoError(t, err)

	assert.Equal(t, "/admin.users.invite", req.URL.Path)
	assert.Equal(t, http.MethodPost, req.Method)
	assert.Equal(t, "Bearer xoxp-test-token", req.Header.Get("Authorization"))

	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(*body, &payload))
	assert.Equal(t, "T0123456789", payload["team_id"])
	assert.Equal(t, "john@example.com", payload["email"])
	assert.Equal(t, "C111,C222", payload["channel_ids"])
	assert.Equal(t, true, payload["resend"])
}

func TestInviteByEmailTreatsAlreadyInTeamAsSuccess(t *testing.T) {
	for _, code := range []string{"already_in_team", "already_in_team_invited_user"} {
		inviter, _, _ := newTestInviter(t, `{"ok":false,"error":"`+code+`"}`)
		assert.NoError(t, inviter.InviteByEmail(context.Background(), "john@example.com"), code)
	}
}

func TestInviteByEmailSurfacesAPIError(t *testing.T) {
	inviter, _, _ := newTestInviter(t, `{"ok":false,"error":"invalid_email"}`)

	err := inviter.InviteByEmail(context.Background(), "not-an-email")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_email")
}

func TestInviteByEmailSurfacesTransportError(t *testing.T) {
	inviter := NewInviter(Config{
		Token:   "xoxp-test-token",
		TeamID:  "T0123456789",
		BaseURL: "http://127.0.0.1:1", // nothing listens here
	}, httpclient.NewStandardClient())

	err := inviter.InviteByEmail(context.Background(), "john@example.com")
	assert.Error(t, err)
}
