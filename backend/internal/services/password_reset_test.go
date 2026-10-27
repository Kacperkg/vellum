package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/kacperkg/vellum/internal/config"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

type resetRepoFake struct {
	issueErr, errorConsume   error
	email, hash, password    string
	issueCalls, consumeCalls int
}

func (r *resetRepoFake) Issue(_ context.Context, email, hash string, _, _ time.Duration) (*models.PasswordResetToken, *models.User, error) {
	r.issueCalls++
	r.email = email
	r.hash = hash
	if r.issueErr != nil {
		return nil, nil, r.issueErr
	}
	return &models.PasswordResetToken{}, &models.User{Email: email}, nil
}
func (r *resetRepoFake) Consume(_ context.Context, hash, password string) error {
	r.consumeCalls++
	r.hash = hash
	r.password = password
	return r.errorConsume
}
func (r *resetRepoFake) Cleanup(context.Context, time.Time) error { return nil }

type resetSenderFake struct {
	sent    []ResetEmail
	err     error
	timeout bool
}

func (s *resetSenderFake) SendPasswordReset(ctx context.Context, email ResetEmail) error {
	s.sent = append(s.sent, email)
	if s.timeout {
		<-ctx.Done()
		return ctx.Err()
	}
	return s.err
}
func serviceResetConfig() config.PasswordReset {
	return config.PasswordReset{Mode: "ses", Environment: "production", Region: "eu-west-2", FromAddress: "no-reply@kacpergajdarski.com", FrontendURL: "https://vellum.kacpergajdarski.com/reset-password", TokenTTL: 20 * time.Minute, Cooldown: time.Minute, QueueSize: 2, SendTimeout: 20 * time.Millisecond}
}
func TestPasswordResetDeliveryAndRedaction(t *testing.T) {
	for _, test := range []struct {
		name              string
		issueErr, sendErr error
		timeout           bool
		sent              int
	}{
		{"existing", nil, nil, false, 1}, {"unknown", repository.ErrNotFound, nil, false, 0}, {"database failure", errors.New("sensitive-email"), nil, false, 0}, {"SES failure", nil, errors.New("sensitive-provider-message"), false, 1}, {"SES timeout", nil, nil, true, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &resetRepoFake{issueErr: test.issueErr}
			sender := &resetSenderFake{err: test.sendErr, timeout: test.timeout}
			s := &PasswordResetService{repo: repo, sender: sender, cfg: serviceResetConfig()}
			var logs bytes.Buffer
			old := log.Writer()
			log.SetOutput(&logs)
			defer log.SetOutput(old)
			start := time.Now()
			s.deliver(context.Background(), "exact.Case@example.com")
			if time.Since(start) > time.Second {
				t.Fatal("delivery timeout not bounded")
			}
			if len(sender.sent) != test.sent {
				t.Fatalf("sent=%d want%d", len(sender.sent), test.sent)
			}
			if strings.Contains(logs.String(), "sensitive") || strings.Contains(logs.String(), "example.com") {
				t.Fatal("sensitive error or recipient logged")
			}
			if test.sent == 1 {
				mail := sender.sent[0]
				raw := strings.TrimPrefix(mail.URL, s.cfg.FrontendURL+"#token=")
				material, err := base64.RawURLEncoding.DecodeString(raw)
				if err != nil || len(material) != 32 || repo.hash != HashResetToken(raw) || strings.Contains(logs.String(), raw) {
					t.Fatal("reset token security failed")
				}
				if mail.ExpiresIn != 20*time.Minute {
					t.Fatal("expiry not sent")
				}
			}
		})
	}
}
func TestPasswordResetStagingRestrictionAndQueueBound(t *testing.T) {
	repo := &resetRepoFake{}
	sender := &resetSenderFake{}
	cfg := serviceResetConfig()
	cfg.AllowedRecipients = []string{"allowed@example.com"}
	s := &PasswordResetService{repo: repo, sender: sender, cfg: cfg, queue: make(chan string, 2)}
	s.deliver(context.Background(), "blocked@example.com")
	if repo.issueCalls != 0 || len(sender.sent) != 0 {
		t.Fatal("restriction bypassed")
	}
	for range 1000 {
		s.QueueRecovery("allowed@example.com")
	}
	if len(s.queue) != 2 || repo.issueCalls != 0 {
		t.Fatal("queue unbounded or public path looked up account")
	}
	s.cfg.Mode = "disabled"
	s.QueueRecovery("third@example.com")
	if len(s.queue) != 2 {
		t.Fatal("disabled mode queued")
	}
}
func TestPasswordResetServiceValidationAndHashing(t *testing.T) {
	raw := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	for _, test := range []struct {
		name, token, password string
		repoErr, want         error
		calls                 int
	}{
		{"valid", raw, "correct-new-password", nil, nil, 1},
		{"invalid format", "bad", "correct-new-password", nil, ErrInvalidResetToken, 0},
		{"padding", raw + "=", "correct-new-password", nil, ErrInvalidResetToken, 0},
		{"short", raw, "short", nil, ErrInvalidInput, 0},
		{"bcrypt bytes", raw, strings.Repeat("界", 25), nil, ErrInvalidInput, 0},
		{"expired or consumed", raw, "correct-new-password", repository.ErrNotFound, ErrInvalidResetToken, 1},
		{"rollback error", raw, "correct-new-password", errors.New("database failed"), nil, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &resetRepoFake{errorConsume: test.repoErr}
			s := &PasswordResetService{repo: repo}
			err := s.Reset(context.Background(), test.token, test.password)
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("error=%v want%v", err, test.want)
			}
			if test.want == nil && test.repoErr == nil && err != nil {
				t.Fatal(err)
			}
			if repo.consumeCalls != test.calls {
				t.Fatalf("consume=%d", repo.consumeCalls)
			}
			if test.calls > 0 {
				if repo.hash != HashResetToken(raw) || repo.password == test.password || bcrypt.CompareHashAndPassword([]byte(repo.password), []byte(test.password)) != nil {
					t.Fatal("token/password not hashed")
				}
			}
		})
	}
}
func TestPasswordResetWorkerLifecycle(t *testing.T) {
	cfg := serviceResetConfig()
	cfg.Mode = "disabled"
	s, err := NewPasswordResetService(&resetRepoFake{}, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s.Close()
	cfg.Mode = "ses"
	if _, err := NewPasswordResetService(&resetRepoFake{}, nil, cfg); err == nil {
		t.Fatal("missing sender accepted")
	}
}
