package services

import (
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/dto"
	"github.com/kacperkg/vellum/internal/repositories"
)

type CategoryService struct {
	categoryRepository repositories.CategoryRepository
}

func NewCategoryService(categoryRepository repositories.CategoryRepository) *CategoryService {
	return &CategoryService{
		categoryRepository: categoryRepository,
	}
}

func (s *CategoryService) FindByID(categoryID uuid.UUID) (*dto.CategoryResponse, error) {
	category, err := s.categoryRepository.FindByID(categoryID)
	if err != nil {
		return nil, err
	}

	if category == nil {
		return nil, ErrCategoryNotFound
	}

	return &dto.CategoryResponse{
		ID:   category.ID,
		Name: category.Name,
	}, nil
}

func (s *CategoryService) ListAll() ([]*dto.CategoryResponse, error) {
	categories, err := s.categoryRepository.ListAll()
	if err != nil {
		return nil, err
	}

	responses := make([]*dto.CategoryResponse, 0, len(categories))

	for _, category := range categories {
		responses = append(responses, &dto.CategoryResponse{
			ID:   category.ID,
			Name: category.Name,
		})
	}

	return responses, nil
}
