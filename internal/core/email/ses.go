package email

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sestypes "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

// SESSender delivers through Amazon SES v2. Credentials resolve via
// the standard AWS chain (env vars, shared config, IRSA, instance
// profile) — same story as every other AWS integration in the tree.
// The From address must belong to a verified SES identity (domain or
// address) in the configured region.
type SESSender struct {
	client *sesv2.Client
}

// NewSESSender builds the client. Region is required — SES identities
// are regional, so guessing would mis-deliver in multi-region accounts.
func NewSESSender(ctx context.Context, region string) (*SESSender, error) {
	if region == "" {
		return nil, fmt.Errorf("email/ses: region required")
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("email/ses: load aws config: %w", err)
	}
	return &SESSender{client: sesv2.NewFromConfig(cfg)}, nil
}

func (s *SESSender) Send(ctx context.Context, m Message) error {
	if len(m.To) == 0 {
		return fmt.Errorf("email/ses: no recipients")
	}
	body := &sestypes.Body{}
	if m.HTML != "" {
		body.Html = &sestypes.Content{Data: aws.String(m.HTML)}
	}
	if m.Text != "" {
		body.Text = &sestypes.Content{Data: aws.String(m.Text)}
	}
	input := &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(m.From),
		Destination:      &sestypes.Destination{ToAddresses: m.To},
		Content: &sestypes.EmailContent{
			Simple: &sestypes.Message{
				Subject: &sestypes.Content{Data: aws.String(m.Subject)},
				Body:    body,
			},
		},
	}
	if m.Tag != "" {
		input.EmailTags = []sestypes.MessageTag{
			{Name: aws.String("tag"), Value: aws.String(m.Tag)},
		}
	}
	if _, err := s.client.SendEmail(ctx, input); err != nil {
		return fmt.Errorf("email/ses: send to %v: %w", m.To, err)
	}
	return nil
}
