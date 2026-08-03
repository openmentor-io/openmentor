package email

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sesv2types "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

// mockSESClient captures the SendEmail input for assertions.
type mockSESClient struct {
	input *sesv2.SendEmailInput
	err   error
}

func (m *mockSESClient) SendEmail(_ context.Context, params *sesv2.SendEmailInput, _ ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error) {
	m.input = params
	if m.err != nil {
		return nil, m.err
	}
	return &sesv2.SendEmailOutput{MessageId: aws.String("test-message-id")}, nil
}

func TestSendBuildsSESParams(t *testing.T) {
	mock := &mockSESClient{}
	sender := NewSenderWithClient(mock, "production", "")

	messageID, err := sender.Send(context.Background(), Message{
		TemplateName: "mentor-login",
		Recipient:    "mentor@example.com",
		Props: map[string]interface{}{
			"mentor_name": "Jane",
			"login_url":   "https://openmentor.io/login?token=abc",
		},
	})
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if messageID != "test-message-id" {
		t.Errorf("messageID = %q, want %q", messageID, "test-message-id")
	}

	input := mock.input
	if input == nil {
		t.Fatal("SendEmail was not called")
	}

	// FromEmailAddress mirrors PostboxEmailSender: "The OpenMentor Team <hello@openmentor.io>"
	if got, want := aws.ToString(input.FromEmailAddress), "The OpenMentor Team <hello@openmentor.io>"; got != want {
		t.Errorf("FromEmailAddress = %q, want %q", got, want)
	}

	if len(input.Destination.ToAddresses) != 1 || input.Destination.ToAddresses[0] != "mentor@example.com" {
		t.Errorf("ToAddresses = %v, want [mentor@example.com]", input.Destination.ToAddresses)
	}

	// Rendering happens locally, so SES receives Simple content, not a
	// Template + TemplateData pair for SES to substitute.
	if input.Content.Template != nil {
		t.Error("Content.Template must be nil: SES must not render the template")
	}
	simple := input.Content.Simple
	if simple == nil {
		t.Fatal("Content.Simple is nil")
	}

	if got := aws.ToString(simple.Subject.Data); got != "Your OpenMentor sign-in link" {
		t.Errorf("Subject = %q, want the mentor-login subject", got)
	}
	// The subjects and bodies contain non-ASCII characters, so every part
	// must declare UTF-8; SES defaults to 7-bit ASCII.
	for part, content := range map[string]*sesv2types.Content{
		"subject": simple.Subject, "html": simple.Body.Html, "text": simple.Body.Text,
	} {
		if got := aws.ToString(content.Charset); got != "UTF-8" {
			t.Errorf("%s charset = %q, want UTF-8", part, got)
		}
	}

	// Both bodies are fully substituted: the props appear, the markers do not.
	htmlBody := aws.ToString(simple.Body.Html.Data)
	textBody := aws.ToString(simple.Body.Text.Data)
	for part, body := range map[string]string{"html": htmlBody, "text": textBody} {
		if !strings.Contains(body, "Jane") {
			t.Errorf("%s body is missing the mentor_name value: %q", part, body)
		}
		if strings.Contains(body, "{{") {
			t.Errorf("%s body still contains unsubstituted markers", part)
		}
	}
	// html/template entity-encodes the & of a query string; the plaintext
	// body keeps the URL verbatim.
	if !strings.Contains(htmlBody, "https://openmentor.io/login?token=abc") {
		t.Errorf("html body is missing the login_url: %q", htmlBody)
	}
	if !strings.Contains(textBody, "https://openmentor.io/login?token=abc") {
		t.Errorf("text body is missing the login_url: %q", textBody)
	}
}

func TestSendAppliesDevEmailOverride(t *testing.T) {
	mock := &mockSESClient{}
	sender := NewSenderWithClient(mock, "development", "dev-inbox@example.com")

	_, err := sender.Send(context.Background(), Message{
		TemplateName: "profile-deactivated",
		Recipient:    "mentor@example.com",
		Props:        map[string]interface{}{"mentor_name": "Jane"},
	})
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if got := mock.input.Destination.ToAddresses[0]; got != "dev-inbox@example.com" {
		t.Errorf("recipient = %q, want the DEV_EMAIL_OVERRIDE address", got)
	}
}

func TestSendIgnoresOverrideInProduction(t *testing.T) {
	mock := &mockSESClient{}
	sender := NewSenderWithClient(mock, "production", "dev-inbox@example.com")

	_, err := sender.Send(context.Background(), Message{
		TemplateName: "profile-deactivated",
		Recipient:    "mentor@example.com",
		Props:        map[string]interface{}{"mentor_name": "Jane"},
	})
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if got := mock.input.Destination.ToAddresses[0]; got != "mentor@example.com" {
		t.Errorf("recipient = %q, want the original recipient in production", got)
	}
}

func TestSendUnknownTemplate(t *testing.T) {
	mock := &mockSESClient{}
	sender := NewSenderWithClient(mock, "production", "")

	_, err := sender.Send(context.Background(), Message{
		TemplateName: "no-such-template",
		Recipient:    "mentor@example.com",
	})
	if err == nil {
		t.Fatal("Send with an unknown template should fail")
	}
	if mock.input != nil {
		t.Error("SendEmail must not be called when the template is unknown")
	}
}

// Rendering locally means a prop the template needs but the caller forgot is
// a send error. SES used to substitute the empty string and deliver a
// half-written email.
func TestSendRejectsMissingProps(t *testing.T) {
	cases := []struct {
		name    string
		props   map[string]interface{}
		missing string
	}{
		{"nil props", nil, "mentor_name"},
		{"partial props", map[string]interface{}{"mentor_name": "Jane"}, "login_url"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockSESClient{}
			sender := NewSenderWithClient(mock, "production", "")

			_, err := sender.Send(context.Background(), Message{
				TemplateName: "mentor-login",
				Recipient:    "mentor@example.com",
				Props:        tc.props,
			})
			if err == nil {
				t.Fatal("Send should fail when a template placeholder has no prop")
			}
			if !strings.Contains(err.Error(), tc.missing) {
				t.Errorf("error should name the missing placeholder %q, got: %v", tc.missing, err)
			}
			if mock.input != nil {
				t.Error("SendEmail must not be called when rendering failed")
			}
		})
	}
}

func TestSendPropagatesSESError(t *testing.T) {
	mock := &mockSESClient{err: errors.New("ses unavailable")}
	sender := NewSenderWithClient(mock, "production", "")

	_, err := sender.Send(context.Background(), Message{
		TemplateName: "mentor-login",
		Recipient:    "mentor@example.com",
		Props: map[string]interface{}{
			"mentor_name": "Jane",
			"login_url":   "https://openmentor.io/login?token=abc",
		},
	})
	if err == nil {
		t.Fatal("Send should propagate SES errors")
	}
	if !strings.Contains(err.Error(), "ses unavailable") {
		t.Errorf("error should wrap the SES failure, got: %v", err)
	}
}
