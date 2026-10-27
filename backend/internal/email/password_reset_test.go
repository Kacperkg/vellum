package email

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/kacperkg/vellum/internal/services"
	"golang.org/x/net/html"
)

const syntheticResetToken = "PREVIEW_ONLY_000000000000000000000000000000"
const syntheticResetURL = "https://vellum.example/reset-password#token=" + syntheticResetToken

func renderedDocument(t *testing.T, body string) []*html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var elements []*html.Node
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode {
			elements = append(elements, node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(doc)
	return elements
}

func attribute(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func TestPasswordResetConfiguredExpiryAndUnchangedPlainText(t *testing.T) {
	for _, ttl := range []time.Duration{30 * time.Second, time.Minute, 7*time.Minute + time.Second, 37 * time.Minute, time.Hour} {
		t.Run(ttl.String(), func(t *testing.T) {
			client := &sesFake{}
			if err := NewSESSender(client, "sender@example.com").SendPasswordReset(context.Background(), services.ResetEmail{To: "recipient@example.com", URL: syntheticResetURL, ExpiresIn: ttl}); err != nil {
				t.Fatal(err)
			}
			minutes := int((ttl + time.Minute - 1) / time.Minute)
			message := client.input.Content.Simple
			wantText := fmt.Sprintf("A password reset was requested for your Vellum account.\n\nReset your password: %s\n\nThis link expires in %d minutes and can be used once.\nIf you did not request this, ignore this email. Your password has not changed.\n", syntheticResetURL, minutes)
			if aws.ToString(message.Body.Text.Data) != wantText {
				t.Fatal("existing plain-text alternative changed")
			}
			if strings.Count(aws.ToString(message.Body.Html.Data), fmt.Sprintf("%d minutes", minutes)) != 2 {
				t.Fatal("configured expiry missing from preheader or visible panel")
			}
			if aws.ToString(message.Subject.Data) != "Reset your Vellum password" || aws.ToString(message.Subject.Charset) != "UTF-8" || aws.ToString(message.Body.Text.Charset) != "UTF-8" || aws.ToString(message.Body.Html.Charset) != "UTF-8" {
				t.Fatal("subject/charset contract changed")
			}
		})
	}
}

func TestPasswordResetInlineLogoAndEnvelope(t *testing.T) {
	client := &sesFake{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sender := NewSESSender(client, "sender@example.com")
	request := services.ResetEmail{To: "recipient@example.com", URL: syntheticResetURL, ExpiresIn: 20 * time.Minute}
	if err := sender.SendPasswordReset(ctx, request); err != nil {
		t.Fatal(err)
	}
	input := client.input
	if client.ctx != ctx || aws.ToString(input.FromEmailAddress) != "sender@example.com" || len(input.Destination.ToAddresses) != 1 || input.Destination.ToAddresses[0] != request.To || len(input.Destination.CcAddresses) != 0 || len(input.Destination.BccAddresses) != 0 || input.ConfigurationSetName != nil || len(input.EmailTags) != 0 || len(input.ReplyToAddresses) != 0 || input.Content.Raw != nil || input.Content.Template != nil {
		t.Fatal("sender context, envelope or restricted Simple SendEmail path changed")
	}
	attachments := input.Content.Simple.Attachments
	if len(attachments) != 1 {
		t.Fatal("expected only the inline logo")
	}
	logo := attachments[0]
	if aws.ToString(logo.ContentId) != passwordResetLogoCID || aws.ToString(logo.FileName) != "vellum-mark.png" || aws.ToString(logo.ContentType) != "image/png" || logo.ContentDisposition != types.AttachmentContentDispositionInline || logo.ContentTransferEncoding != types.AttachmentContentTransferEncodingBase64 || !bytes.Equal(logo.RawContent, passwordResetLogo) {
		t.Fatal("inline logo metadata/bytes do not match the template CID")
	}
	decoded, err := png.Decode(bytes.NewReader(logo.RawContent))
	if err != nil || decoded.Bounds().Dx() != 128 || decoded.Bounds().Dy() != 128 || len(logo.RawContent) > 16*1024 {
		t.Fatal("logo is not a bounded 128px PNG")
	}
	// A client mutating its request must not corrupt the asset for later sends.
	logo.RawContent[0] ^= 0xff
	if err := sender.SendPasswordReset(ctx, request); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(client.input.Content.Simple.Attachments[0].RawContent, passwordResetLogo) {
		t.Fatal("request attachment aliases the embedded logo")
	}
}

func TestPasswordResetAccessibleOfflineMarkup(t *testing.T) {
	body, err := renderPasswordResetHTML(syntheticResetURL, 17)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, string(outlookStart)) || !strings.Contains(body, string(outlookEnd)) {
		t.Fatal("classic Outlook wrapper stripped during rendering")
	}
	counts := make(map[string]int)
	allowed := map[string]bool{"html": true, "head": true, "meta": true, "title": true, "body": true, "div": true, "table": true, "tbody": true, "tr": true, "td": true, "img": true, "p": true, "h1": true, "strong": true, "br": true, "a": true}
	for _, element := range renderedDocument(t, body) {
		counts[element.Data]++
		if !allowed[element.Data] {
			t.Errorf("unexpected email element %s", element.Data)
		}
		for _, attr := range element.Attr {
			if strings.HasPrefix(attr.Key, "on") || attr.Key == "srcset" || (attr.Key == "style" && (strings.Contains(attr.Val, "url(") || strings.Contains(attr.Val, "@import"))) {
				t.Errorf("active or remote resource attribute %s", attr.Key)
			}
		}
		switch element.Data {
		case "html":
			if attribute(element, "lang") != "en" {
				t.Fatal("missing document language")
			}
		case "table":
			if attribute(element, "role") != "presentation" {
				t.Fatal("layout table exposed as data table")
			}
		case "a":
			if attribute(element, "href") != syntheticResetURL || attribute(element, "rel") != "noreferrer" {
				t.Fatal("reset URL or referrer suppression changed")
			}
		case "img":
			if attribute(element, "src") != "cid:"+passwordResetLogoCID || attribute(element, "width") != "34" || attribute(element, "height") != "34" || attribute(element, "alt") != "" {
				t.Fatal("unexpected logo resource or duplicate accessible brand name")
			}
		}
	}
	if counts["img"] != 1 || counts["a"] != 2 || counts["h1"] != 1 {
		t.Fatal("expected one logo, heading and two reset links")
	}
	for _, expected := range []string{"Vellum", "Reset password", "Single use", "Your password has not changed.", syntheticResetURL, "This is an automated password-reset email.", "background-color:#0b0c0e", "background-color:#15161a", "background-color:#222328", "background-color:#ff246c", "font-size:16px;", "border:16px solid #ff246c", "border-radius:6px"} {
		if !strings.Contains(body, expected) {
			t.Errorf("approved design/copy missing %q", expected)
		}
	}
}

func TestPasswordResetEscapesURLInAttributesAndVisibleFallback(t *testing.T) {
	url := syntheticResetURL + `&x=" onmouseover="alert(1)"><script>alert(2)</script>&name='sample'`
	body, err := renderPasswordResetHTML(url, 37)
	if err != nil {
		t.Fatal(err)
	}
	links := 0
	for _, element := range renderedDocument(t, body) {
		if element.Data == "script" {
			t.Fatal("URL created executable markup")
		}
		for _, attr := range element.Attr {
			if strings.HasPrefix(attr.Key, "on") {
				t.Fatal("URL escaped its attribute")
			}
		}
		if element.Data == "a" {
			links++
			if !strings.HasPrefix(attribute(element, "href"), syntheticResetURL+"&x=%22%20onmouseover=") {
				t.Fatal("attribute URL was truncated or escaped incorrectly")
			}
		}
	}
	if links != 2 || !strings.Contains(body, "&lt;script&gt;alert(2)&lt;/script&gt;") || strings.Contains(body, `<script>alert(2)</script>`) {
		t.Fatal("visible URL fallback did not escape hostile text")
	}
}

func TestPasswordResetRejectsUnsafeLinksWithoutSending(t *testing.T) {
	for _, url := range []string{"javascript:alert(1)", "data:text/html,PREVIEW_ONLY", "http://vellum.example/reset-password#token=" + syntheticResetToken, "//vellum.example/reset-password#token=" + syntheticResetToken, "https://user@vellum.example/reset-password#token=" + syntheticResetToken, "https://vellum.example/reset-password?token=" + syntheticResetToken, "https://vellum.example/reset-password?#token=" + syntheticResetToken, "https://vellum.example/reset-password", "https://vellum.example/other#token=" + syntheticResetToken, "https://vellum.example/reset-password\n#token=" + syntheticResetToken} {
		client := &sesFake{}
		err := NewSESSender(client, "sender@example.com").SendPasswordReset(context.Background(), services.ResetEmail{To: "recipient@example.com", URL: url, ExpiresIn: time.Minute})
		if err == nil || client.input != nil || strings.Contains(err.Error(), syntheticResetToken) || strings.Contains(err.Error(), "recipient@example.com") {
			t.Fatal("unsafe link sent, or sensitive input exposed in error")
		}
	}
}

func TestPasswordResetConcurrentRendersAreIndependent(t *testing.T) {
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			url := fmt.Sprintf("https://vellum.example/reset-password#token=PREVIEW_ONLY_%030d", i)
			body, err := renderPasswordResetHTML(url, i+1)
			if err != nil || strings.Count(body, url) != 3 || strings.Count(body, fmt.Sprintf("%d minutes", i+1)) != 2 {
				t.Error("concurrent rendering lost or mixed message data")
			}
		})
	}
	wg.Wait()
}
