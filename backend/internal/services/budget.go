package services

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repository"
)

type BudgetService struct {
	budgetRepository   repository.BudgetRepository
	categoryRepository repository.CategoryRepository
}

func NewBudgetService(budgetRepository repository.BudgetRepository, categoryRepository repository.CategoryRepository) *BudgetService {
	return &BudgetService{
		budgetRepository:   budgetRepository,
		categoryRepository: categoryRepository,
	}
}

func (s *BudgetService) FindByID(userID uuid.UUID, budgetID uuid.UUID) (*BudgetResponse, error) {
	budget, err := s.budgetRepository.FindByID(budgetID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrBudgetNotFound
		}
		return nil, err
	}

	if budget == nil {
		return nil, ErrBudgetNotFound
	}

	if budget.UserID != userID {
		return nil, ErrBudgetNotFound
	}

	return &BudgetResponse{
		ID:              budget.ID,
		CategoryID:      budget.CategoryID,
		AllocatedAmount: budget.AllocatedAmount,
	}, nil
}

func (s *BudgetService) FindByUserID(userID uuid.UUID) ([]*BudgetResponse, error) {
	budgets, err := s.budgetRepository.FindByUserID(userID)
	if err != nil {
		return nil, err
	}

	responses := make([]*BudgetResponse, 0, len(budgets))

	for _, budget := range budgets {
		responses = append(responses, &BudgetResponse{
			ID:              budget.ID,
			CategoryID:      budget.CategoryID,
			AllocatedAmount: budget.AllocatedAmount,
		})
	}

	return responses, nil
}

func (s *BudgetService) Update(userID uuid.UUID, budgetID uuid.UUID, req UpdateBudgetRequest) (*BudgetResponse, error) {
	budget, err := s.budgetRepository.FindByID(budgetID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrBudgetNotFound
		}
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
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrCategoryNotFound
		}
		return nil, err
	}

	if category == nil {
		return nil, ErrCategoryNotFound
	}

	newAllocation := req.AllocatedAmount
	if newAllocation <= 0 {
		return nil, ErrAllocatedAmountPositive
	}

	if err := s.ensureCategoryAvailable(userID, newCategory, budget.ID); err != nil {
		return nil, err
	}

	budget.CategoryID = category.ID
	budget.AllocatedAmount = newAllocation

	err = s.budgetRepository.Update(userID, budget)
	if err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, ErrBudgetCategoryExists
		}
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrBudgetNotFound
		}
		return nil, err
	}

	return &BudgetResponse{
		ID:              budget.ID,
		CategoryID:      budget.CategoryID,
		AllocatedAmount: budget.AllocatedAmount,
	}, nil
}

func (s *BudgetService) Delete(userID uuid.UUID, budgetID uuid.UUID) error {
	budget, err := s.budgetRepository.FindByID(budgetID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrBudgetNotFound
		}
		return err
	}

	if budget == nil {
		return ErrBudgetNotFound
	}

	if budget.UserID != userID {
		return ErrBudgetNotFound
	}

	err = s.budgetRepository.Delete(userID, budgetID)

	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrBudgetNotFound
		}
		return err
	}

	return nil
}

func (s *BudgetService) Create(userID uuid.UUID, req CreateBudgetRequest) (*BudgetResponse, error) {
	categoryID := req.CategoryID

	if categoryID == uuid.Nil {
		return nil, ErrCategoryRequired
	}

	category, err := s.categoryRepository.FindByID(categoryID)

	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrCategoryNotFound
		}
		return nil, err
	}

	if category == nil {
		return nil, ErrCategoryNotFound
	}

	allocatedAmount := req.AllocatedAmount

	if allocatedAmount <= 0 {
		return nil, ErrAllocatedAmountPositive
	}

	if err := s.ensureCategoryAvailable(userID, category.ID, uuid.Nil); err != nil {
		return nil, err
	}

	budget := &models.Budget{
		UserID:          userID,
		CategoryID:      category.ID,
		AllocatedAmount: allocatedAmount,
	}

	err = s.budgetRepository.Create(budget)
	if err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, ErrBudgetCategoryExists
		}
		return nil, err
	}

	return &BudgetResponse{
		ID:              budget.ID,
		CategoryID:      budget.CategoryID,
		AllocatedAmount: budget.AllocatedAmount,
	}, nil
}

func (s *BudgetService) ensureCategoryAvailable(userID, categoryID, excludedBudgetID uuid.UUID) error {
	budgets, err := s.budgetRepository.FindByUserID(userID)
	if err != nil {
		return err
	}

	for _, budget := range budgets {
		if budget.CategoryID == categoryID && budget.ID != excludedBudgetID {
			return ErrBudgetCategoryExists
		}
	}

	return nil
}

// WithContext returns a request-scoped copy; the shared service is never mutated.
func (s *BudgetService) WithContext(ctx context.Context) *BudgetService {
	clone := *s
	clone.budgetRepository = s.budgetRepository.WithContext(ctx)
	clone.categoryRepository = s.categoryRepository.WithContext(ctx)
	return &clone
}
