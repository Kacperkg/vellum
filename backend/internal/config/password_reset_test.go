package config

import (
	"testing"
	"time"
)

func resetConfig() PasswordReset {
	return PasswordReset{Mode: "ses", Environment: "staging", Region: "eu-west-2", FromAddress: "no-reply-staging@kacpergajdarski.com", FrontendURL: "https://staging.vellum.kacpergajdarski.com/reset-password", TokenTTL: 20 * time.Minute, Cooldown: time.Minute, QueueSize: 32, SendTimeout: 5 * time.Second, AllowedRecipients: []string{"test@example.com"}}
}
func TestPasswordResetConfig(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*PasswordReset)
		valid  bool
	}{
		{"staging", func(*PasswordReset) {}, true},
		{"production", func(c *PasswordReset) {
			c.Environment = "production"
			c.FrontendURL = "https://vellum.kacpergajdarski.com/reset-password"
			c.FromAddress = "no-reply@kacpergajdarski.com"
			c.AllowedRecipients = nil
		}, true},
		{"disabled", func(c *PasswordReset) { c.Mode = "disabled"; c.FrontendURL = ""; c.FromAddress = "" }, true},
		{"unknown mode", func(c *PasswordReset) { c.Mode = "log" }, false},
		{"wrong region", func(c *PasswordReset) { c.Region = "us-east-1" }, false},
		{"development sending", func(c *PasswordReset) { c.Environment = "development" }, false},
		{"sender newline", func(c *PasswordReset) { c.FromAddress = "test@example.com\nX: evil" }, false},
		{"production sender in staging", func(c *PasswordReset) { c.FromAddress = "no-reply@kacpergajdarski.com" }, false},
		{"http", func(c *PasswordReset) { c.FrontendURL = "http://staging.vellum.kacpergajdarski.com/reset-password" }, false},
		{"wrong host", func(c *PasswordReset) { c.FrontendURL = "https://evil.example/reset-password" }, false},
		{"production host in staging", func(c *PasswordReset) { c.FrontendURL = "https://vellum.kacpergajdarski.com/reset-password" }, false},
		{"userinfo", func(c *PasswordReset) {
			c.FrontendURL = "https://user@staging.vellum.kacpergajdarski.com/reset-password"
		}, false},
		{"query", func(c *PasswordReset) { c.FrontendURL += "?token=value" }, false},
		{"fragment", func(c *PasswordReset) { c.FrontendURL += "#token=value" }, false},
		{"no staging recipients", func(c *PasswordReset) { c.AllowedRecipients = nil }, false},
		{"invalid recipient", func(c *PasswordReset) { c.AllowedRecipients = []string{"not-email"} }, false},
		{"long ttl", func(c *PasswordReset) { c.TokenTTL = 2 * time.Hour }, false},
		{"short cooldown", func(c *PasswordReset) { c.Cooldown = time.Second }, false},
		{"oversize queue", func(c *PasswordReset) { c.QueueSize = 257 }, false},
		{"unbounded timeout", func(c *PasswordReset) { c.SendTimeout = 0 }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := resetConfig()
			test.change(&cfg)
			err := cfg.Validate()
			if (err == nil) != test.valid {
				t.Fatalf("validation=%v valid=%t", err, test.valid)
			}
		})
	}
}
func TestPasswordResetDefaults(t *testing.T) {
	for _, key := range []string{"PASSWORD_RESET_EMAIL_MODE", "APP_ENV", "AWS_REGION", "SES_FROM_ADDRESS", "PASSWORD_RESET_FRONTEND_URL", "PASSWORD_RESET_TOKEN_TTL", "PASSWORD_RESET_COOLDOWN", "PASSWORD_RESET_QUEUE_SIZE", "PASSWORD_RESET_SEND_TIMEOUT", "PASSWORD_RESET_ALLOWED_RECIPIENTS"} {
		t.Setenv(key, "")
	}
	cfg, err := loadPasswordReset()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != "disabled" || cfg.TokenTTL != 20*time.Minute || cfg.Region != "eu-west-2" || cfg.QueueSize != 32 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}
