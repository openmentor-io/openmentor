// Package slack invites newly approved mentors to the community Slack
// workspace by email, via the official admin.users.invite Web API method.
// NOTE: admin.users.invite is only available on Slack Enterprise Grid
// organizations and requires a USER (xoxp-) token from an org admin/owner
// with the admin.users:write scope — a plain bot token will not work.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/openmentor-io/openmentor/api/pkg/httpclient"
)

// DefaultAPIBaseURL is the Slack Web API root.
const DefaultAPIBaseURL = "https://slack.com/api"

// Config configures the workspace inviter.
type Config struct {
	Token      string   // org admin/owner user OAuth token (xoxp-…) with admin.users:write
	TeamID     string   // workspace (team) id the invite targets, e.g. T0123456789
	ChannelIDs []string // channels the invited user lands in (admin.users.invite requires ≥1)
	BaseURL    string   // optional API root override (tests); empty = DefaultAPIBaseURL
}

// Inviter sends workspace invites through admin.users.invite.
type Inviter struct {
	cfg  Config
	http httpclient.Client
}

// NewInviter creates a workspace inviter.
func NewInviter(cfg Config, client httpclient.Client) *Inviter {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultAPIBaseURL
	}
	return &Inviter{cfg: cfg, http: client}
}

// inviteRequest is the admin.users.invite JSON body.
type inviteRequest struct {
	TeamID     string `json:"team_id"`
	Email      string `json:"email"`
	ChannelIDs string `json:"channel_ids"` // comma-separated per the API contract
	Resend     bool   `json:"resend"`      // re-send if a previous invite is still pending
}

// apiResponse is the envelope every Slack Web API method returns.
type apiResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

// InviteByEmail invites the given address to the configured workspace.
// Idempotent against replays: the "already a member / already invited"
// API errors are treated as success, and pending invites are re-sent
// (resend=true) rather than rejected.
func (i *Inviter) InviteByEmail(ctx context.Context, email string) error {
	body, err := json.Marshal(inviteRequest{
		TeamID:     i.cfg.TeamID,
		Email:      email,
		ChannelIDs: strings.Join(i.cfg.ChannelIDs, ","),
		Resend:     true,
	})
	if err != nil {
		return fmt.Errorf("failed to encode slack invite: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, i.cfg.BaseURL+"/admin.users.invite", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build slack invite request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+i.cfg.Token)

	resp, err := i.http.Do(req)
	if err != nil {
		return fmt.Errorf("slack invite request failed: %w", err)
	}
	defer resp.Body.Close()

	var result apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode slack invite response (HTTP %d): %w", resp.StatusCode, err)
	}
	if result.OK || benignInviteError(result.Error) {
		return nil
	}
	return fmt.Errorf("slack invite rejected: %s", result.Error)
}

// benignInviteError reports the admin.users.invite errors that mean the
// mentor is already in (or already on their way into) the workspace.
func benignInviteError(code string) bool {
	switch code {
	case "already_in_team", "already_in_team_invited_user":
		return true
	}
	return false
}
