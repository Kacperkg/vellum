package email

import (
	"context"
	"fmt"
	"html"
	"math"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/kacperkg/vellum/internal/services"
)

type SESClient interface {
	SendEmail(context.Context, *sesv2.SendEmailInput, ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error)
}
type SESSender struct {
	client SESClient
	from   string
}

func NewSESSender(client SESClient, from string) *SESSender {
	return &SESSender{client: client, from: from}
}

// NewAWSSESSender uses the default credential chain, including EC2 instance-role
// credentials. It performs no send and never creates or changes AWS resources.
func NewAWSSESSender(ctx context.Context, region, from string, timeout time.Duration) (*SESSender, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region), awsconfig.WithRetryMaxAttempts(1), awsconfig.WithHTTPClient(&http.Client{Timeout: timeout}))
	if err != nil {
		return nil, fmt.Errorf("load AWS email configuration: %w", err)
	}
	return NewSESSender(sesv2.NewFromConfig(cfg, func(o *sesv2.Options) { o.RetryMaxAttempts = 1 }), from), nil
}
func (s *SESSender) SendPasswordReset(ctx context.Context, email services.ResetEmail) error {
	minutes := int(math.Ceil(email.ExpiresIn.Minutes()))
	text := fmt.Sprintf("A password reset was requested for your Vellum account.\n\nReset your password: %s\n\nThis link expires in %d minutes and can be used once.\nIf you did not request this, ignore this email. Your password has not changed.\n", email.URL, minutes)
	body := fmt.Sprintf(`<html><body><p>A password reset was requested for your Vellum account.</p><p><a href="%s">Reset your password</a></p><p>This link expires in %d minutes and can be used once.</p><p>If you did not request this, ignore this email. Your password has not changed.</p></body></html>`, html.EscapeString(email.URL), minutes)
	_, err := s.client.SendEmail(ctx, &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(s.from),
		Destination:      &types.Destination{ToAddresses: []string{email.To}},
		Content: &types.EmailContent{Simple: &types.Message{
			Subject: &types.Content{Data: aws.String("Reset your Vellum password"), Charset: aws.String("UTF-8")},
			Body:    &types.Body{Text: &types.Content{Data: aws.String(text), Charset: aws.String("UTF-8")}, Html: &types.Content{Data: aws.String(body), Charset: aws.String("UTF-8")}},
		}},
	})
	return err
}
