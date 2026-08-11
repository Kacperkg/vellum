package services

import (
	"time"

	"github.com/kacperkg/vellum/internal/auth"
	"github.com/kacperkg/vellum/internal/dto"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repositories"
	"golang.org/x/crypto/bcrypt"
)

type UserService struct {
	userRepository         repositories.UserRepository
	userSettingsRepository repositories.UserSettingsRepository

	jwtSecret string
	jwtExpiry time.Duration
}

func NewUserService(userRepository repositories.UserRepository, userSettingsRepository repositories.UserSettingsRepository, jwtSecret string, jwtExpiry time.Duration) *UserService {
	return &UserService{
		userRepository:         userRepository,
		userSettingsRepository: userSettingsRepository,
		jwtSecret:              jwtSecret,
		jwtExpiry:              jwtExpiry,
	}
}

func (s *UserService) Register(req dto.RegisterRequest) (*dto.AuthResponse, error) {
	// Check if email already exists
	user, err := s.userRepository.FindByEmail(req.Email)
	if err != nil {
		return nil, err
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

	// Generate token
	token, err := auth.GenerateToken(user.ID, s.jwtSecret, s.jwtExpiry)
	if err != nil {
		return nil, err
	}

	return &dto.AuthResponse{
		AccessToken: token,
	}, nil
}

func (s *UserService) Login(req dto.LoginRequest) (*dto.AuthResponse, error) {
	// Check if user exists
	user, err := s.userRepository.FindByEmail(req.Email)
	if err != nil {
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

	// Generate token
	token, err := auth.GenerateToken(user.ID, s.jwtSecret, s.jwtExpiry)
	if err != nil {
		return nil, err
	}

	return &dto.AuthResponse{
		AccessToken: token,
	}, nil
}
