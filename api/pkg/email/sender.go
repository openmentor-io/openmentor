// Package email implements the transactional email layer: an AWS SESv2
// sender with inline templates ({{placeholder}} rendering is performed
// server-side by SES) and the DEV_EMAIL_OVERRIDE recipient rerouting.
//
// It is a functional port of openmentor-func's email stack
// (lib/postbox/PostboxEmailSender.ts + lib/email/recipientOverride.ts).
// SendGrid support was intentionally dropped: SES is the only provider
// (decision D1 / D6 in docs/migration).
package email

import (
	"context"
	"encoding/json"
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

// Sender sends transactional emails via the AWS SESv2 API using inline
// templates from pkg/email/templates.
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
// FromEmailAddress, the (possibly overridden) recipient and the inline
// template content with JSON-encoded TemplateData.
func (s *Sender) BuildSendEmailInput(msg Message) (*sesv2.SendEmailInput, error) {
	tpl, err := templates.GetTemplate(msg.TemplateName)
	if err != nil {
		return nil, err
	}

	// In non-production, DEV_EMAIL_OVERRIDE reroutes all emails to a dev inbox.
	recipient := ResolveRecipient(msg.Recipient, s.devEmailOverride, s.appEnv)

	props := msg.Props
	if props == nil {
		props = map[string]interface{}{}
	}
	templateData, err := json.Marshal(props)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal template data for %s: %w", msg.TemplateName, err)
	}

	return &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(fmt.Sprintf("%s <%s>", senderName, senderEmail)),
		Destination: &sesv2types.Destination{
			ToAddresses: []string{recipient},
		},
		Content: &sesv2types.EmailContent{
			Template: &sesv2types.Template{
				TemplateContent: &sesv2types.EmailTemplateContent{
					Subject: aws.String(tpl.Subject),
					Html:    aws.String(tpl.HTML),
					Text:    aws.String(tpl.Text),
				},
				TemplateData: aws.String(string(templateData)),
			},
		},
	}, nil
}

// Send sends an email via SES and returns the SES MessageId.
// Template rendering is performed server-side by SES.
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
