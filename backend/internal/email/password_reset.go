package email

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"net/url"
)

const passwordResetLogoCID = "vellum-mark@vellum"

// html/template removes literal comments. These developer-owned constants keep
// the fixed-width wrapper for classic Outlook; no message data is trusted HTML.
const outlookStart = template.HTML(`<!--[if mso]><table role="presentation" width="600" align="center" cellpadding="0" cellspacing="0" border="0"><tr><td><![endif]-->`)
const outlookEnd = template.HTML(`<!--[if mso]></td></tr></table><![endif]-->`)

// The approved Option B template and PNG ship inside the binary: rendering needs
// neither a writable filesystem nor a remote image host.
//
//go:embed password_reset.html
var passwordResetHTML string

//go:embed assets/vellum-mark.png
var passwordResetLogo []byte

var passwordResetTemplate = template.Must(template.New("password-reset").Parse(passwordResetHTML))

func renderPasswordResetHTML(resetURL string, minutes int) (string, error) {
	// Check the transport shape without taking over token validation or the
	// environment-specific host restrictions already enforced by config/services.
	link, err := url.Parse(resetURL)
	if err != nil || link.Scheme != "https" || link.Host == "" || link.User != nil ||
		link.Path != "/reset-password" || link.RawQuery != "" || link.ForceQuery || link.Fragment == "" {
		return "", fmt.Errorf("invalid password reset email URL")
	}
	var body bytes.Buffer
	err = passwordResetTemplate.Execute(&body, struct {
		URL                      string
		Minutes                  int
		OutlookStart, OutlookEnd template.HTML
	}{URL: resetURL, Minutes: minutes, OutlookStart: outlookStart, OutlookEnd: outlookEnd})
	if err != nil {
		// Do not include the reset URL, token or partial body in an error.
		return "", fmt.Errorf("render password reset email")
	}
	return body.String(), nil
}
