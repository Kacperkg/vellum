package services

import (
	"errors"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/repository"
)

type CategoryService struct {
	categoryRepository repository.CategoryRepository
}

func NewCategoryService(categoryRepository repository.CategoryRepository) *CategoryService {
	return &CategoryService{
		categoryRepository: categoryRepository,
	}
}

func (s *CategoryService) FindByID(categoryID uuid.UUID) (*CategoryResponse, error) {
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

	return &CategoryResponse{
		ID:   category.ID,
		Name: category.Name,
	}, nil
}

func (s *CategoryService) ListAll() ([]*CategoryResponse, error) {
	categories, err := s.categoryRepository.ListAll()
	if err != nil {
		return nil, err
	}

	responses := make([]*CategoryResponse, 0, len(categories))

	for _, category := range categories {
		responses = append(responses, &CategoryResponse{
			ID:   category.ID,
			Name: category.Name,
		})
	}

	return responses, nil
}
