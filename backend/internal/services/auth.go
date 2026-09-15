package services

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/auth"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

type UserService struct {
	userRepository         repository.UserRepository
	userSettingsRepository repository.UserSettingsRepository
	refreshTokenRepository repository.RefreshTokenRepository

	jwtSecret     string
	jwtExpiry     time.Duration
	refreshExpiry time.Duration
}

func NewUserService(
	userRepository repository.UserRepository,
	userSettingsRepository repository.UserSettingsRepository,
	refreshTokenRepository repository.RefreshTokenRepository,
	jwtSecret string,
	jwtExpiry time.Duration,
	refreshExpiry time.Duration,
) *UserService {
	return &UserService{
		userRepository:         userRepository,
		userSettingsRepository: userSettingsRepository,
		refreshTokenRepository: refreshTokenRepository,
		jwtSecret:              jwtSecret,
		jwtExpiry:              jwtExpiry,
		refreshExpiry:          refreshExpiry,
	}
}

func (s *UserService) Register(req RegisterRequest) (*AuthResponse, error) {
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
		FirstName:    req.FirstName,
		LastName:     req.LastName,
		Email:        req.Email,
		PasswordHash: string(hashedPassword),
	}

	err = s.userRepository.Create(user)
	if err != nil {
		return nil, err
	}

	// Create default user settings
	defaultSettings := &models.UserSettings{
		UserID:       user.ID,
		Currency:     models.GBP,
		DateFormat:   models.DDMMYYYY,
		NumberFormat: models.CommaDecimal,
		WeekStartsOn: models.Monday,
	}

	err = s.userSettingsRepository.Create(defaultSettings)
	if err != nil {
		return nil, err
	}

	return s.issueTokenPair(user.ID, s.refreshTokenRepository.Create)
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

	return s.issueTokenPair(user.ID, s.refreshTokenRepository.Create)
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

	return s.issueTokenPair(user.ID, func(replacement *models.RefreshToken) error {
		return s.refreshTokenRepository.Rotate(current.ID, replacement, now)
	})
}

func (s *UserService) Logout(req RefreshRequest) error {
	return s.refreshTokenRepository.RevokeByHash(hashRefreshToken(req.RefreshToken), time.Now())
}

func (s *UserService) issueTokenPair(
	userID uuid.UUID,
	persist func(*models.RefreshToken) error,
) (*AuthResponse, error) {
	accessToken, err := auth.GenerateToken(userID, s.jwtSecret, s.jwtExpiry)
	if err != nil {
		return nil, fmt.Errorf("generate access token: %w", err)
	}

	rawRefreshToken, err := generateRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}

	refreshToken := &models.RefreshToken{
		UserID:    userID,
		TokenHash: hashRefreshToken(rawRefreshToken),
		ExpiresAt: time.Now().Add(s.refreshExpiry),
	}
	if err := persist(refreshToken); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrInvalidRefreshToken
		}
		return nil, fmt.Errorf("persist refresh token: %w", err)
	}

	return &AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: rawRefreshToken,
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
