package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/auth"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

type UserService struct {
	userRepository         repository.UserRepository
	refreshTokenRepository repository.RefreshTokenRepository

	jwtSecret     string
	jwtExpiry     time.Duration
	refreshExpiry time.Duration
}

func NewUserService(
	userRepository repository.UserRepository,
	refreshTokenRepository repository.RefreshTokenRepository,
	jwtSecret string,
	jwtExpiry time.Duration,
	refreshExpiry time.Duration,
) *UserService {
	return &UserService{
		userRepository:         userRepository,
		refreshTokenRepository: refreshTokenRepository,
		jwtSecret:              jwtSecret,
		jwtExpiry:              jwtExpiry,
		refreshExpiry:          refreshExpiry,
	}
}

func (s *UserService) Register(req RegisterRequest) (*AuthResponse, error) {
	if utf8.RuneCountInString(req.Password) < 8 || len(req.Password) > 72 {
		return nil, invalid("password must contain at least 8 characters and at most 72 UTF-8 bytes")
	}
	// Check if email already exists
	user, err := s.userRepository.FindByEmail(req.Email)
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			return nil, err
		}
		user = nil
	}

	if user != nil {
		return nil, ErrUserEmailExists
	}

	// Hash the password
	hashedPassword, err := bcrypt.GenerateFromPassword(
		[]byte(req.Password),
		bcrypt.DefaultCost,
	)

	if err != nil {
		return nil, err
	}

	user = &models.User{
		BaseModel:    models.BaseModel{ID: uuid.New()},
		FirstName:    req.FirstName,
		LastName:     req.LastName,
		Email:        req.Email,
		PasswordHash: string(hashedPassword),
	}

	// Create default user settings
	defaultSettings := &models.UserSettings{
		UserID:       user.ID,
		Currency:     models.GBP,
		DateFormat:   models.DDMMYYYY,
		NumberFormat: models.CommaDecimal,
		WeekStartsOn: models.Monday,
	}

	response, err := s.issueTokenPair(user, uuid.New(), func(token *models.RefreshToken) error {
		return s.userRepository.CreateRegistration(user, defaultSettings, token)
	})
	if errors.Is(err, repository.ErrConflict) {
		return nil, ErrUserEmailExists
	}
	return response, err
}

func (s *UserService) Login(req LoginRequest) (*AuthResponse, error) {
	// Check if user exists
	user, err := s.userRepository.FindByEmail(req.Email)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	if user == nil {
		return nil, ErrInvalidCredentials
	}

	// Check if password is correct
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password))
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	return s.issueTokenPair(user, uuid.New(), s.refreshTokenRepository.Create)
}

func (s *UserService) Refresh(req RefreshRequest) (*AuthResponse, error) {
	now := time.Now()
	current, err := s.refreshTokenRepository.FindValid(hashRefreshToken(req.RefreshToken), now)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrInvalidRefreshToken
		}
		return nil, err
	}

	user, err := s.userRepository.FindByID(current.UserID)
	if err != nil || user == nil {
		return nil, ErrInvalidRefreshToken
	}

	return s.issueTokenPair(user, current.SessionFamilyID(), func(replacement *models.RefreshToken) error {
		return s.refreshTokenRepository.Rotate(current.ID, replacement, now)
	})
}

func (s *UserService) Logout(req RefreshRequest) error {
	return s.refreshTokenRepository.RevokeByHash(hashRefreshToken(req.RefreshToken), time.Now())
}

func (s *UserService) issueTokenPair(
	user *models.User,
	familyID uuid.UUID,
	persist func(*models.RefreshToken) error,
) (*AuthResponse, error) {
	accessToken, err := auth.GenerateSessionToken(user.ID, s.jwtSecret, s.jwtExpiry, user.AuthVersion, familyID)
	if err != nil {
		return nil, fmt.Errorf("generate access token: %w", err)
	}

	rawRefreshToken, err := generateRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}

	refreshToken := &models.RefreshToken{
		UserID:               user.ID,
		AuthVersion:          user.AuthVersion,
		FamilyID:             &familyID,
		PasswordHashSnapshot: user.PasswordHash,
		TokenHash:            hashRefreshToken(rawRefreshToken),
		ExpiresAt:            time.Now().Add(s.refreshExpiry),
	}
	if err := persist(refreshToken); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrInvalidRefreshToken
		}
		return nil, fmt.Errorf("persist session: %w", err)
	}

	return &AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: rawRefreshToken,
		User: UserResponse{
			ID:        user.ID,
			CreatedAt: user.CreatedAt,
			UpdatedAt: user.UpdatedAt,
			FirstName: user.FirstName,
			LastName:  user.LastName,
			Email:     user.Email,
		},
	}, nil
}

func generateRefreshToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func hashRefreshToken(rawToken string) string {
	hash := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(hash[:])
}

// ValidateAuthVersion preserves legacy JWTs (missing version means zero) only
// until the first reset. Database errors fail closed.
func (s *UserService) ValidateAuthVersion(ctx context.Context, userID uuid.UUID, version int64) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	user, err := s.userRepository.FindByIDContext(ctx, userID)
	return err == nil && user != nil && version >= 0 && user.AuthVersion == version
}

// ValidateSession preserves pre-upgrade JWTs until their existing expiry (or a
// password reset), while new JWTs also require their own live refresh family.
func (s *UserService) ValidateSession(ctx context.Context, userID uuid.UUID, version int64, familyID *uuid.UUID) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if !s.ValidateAuthVersion(ctx, userID, version) {
		return false
	}
	if familyID == nil {
		return true
	}
	if *familyID == uuid.Nil {
		return false
	}
	active, err := s.refreshTokenRepository.HasActiveFamily(ctx, userID, version, *familyID, time.Now())
	return err == nil && active
}
