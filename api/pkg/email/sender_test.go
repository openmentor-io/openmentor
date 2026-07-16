package email

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
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

	tplContent := input.Content.Template.TemplateContent
	if got := aws.ToString(tplContent.Subject); got != "Your OpenMentor sign-in link" {
		t.Errorf("Subject = %q, want the mentor-login subject", got)
	}
	if aws.ToString(tplContent.Html) == "" || aws.ToString(tplContent.Text) == "" {
		t.Error("Html and Text template content must not be empty")
	}

	var templateData map[string]interface{}
	if err := json.Unmarshal([]byte(aws.ToString(input.Content.Template.TemplateData)), &templateData); err != nil {
		t.Fatalf("TemplateData is not valid JSON: %v", err)
	}
	if templateData["mentor_name"] != "Jane" {
		t.Errorf("TemplateData mentor_name = %v, want Jane", templateData["mentor_name"])
	}
	if templateData["login_url"] != "https://openmentor.io/login?token=abc" {
		t.Errorf("TemplateData login_url = %v", templateData["login_url"])
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

func TestSendNilPropsMarshalsEmptyObject(t *testing.T) {
	mock := &mockSESClient{}
	sender := NewSenderWithClient(mock, "production", "")

	_, err := sender.Send(context.Background(), Message{
		TemplateName: "mentor-login",
		Recipient:    "mentor@example.com",
	})
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if got := aws.ToString(mock.input.Content.Template.TemplateData); got != "{}" {
		t.Errorf("TemplateData = %q, want {}", got)
	}
}

func TestSendPropagatesSESError(t *testing.T) {
	mock := &mockSESClient{err: errors.New("ses unavailable")}
	sender := NewSenderWithClient(mock, "production", "")

	_, err := sender.Send(context.Background(), Message{
		TemplateName: "mentor-login",
		Recipient:    "mentor@example.com",
	})
	if err == nil {
		t.Fatal("Send should propagate SES errors")
	}
}
