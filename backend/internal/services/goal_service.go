package services

import (
	"strings"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/dto"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repositories"
)

type GoalService struct {
	goalRepository repositories.GoalRepository
}

func NewGoalService(goalRepository repositories.GoalRepository) *GoalService {
	return &GoalService{
		goalRepository: goalRepository,
	}
}

func (s *GoalService) Create(userID uuid.UUID, req dto.CreateGoalRequest) (*dto.GoalResponse, error) {
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

	goal := &models.Goal{
		UserID: userID,

		Name:                name,
		TargetAmount:        targetAmount,
		SavedAmount:         savedAmount,
		MonthlyContribution: monthlyContribution,
	}

	err := s.goalRepository.Create(goal)
	if err != nil {
		return nil, err
	}

	return &dto.GoalResponse{
		ID:                  goal.ID,
		Name:                goal.Name,
		TargetAmount:        goal.TargetAmount,
		SavedAmount:         goal.SavedAmount,
		MonthlyContribution: goal.MonthlyContribution,
	}, nil
}

func (s *GoalService) GetByID(userID uuid.UUID, goalID uuid.UUID) (*dto.GoalResponse, error) {
	goal, err := s.goalRepository.FindByID(goalID)
	if err != nil {
		return nil, err
	}

	if goal == nil {
		return nil, ErrGoalNotFound
	}

	if goal.UserID != userID {
		return nil, ErrGoalNotFound
	}

	return &dto.GoalResponse{
		ID:                  goal.ID,
		Name:                goal.Name,
		TargetAmount:        goal.TargetAmount,
		SavedAmount:         goal.SavedAmount,
		MonthlyContribution: goal.MonthlyContribution,
	}, nil
}

func (s *GoalService) FindByUserID(userID uuid.UUID) ([]*dto.GoalResponse, error) {
	goals, err := s.goalRepository.FindByUserID(userID)
	if err != nil {
		return nil, err
	}

	responses := make([]*dto.GoalResponse, 0, len(goals))

	for _, goal := range goals {
		responses = append(responses, &dto.GoalResponse{
			ID:                  goal.ID,
			Name:                goal.Name,
			TargetAmount:        goal.TargetAmount,
			SavedAmount:         goal.SavedAmount,
			MonthlyContribution: goal.MonthlyContribution,
		})
	}

	return responses, nil
}

func (s *GoalService) Update(userID uuid.UUID, goalID uuid.UUID, req dto.UpdateGoalRequest) (*dto.GoalResponse, error) {
	goal, err := s.goalRepository.FindByID(goalID)
	if err != nil {
		return nil, err
	}

	if goal == nil {
		return nil, ErrGoalNotFound
	}

	if goal.UserID != userID {
		return nil, ErrGoalNotFound
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

	goal.Name = newName
	goal.TargetAmount = newTargetAmount
	goal.SavedAmount = newSavedAmount
	goal.MonthlyContribution = newMonthlyContribution

	err = s.goalRepository.Update(goal)
	if err != nil {
		return nil, err
	}

	return &dto.GoalResponse{
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
		return err
	}

	if goal == nil {
		return ErrGoalNotFound
	}

	if goal.UserID != userID {
		return ErrGoalNotFound
	}

	err = s.goalRepository.Delete(goalID)

	if err != nil {
		return err
	}

	return nil
}
