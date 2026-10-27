package config

import (
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"
)

type PasswordReset struct {
	Mode              string
	Environment       string
	Region            string
	FromAddress       string
	FrontendURL       string
	TokenTTL          time.Duration
	Cooldown          time.Duration
	QueueSize         int
	SendTimeout       time.Duration
	AllowedRecipients []string
}

func loadPasswordReset() (PasswordReset, error) {
	ttl, err := time.ParseDuration(valueOrDefault("PASSWORD_RESET_TOKEN_TTL", "20m"))
	if err != nil {
		return PasswordReset{}, fmt.Errorf("invalid PASSWORD_RESET_TOKEN_TTL")
	}
	cooldown, err := time.ParseDuration(valueOrDefault("PASSWORD_RESET_COOLDOWN", "1m"))
	if err != nil {
		return PasswordReset{}, fmt.Errorf("invalid PASSWORD_RESET_COOLDOWN")
	}
	timeout, err := time.ParseDuration(valueOrDefault("PASSWORD_RESET_SEND_TIMEOUT", "5s"))
	if err != nil {
		return PasswordReset{}, fmt.Errorf("invalid PASSWORD_RESET_SEND_TIMEOUT")
	}
	c := PasswordReset{
		Mode: valueOrDefault("PASSWORD_RESET_EMAIL_MODE", "disabled"), Environment: valueOrDefault("APP_ENV", "development"), Region: valueOrDefault("AWS_REGION", "eu-west-2"),
		FromAddress: valueOrDefault("SES_FROM_ADDRESS", ""), FrontendURL: valueOrDefault("PASSWORD_RESET_FRONTEND_URL", ""),
		TokenTTL: ttl, Cooldown: cooldown, QueueSize: positiveInt("PASSWORD_RESET_QUEUE_SIZE", 32), SendTimeout: timeout,
		AllowedRecipients: csvValues(valueOrDefault("PASSWORD_RESET_ALLOWED_RECIPIENTS", "")),
	}
	return c, c.Validate()
}

func (c PasswordReset) Validate() error {
	if c.Mode != "disabled" && c.Mode != "ses" {
		return fmt.Errorf("PASSWORD_RESET_EMAIL_MODE must be disabled or ses")
	}
	if c.TokenTTL <= 0 || c.TokenTTL > time.Hour {
		return fmt.Errorf("PASSWORD_RESET_TOKEN_TTL must be positive and at most 1h")
	}
	if c.Cooldown < time.Minute || c.Cooldown > 24*time.Hour {
		return fmt.Errorf("PASSWORD_RESET_COOLDOWN must be between 1m and 24h")
	}
	if c.QueueSize < 1 || c.QueueSize > 256 {
		return fmt.Errorf("PASSWORD_RESET_QUEUE_SIZE must be between 1 and 256")
	}
	if c.SendTimeout <= 0 || c.SendTimeout > 10*time.Second {
		return fmt.Errorf("PASSWORD_RESET_SEND_TIMEOUT must be positive and at most 10s")
	}
	if c.Mode == "disabled" {
		return nil
	}
	if c.Environment != "staging" && c.Environment != "production" {
		return fmt.Errorf("SES delivery requires APP_ENV=staging or production")
	}
	if c.Region != "eu-west-2" {
		return fmt.Errorf("SES delivery requires AWS_REGION=eu-west-2")
	}
	if !validMailbox(c.FromAddress) {
		return fmt.Errorf("SES_FROM_ADDRESS must be a plain email address")
	}
	expectedSender := "no-reply@kacpergajdarski.com"
	if c.Environment == "staging" {
		expectedSender = "no-reply-staging@kacpergajdarski.com"
	}
	if c.FromAddress != expectedSender {
		return fmt.Errorf("SES_FROM_ADDRESS does not match APP_ENV")
	}
	u, err := url.Parse(c.FrontendURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/reset-password" {
		return fmt.Errorf("PASSWORD_RESET_FRONTEND_URL must be an absolute HTTPS /reset-password URL without credentials, query or fragment")
	}
	// Exact environment hosts prevent accidentally emailing staging links to
	// production users (or sending a bearer credential to an arbitrary origin).
	host := "vellum.kacpergajdarski.com"
	if c.Environment == "staging" {
		host = "staging.vellum.kacpergajdarski.com"
	}
	if u.Host != host {
		return fmt.Errorf("PASSWORD_RESET_FRONTEND_URL does not match APP_ENV")
	}
	for _, email := range c.AllowedRecipients {
		if !validMailbox(email) {
			return fmt.Errorf("PASSWORD_RESET_ALLOWED_RECIPIENTS contains an invalid address")
		}
	}
	if c.Environment == "staging" && len(c.AllowedRecipients) == 0 {
		return fmt.Errorf("staging SES delivery requires PASSWORD_RESET_ALLOWED_RECIPIENTS")
	}
	return nil
}

func validMailbox(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && !strings.ContainsAny(value, "\r\n")
}
