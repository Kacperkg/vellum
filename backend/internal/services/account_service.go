package services

import (
	"strings"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/dto"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repositories"
)

type AccountService struct {
	accountRepository repositories.AccountRepository
}

func NewAccountService(accountRepository repositories.AccountRepository) *AccountService {
	return &AccountService{
		accountRepository: accountRepository,
	}
}

func (s *AccountService) Create(userID uuid.UUID, req dto.CreateAccountRequest) (*dto.AccountResponse, error) {
	// Validate the request
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, ErrAccountNameRequired
	}

	// Create the model with information provided
	account := &models.Account{
		UserID: userID,
		Name:   name,
	}

	// Save the account to the repository
	err := s.accountRepository.Create(account)
	if err != nil {
		return nil, err
	}

	// return to frontend the newly created account information
	return &dto.AccountResponse{
		ID:   account.ID,
		Name: account.Name,
	}, nil
}

func (s *AccountService) GetByID(userID uuid.UUID, accountID uuid.UUID) (*dto.AccountResponse, error) {
	// Fetch the account by ID

	// Think of it as a try catch
	// s.accountRepository.GetByID(accountID) -> try block
	// if err != nil -> catch block
	account, err := s.accountRepository.GetByID(accountID)
	if err != nil {
		return nil, err
	}

	// Check if the account exists
	if account == nil {
		return nil, ErrAccountNotFound
	}

	// Check if the account belongs to the user - dont reveal account ownership to unauthorized users
	if account.UserID != userID {
		return nil, ErrAccountNotFound
	}

	return &dto.AccountResponse{
		ID:   account.ID,
		Name: account.Name,
	}, nil
}

func (s *AccountService) FindByUserID(userID uuid.UUID) ([]*dto.AccountResponse, error) {
	// Fetch the accounts by user ID
	accounts, err := s.accountRepository.FindByUserID(userID)
	if err != nil {
		return nil, err
	}

	// Variable that will hold the list
	// variable = a list in shape of AccountResponse that is currently 0 and will be length of how many accounts
	responses := make([]*dto.AccountResponse, 0, len(accounts))

	// for each account append AccountResponse into responses
	for _, account := range accounts {
		responses = append(responses, &dto.AccountResponse{
			ID:   account.ID,
			Name: account.Name,
		})
	}

	return responses, nil
}

func (s *AccountService) Update(userID uuid.UUID, accountID uuid.UUID, req dto.UpdateAccountRequest) (*dto.AccountResponse, error) {
	// Fetch the account
	account, err := s.accountRepository.GetByID(accountID)
	if err != nil {
		return nil, err
	}

	// Check if the account exists
	if account == nil {
		return nil, ErrAccountNotFound
	}

	// Check if the account belongs to the user - dont reveal account ownership to unauthorized users
	if account.UserID != userID {
		return nil, ErrAccountNotFound
	}

	// Validate the new name
	newName := strings.TrimSpace(req.Name)
	if newName == "" {
		return nil, ErrAccountNameRequired
	}

	// Set the new name on the existing account
	account.Name = newName

	// Update the name to the repository
	err = s.accountRepository.Update(account)
	if err != nil {
		return nil, err
	}

	// return to frontend the newly updated account information
	return &dto.AccountResponse{
		ID:   account.ID,
		Name: account.Name,
	}, nil
}

func (s *AccountService) Delete(userID uuid.UUID, accountID uuid.UUID) error {
	account, err := s.accountRepository.GetByID(accountID)
	if err != nil {
		return err
	}

	// Check if the account exists
	if account == nil {
		return ErrAccountNotFound
	}

	// Check if the account belongs to the user - dont reveal account ownership to unauthorized users
	if account.UserID != userID {
		return ErrAccountNotFound
	}

	err = s.accountRepository.Delete(accountID)

	if err != nil {
		return err
	}

	return nil
}
