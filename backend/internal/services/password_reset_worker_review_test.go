package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kacperkg/vellum/internal/models"
)

type workerIssue struct{ email, hash string }
type workerReviewRepository struct{ issued chan workerIssue }

func (r *workerReviewRepository) Issue(_ context.Context, email, hash string, _, _ time.Duration) (*models.PasswordResetToken, *models.User, error) {
	r.issued <- workerIssue{email, hash}
	return &models.PasswordResetToken{}, &models.User{Email: email}, nil
}
func (*workerReviewRepository) Consume(context.Context, string, string) error { return nil }
func (*workerReviewRepository) Cleanup(context.Context, time.Time) error      { return nil }

type workerReviewSender struct{ sent chan ResetEmail }

func (s *workerReviewSender) SendPasswordReset(_ context.Context, email ResetEmail) error {
	s.sent <- email
	return nil
}

func TestPasswordResetQueuedWorkerDeliversWithoutPublicLookup(t *testing.T) {
	repo := &workerReviewRepository{issued: make(chan workerIssue, 1)}
	sender := &workerReviewSender{sent: make(chan ResetEmail, 1)}
	service, err := NewPasswordResetService(repo, sender, serviceResetConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	service.QueueRecovery("  exact.Case@example.com  ")
	select {
	case email := <-sender.sent:
		issue := <-repo.issued
		if email.To != "exact.Case@example.com" || issue.email != email.To {
			t.Fatal("worker altered case-sensitive recipient identity")
		}
		raw := strings.TrimPrefix(email.URL, service.cfg.FrontendURL+"#token=")
		if len(raw) != 43 || issue.hash != HashResetToken(raw) {
			t.Fatal("worker did not persist only the delivered token's hash")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bounded worker did not process queued recovery request")
	}
}
