package services

import (
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/dto"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repositories"
)

type BudgetService struct {
	budgetRepository   repositories.BudgetRepository
	categoryRepository repositories.CategoryRepository
}

func NewBudgetService(budgetRepository repositories.BudgetRepository, categoryRepository repositories.CategoryRepository) *BudgetService {
	return &BudgetService{
		budgetRepository:   budgetRepository,
		categoryRepository: categoryRepository,
	}
}

func (s *BudgetService) FindByID(userID uuid.UUID, budgetID uuid.UUID) (*dto.BudgetResponse, error) {
	budget, err := s.budgetRepository.FindByID(budgetID)
	if err != nil {
		return nil, err
	}

	if budget == nil {
		return nil, ErrBudgetNotFound
	}

	if budget.UserID != userID {
		return nil, ErrBudgetNotFound
	}

	return &dto.BudgetResponse{
		ID:              budget.ID,
		CategoryID:      budget.CategoryID,
		AllocatedAmount: budget.AllocatedAmount,
	}, nil
}

func (s *BudgetService) FindByUserID(userID uuid.UUID) ([]*dto.BudgetResponse, error) {
	budgets, err := s.budgetRepository.FindByUserID(userID)
	if err != nil {
		return nil, err
	}

	responses := make([]*dto.BudgetResponse, 0, len(budgets))

	for _, budget := range budgets {
		responses = append(responses, &dto.BudgetResponse{
			ID:              budget.ID,
			CategoryID:      budget.CategoryID,
			AllocatedAmount: budget.AllocatedAmount,
		})
	}

	return responses, nil
}

func (s *BudgetService) Update(userID uuid.UUID, budgetID uuid.UUID, req dto.UpdateBudgetRequest) (*dto.BudgetResponse, error) {
	budget, err := s.budgetRepository.FindByID(budgetID)
	if err != nil {
		return nil, err
	}

	if budget == nil {
		return nil, ErrBudgetNotFound
	}

	if budget.UserID != userID {
		return nil, ErrBudgetNotFound
	}

	newCategory := req.CategoryID
	if newCategory == uuid.Nil {
		return nil, ErrCategoryRequired
	}

	category, err := s.categoryRepository.FindByID(newCategory)

	if err != nil {
		return nil, err
	}

	if category == nil {
		return nil, ErrCategoryNotFound
	}

	newAllocation := req.AllocatedAmount
	if newAllocation <= 0 {
		return nil, ErrAllocatedAmountPositive
	}

	budget.CategoryID = category.ID
	budget.AllocatedAmount = newAllocation

	err = s.budgetRepository.Update(budget)
	if err != nil {
		return nil, err
	}

	return &dto.BudgetResponse{
		ID:              budget.ID,
		CategoryID:      budget.CategoryID,
		AllocatedAmount: budget.AllocatedAmount,
	}, nil
}

func (s *BudgetService) Delete(userID uuid.UUID, budgetID uuid.UUID) error {
	budget, err := s.budgetRepository.FindByID(budgetID)
	if err != nil {
		return err
	}

	if budget == nil {
		return ErrBudgetNotFound
	}

	if budget.UserID != userID {
		return ErrBudgetNotFound
	}

	err = s.budgetRepository.Delete(budgetID)

	if err != nil {
		return err
	}

	return nil
}

func (s *BudgetService) Create(userID uuid.UUID, req dto.CreateBudgetRequest) (*dto.BudgetResponse, error) {
	categoryID := req.CategoryID

	if categoryID == uuid.Nil {
		return nil, ErrCategoryRequired
	}

	category, err := s.categoryRepository.FindByID(categoryID)

	if err != nil {
		return nil, err
	}

	if category == nil {
		return nil, ErrCategoryNotFound
	}

	allocatedAmount := req.AllocatedAmount

	if allocatedAmount <= 0 {
		return nil, ErrAllocatedAmountPositive
	}

	budget := &models.Budget{
		UserID:          userID,
		CategoryID:      category.ID,
		AllocatedAmount: allocatedAmount,
	}

	err = s.budgetRepository.Create(budget)
	if err != nil {
		return nil, err
	}

	return &dto.BudgetResponse{
		ID:              budget.ID,
		CategoryID:      budget.CategoryID,
		AllocatedAmount: budget.AllocatedAmount,
	}, nil
}
