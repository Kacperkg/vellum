// preview-password-reset-email captures the real SES sender's output without
// constructing an AWS client, loading credentials or sending a message.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/kacperkg/vellum/internal/email"
	"github.com/kacperkg/vellum/internal/services"
)

type captureClient struct{ input *sesv2.SendEmailInput }

func (c *captureClient) SendEmail(_ context.Context, input *sesv2.SendEmailInput, _ ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error) {
	c.input = input
	return &sesv2.SendEmailOutput{}, nil
}

func main() {
	output := flag.String("out", "", "output directory for synthetic preview files (required)")
	ttl := flag.Duration("ttl", 20*time.Minute, "sample lifetime, matching PASSWORD_RESET_TOKEN_TTL semantics")
	flag.Parse()
	if *output == "" || *ttl <= 0 || *ttl > time.Hour {
		fmt.Fprintln(os.Stderr, "provide -out and a positive -ttl of at most 1h")
		os.Exit(2)
	}
	if err := writePreview(*output, *ttl); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func writePreview(output string, ttl time.Duration) error {
	capture := &captureClient{}
	err := email.NewSESSender(capture, "sender@example.com").SendPasswordReset(context.Background(), services.ResetEmail{
		To: "recipient@example.com", URL: "https://vellum.example/reset-password#token=PREVIEW_ONLY_000000000000000000000000000000", ExpiresIn: ttl,
	})
	if err != nil {
		return err
	}
	message := capture.input.Content.Simple
	logo := message.Attachments[0]
	if err := os.MkdirAll(filepath.Join(output, "assets"), 0o755); err != nil {
		return err
	}
	production := aws.ToString(message.Body.Html.Data)
	// The HTML sent to SES keeps cid:. Only the browser copy resolves the captured
	// attachment to a local PNG; all other production markup is unchanged.
	preview := strings.ReplaceAll(production, "cid:"+aws.ToString(logo.ContentId), "assets/vellum-mark.png")
	for name, content := range map[string][]byte{
		"production.html":        []byte(production),
		"preview.html":           []byte(preview),
		"plain-text.txt":         []byte(aws.ToString(message.Body.Text.Data)),
		"assets/vellum-mark.png": logo.RawContent,
	} {
		if err := os.WriteFile(filepath.Join(output, name), content, 0o644); err != nil {
			return err
		}
	}
	return nil
}
