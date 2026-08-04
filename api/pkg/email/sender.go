// Package email implements the transactional email layer: an AWS SESv2
// sender for locally rendered templates (see pkg/email/templates) and the
// DEV_EMAIL_OVERRIDE recipient rerouting.
//
// It is a functional port of openmentor-func's email stack
// (lib/postbox/PostboxEmailSender.ts + lib/email/recipientOverride.ts).
// SendGrid support was intentionally dropped: SES is the only provider
// (decision D1 / D6 in docs/migration).
package email

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sesv2types "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/email/templates"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
)

const (
	// senderEmail / senderName mirror PostboxEmailSender.SENDER_EMAIL and
	// SENDER_NAME in openmentor-func/lib/postbox/PostboxEmailSender.ts.
	senderEmail = "hello@openmentor.io"
	senderName  = "The OpenMentor Team"

	// DefaultModeratorsEmail is the fallback moderators mailbox used when
	// MODERATORS_EMAIL is not configured (mirrors the func app's default).
	DefaultModeratorsEmail = "moderators@openmentor.io"
)

// SESClient is the subset of the SESv2 API used by the Sender. It allows
// tests to substitute a mock client.
type SESClient interface {
	SendEmail(ctx context.Context, params *sesv2.SendEmailInput, optFns ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error)
}

// Config configures the SESv2 sender. The env names match the func app:
// SES_REGION, SES_ACCESS_KEY_ID, SES_SECRET_ACCESS_KEY and the optional
// SES_ENDPOINT (points at any SESv2-compatible service, e.g. a local test
// double). AppEnv and DevEmailOverride drive recipient rerouting.
type Config struct {
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	Endpoint        string

	AppEnv           string
	DevEmailOverride string
}

// Sender sends transactional emails via the AWS SESv2 API, rendering the
// templates from pkg/email/templates locally.
type Sender struct {
	client           SESClient
	appEnv           string
	devEmailOverride string
}

// NewSender builds a Sender backed by a real SESv2 client.
func NewSender(cfg Config) *Sender {
	awsCfg := aws.Config{
		Region: cfg.Region,
		Credentials: credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID,
			cfg.SecretAccessKey,
			"",
		),
	}

	client := sesv2.NewFromConfig(awsCfg, func(o *sesv2.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
	})

	return NewSenderWithClient(client, cfg.AppEnv, cfg.DevEmailOverride)
}

// NewSenderWithClient builds a Sender with a caller-provided SES client
// (used by tests to inject a mock).
func NewSenderWithClient(client SESClient, appEnv, devEmailOverride string) *Sender {
	return &Sender{
		client:           client,
		appEnv:           appEnv,
		devEmailOverride: devEmailOverride,
	}
}

// BuildSendEmailInput constructs the SESv2 SendEmail parameters for a
// message, mirroring what PostboxEmailSender.send() assembles: the fixed
// FromEmailAddress, the (possibly overridden) recipient and the message
// body. The template is rendered here rather than by SES, so the content is
// Simple (Subject + Body.Html + Body.Text) instead of Template +
// TemplateData; that is what gives each part the escaping it needs.
func (s *Sender) BuildSendEmailInput(msg Message) (*sesv2.SendEmailInput, error) {
	rendered, err := templates.Render(msg.TemplateName, msg.Props)
	if err != nil {
		return nil, err
	}

	// In non-production, DEV_EMAIL_OVERRIDE reroutes all emails to a dev inbox.
	recipient := ResolveRecipient(msg.Recipient, s.devEmailOverride, s.appEnv)

	return &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(fmt.Sprintf("%s <%s>", senderName, senderEmail)),
		Destination: &sesv2types.Destination{
			ToAddresses: []string{recipient},
		},
		Content: &sesv2types.EmailContent{
			Simple: &sesv2types.Message{
				Subject: utf8Content(rendered.Subject),
				Body: &sesv2types.Body{
					Html: utf8Content(rendered.HTML),
					Text: utf8Content(rendered.Text),
				},
			},
		},
	}, nil
}

// utf8Content wraps a rendered part for SES. The charset must be declared:
// the subjects and bodies contain em dashes and other non-ASCII characters,
// and SES defaults to 7-bit ASCII.
func utf8Content(data string) *sesv2types.Content {
	return &sesv2types.Content{
		Data:    aws.String(data),
		Charset: aws.String("UTF-8"),
	}
}

// Send renders the template, sends the email via SES and returns the SES
// MessageId.
func (s *Sender) Send(ctx context.Context, msg Message) (string, error) {
	start := time.Now()

	input, err := s.BuildSendEmailInput(msg)
	if err != nil {
		return "", err
	}

	resp, err := s.client.SendEmail(ctx, input)
	duration := time.Since(start)
	if err != nil {
		logger.Error("SES email send failed",
			zap.String("template", msg.TemplateName),
			zap.String("recipient", msg.Recipient),
			zap.Duration("duration", duration),
			zap.Error(err),
		)
		return "", fmt.Errorf("failed to send email via SES: %w", err)
	}

	messageID := aws.ToString(resp.MessageId)
	logger.Info("SES email sent",
		zap.String("template", msg.TemplateName),
		zap.String("recipient", input.Destination.ToAddresses[0]),
		zap.String("message_id", messageID),
		zap.Duration("duration", duration),
	)

	return messageID, nil
}
