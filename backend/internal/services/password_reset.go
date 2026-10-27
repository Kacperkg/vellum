package services

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/kacperkg/vellum/internal/config"
	"github.com/kacperkg/vellum/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

const RecoveryMessage = "If an account exists with this email address, password reset instructions will be sent."

var ErrInvalidResetToken = errors.New("invalid or expired password reset link")

type ResetEmail struct {
	To        string
	URL       string
	ExpiresIn time.Duration
}
type PasswordResetSender interface {
	SendPasswordReset(context.Context, ResetEmail) error
}

type PasswordResetService struct {
	repo   repository.PasswordResetRepository
	sender PasswordResetSender
	cfg    config.PasswordReset
	queue  chan string
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

// NewPasswordResetService starts exactly one bounded worker. The queue holds
// email addresses, never bearer secrets, and is intentionally non-durable.
func NewPasswordResetService(repo repository.PasswordResetRepository, sender PasswordResetSender, cfg config.PasswordReset) (*PasswordResetService, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Mode == "ses" && sender == nil {
		return nil, errors.New("SES delivery requires a sender")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &PasswordResetService{repo: repo, sender: sender, cfg: cfg, queue: make(chan string, cfg.QueueSize), cancel: cancel, done: make(chan struct{})}
	go s.run(ctx)
	return s, nil
}
func (s *PasswordResetService) Close() { s.once.Do(s.cancel); <-s.done }

// QueueRecovery does no account lookup; delivery state, unknown recipients and
// queue saturation produce the same public response and comparable timing.
func (s *PasswordResetService) QueueRecovery(email string) {
	if s.cfg.Mode != "ses" {
		return
	}
	email = strings.TrimSpace(email)
	select {
	case s.queue <- email:
	default:
	}
}

func (s *PasswordResetService) run(ctx context.Context) {
	defer close(s.done)
	// A fixed single-worker cadence bounds aggregate volume to 60 attempts/min
	// per process. The database enforces the per-account cooldown across replicas.
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	cleanup := time.NewTicker(15 * time.Minute)
	defer cleanup.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-cleanup.C:
			workCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := s.repo.Cleanup(workCtx, time.Now().Add(-24*time.Hour)); err != nil {
				log.Print("password recovery cleanup failed")
			}
			cancel()
		case <-tick.C:
			select {
			case email := <-s.queue:
				s.deliver(ctx, email)
			default:
			}
		}
	}
}
func (s *PasswordResetService) deliver(ctx context.Context, email string) {
	if len(s.cfg.AllowedRecipients) > 0 {
		allowed := false
		for _, recipient := range s.cfg.AllowedRecipients {
			if recipient == email {
				allowed = true
				break
			}
		}
		if !allowed {
			return
		}
	}
	raw, err := generateRefreshToken()
	if err != nil {
		log.Print("password recovery token generation failed")
		return
	}
	workCtx, cancel := context.WithTimeout(ctx, s.cfg.SendTimeout)
	defer cancel()
	_, user, err := s.repo.Issue(workCtx, email, HashResetToken(raw), s.cfg.TokenTTL, s.cfg.Cooldown)
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			log.Print("password recovery persistence failed")
		}
		return
	}
	// The transaction has committed before any SES network call.
	if err := s.sender.SendPasswordReset(workCtx, ResetEmail{To: user.Email, URL: s.cfg.FrontendURL + "#token=" + raw, ExpiresIn: s.cfg.TokenTTL}); err != nil {
		// Never log SDK errors: they can include recipient addresses or request data.
		log.Print("password recovery delivery failed")
	}
}

func (s *PasswordResetService) Reset(ctx context.Context, raw, password string) error {
	bytes, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(bytes) != 32 || base64.RawURLEncoding.EncodeToString(bytes) != raw {
		return ErrInvalidResetToken
	}
	if utf8.RuneCountInString(password) < 8 || len(password) > 72 {
		return fmt.Errorf("%w: password must contain at least 8 characters and at most 72 UTF-8 bytes", ErrInvalidInput)
	}
	// bcrypt work happens outside the transaction and never holds row locks.
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.repo.Consume(ctx, HashResetToken(raw), string(hash)); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrInvalidResetToken
		}
		return err
	}
	return nil
}
func HashResetToken(raw string) string {
	hash := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(hash[:])
}
