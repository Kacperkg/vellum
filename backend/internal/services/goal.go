package services

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repository"
)

type GoalService struct {
	goalRepository    repository.GoalRepository
	accountRepository repository.AccountRepository
}

func NewGoalService(goalRepository repository.GoalRepository, accountRepository repository.AccountRepository) *GoalService {
	return &GoalService{
		goalRepository:    goalRepository,
		accountRepository: accountRepository,
	}
}

func (s *GoalService) Create(userID uuid.UUID, req CreateGoalRequest) (*GoalResponse, error) {
	if err := validateResourceName(req.Name); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, ErrGoalNameRequired
	}

	targetAmount := req.TargetAmount
	if targetAmount <= 0 {
		return nil, ErrGoalTargetPositive
	}

	savedAmount := req.SavedAmount
	if savedAmount < 0 {
		return nil, ErrGoalSavedAmountNegative
	}
	if savedAmount > targetAmount {
		return nil, ErrGoalSavedExceedsTarget
	}

	monthlyContribution := req.MonthlyContribution
	if monthlyContribution < 0 {
		return nil, ErrGoalContributionNegative
	}
	if err := s.validateAccount(userID, req.AccountID); err != nil {
		return nil, err
	}

	goal := &models.Goal{
		UserID:    userID,
		AccountID: req.AccountID,

		Name:                name,
		TargetAmount:        targetAmount,
		SavedAmount:         savedAmount,
		MonthlyContribution: monthlyContribution,
	}

	err := s.goalRepository.Create(goal)
	if err != nil {
		return nil, err
	}

	return &GoalResponse{
		AccountID:           goal.AccountID,
		ID:                  goal.ID,
		Name:                goal.Name,
		TargetAmount:        goal.TargetAmount,
		SavedAmount:         goal.SavedAmount,
		MonthlyContribution: goal.MonthlyContribution,
	}, nil
}

func (s *GoalService) GetByID(userID uuid.UUID, goalID uuid.UUID) (*GoalResponse, error) {
	goal, err := s.goalRepository.FindByID(goalID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrGoalNotFound
		}
		return nil, err
	}

	if goal == nil {
		return nil, ErrGoalNotFound
	}

	if goal.UserID != userID {
		return nil, ErrGoalNotFound
	}

	return &GoalResponse{
		AccountID:           goal.AccountID,
		ID:                  goal.ID,
		Name:                goal.Name,
		TargetAmount:        goal.TargetAmount,
		SavedAmount:         goal.SavedAmount,
		MonthlyContribution: goal.MonthlyContribution,
	}, nil
}

func (s *GoalService) FindByUserID(userID uuid.UUID) ([]*GoalResponse, error) {
	goals, err := s.goalRepository.FindByUserID(userID)
	if err != nil {
		return nil, err
	}

	responses := make([]*GoalResponse, 0, len(goals))

	for _, goal := range goals {
		responses = append(responses, &GoalResponse{
			AccountID:           goal.AccountID,
			ID:                  goal.ID,
			Name:                goal.Name,
			TargetAmount:        goal.TargetAmount,
			SavedAmount:         goal.SavedAmount,
			MonthlyContribution: goal.MonthlyContribution,
		})
	}

	return responses, nil
}

func (s *GoalService) Update(userID uuid.UUID, goalID uuid.UUID, req UpdateGoalRequest) (*GoalResponse, error) {
	goal, err := s.goalRepository.FindByID(goalID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrGoalNotFound
		}
		return nil, err
	}

	if goal == nil {
		return nil, ErrGoalNotFound
	}

	if goal.UserID != userID {
		return nil, ErrGoalNotFound
	}

	if err := validateResourceName(req.Name); err != nil {
		return nil, err
	}
	newName := strings.TrimSpace(req.Name)
	if newName == "" {
		return nil, ErrGoalNameRequired
	}

	newTargetAmount := req.TargetAmount
	if newTargetAmount <= 0 {
		return nil, ErrGoalTargetPositive
	}

	newSavedAmount := req.SavedAmount
	if newSavedAmount < 0 {
		return nil, ErrGoalSavedAmountNegative
	}
	if newSavedAmount > newTargetAmount {
		return nil, ErrGoalSavedExceedsTarget
	}

	newMonthlyContribution := req.MonthlyContribution
	if newMonthlyContribution < 0 {
		return nil, ErrGoalContributionNegative
	}
	if err := s.validateAccount(userID, req.AccountID); err != nil {
		return nil, err
	}

	goal.AccountID = req.AccountID
	goal.Name = newName
	goal.TargetAmount = newTargetAmount
	goal.SavedAmount = newSavedAmount
	goal.MonthlyContribution = newMonthlyContribution

	err = s.goalRepository.Update(userID, goal)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrGoalNotFound
		}
		return nil, err
	}

	return &GoalResponse{
		AccountID:           goal.AccountID,
		ID:                  goal.ID,
		Name:                goal.Name,
		TargetAmount:        goal.TargetAmount,
		SavedAmount:         goal.SavedAmount,
		MonthlyContribution: goal.MonthlyContribution,
	}, nil
}

func (s *GoalService) Delete(userID uuid.UUID, goalID uuid.UUID) error {
	goal, err := s.goalRepository.FindByID(goalID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrGoalNotFound
		}
		return err
	}

	if goal == nil {
		return ErrGoalNotFound
	}

	if goal.UserID != userID {
		return ErrGoalNotFound
	}

	err = s.goalRepository.Delete(userID, goalID)

	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrGoalNotFound
		}
		return err
	}

	return nil
}

func (s *GoalService) validateAccount(userID uuid.UUID, accountID *uuid.UUID) error {
	if accountID == nil {
		return nil
	}
	if *accountID == uuid.Nil {
		return ErrAccountNotFound
	}
	account, err := s.accountRepository.GetByID(*accountID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrAccountNotFound
		}
		return err
	}
	if account == nil || account.UserID != userID {
		return ErrAccountNotFound
	}
	return nil
}

// WithContext returns a request-scoped copy; the shared service is never mutated.
func (s *GoalService) WithContext(ctx context.Context) *GoalService {
	clone := *s
	clone.goalRepository = s.goalRepository.WithContext(ctx)
	clone.accountRepository = s.accountRepository.WithContext(ctx)
	return &clone
}
