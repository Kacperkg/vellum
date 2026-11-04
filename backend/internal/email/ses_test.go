package email

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/kacperkg/vellum/internal/services"
)

type sesFake struct {
	input *sesv2.SendEmailInput
	err   error
	ctx   context.Context
}

func (s *sesFake) SendEmail(ctx context.Context, input *sesv2.SendEmailInput, _ ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error) {
	s.input = input
	s.ctx = ctx
	return &sesv2.SendEmailOutput{}, s.err
}
func TestSESMultipartPasswordReset(t *testing.T) {
	client := &sesFake{}
	sender := NewSESSender(client, "no-reply@kacpergajdarski.com")
	email := services.ResetEmail{To: "recipient@example.com", URL: "https://vellum.example/reset-password#token=" + syntheticResetToken + "&x=1", ExpiresIn: 20 * time.Minute}
	if err := sender.SendPasswordReset(context.Background(), email); err != nil {
		t.Fatal(err)
	}
	input := client.input
	if *input.FromEmailAddress != "no-reply@kacpergajdarski.com" || len(input.Destination.ToAddresses) != 1 || input.Destination.ToAddresses[0] != email.To {
		t.Fatal("invalid envelope")
	}
	message := input.Content.Simple
	if !strings.Contains(*message.Body.Text.Data, email.URL) || !strings.Contains(*message.Body.Html.Data, syntheticResetToken+"&amp;x=1") || !strings.Contains(*message.Body.Text.Data, "20 minutes") || !strings.Contains(*message.Body.Text.Data, "ignore") {
		t.Fatal("multipart content/escaping missing")
	}
	if strings.Contains(*message.Body.Html.Data, "<script") || strings.Count(*message.Body.Html.Data, "<img") != 1 || !strings.Contains(*message.Body.Html.Data, `src="cid:`+passwordResetLogoCID+`"`) {
		t.Fatal("external or tracking resources")
	}
	client.err = errors.New("SES unavailable")
	if err := sender.SendPasswordReset(context.Background(), email); !errors.Is(err, client.err) {
		t.Fatal("SES error swallowed")
	}
}
