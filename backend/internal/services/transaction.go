package services

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/calendar"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/money"
	"github.com/kacperkg/vellum/internal/repository"
)

type TransactionService struct {
	transactionRepository repository.TransactionRepository
	accountRepository     repository.AccountRepository
	categoryRepository    repository.CategoryRepository
}

func NewTransactionService(
	transactionRepository repository.TransactionRepository,
	accountRepository repository.AccountRepository,
	categoryRepository repository.CategoryRepository,
) *TransactionService {
	return &TransactionService{
		transactionRepository: transactionRepository,
		accountRepository:     accountRepository,
		categoryRepository:    categoryRepository,
	}
}

func (s *TransactionService) Create(userID uuid.UUID, req CreateTransactionRequest) (*TransactionResponse, error) {
	if err := validateTransaction(
		req.Name,
		req.Amount,
		req.AccountID,
		req.CategoryID,
		req.Date,
		req.Frequency,
		req.Type,
	); err != nil {
		return nil, err
	}

	if err := s.validateRelations(userID, req.AccountID, req.CategoryID); err != nil {
		return nil, err
	}

	transaction := &models.Transaction{
		UserID:     userID,
		Name:       strings.TrimSpace(req.Name),
		Amount:     req.Amount,
		AccountID:  req.AccountID,
		CategoryID: req.CategoryID,
		Date:       calendar.Day(req.Date),
		Frequency:  req.Frequency,
		Type:       req.Type,
	}

	if err := s.transactionRepository.Create(transaction); err != nil {
		return nil, err
	}

	return transactionResponse(transaction)
}

func (s *TransactionService) GetByID(userID, transactionID uuid.UUID) (*TransactionResponse, error) {
	transaction, err := s.transactionRepository.FindByID(transactionID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTransactionNotFound
		}
		return nil, err
	}
	if transaction == nil || transaction.UserID != userID {
		return nil, ErrTransactionNotFound
	}

	return transactionResponse(transaction)
}

func (s *TransactionService) FindByUserID(userID uuid.UUID) ([]*TransactionResponse, error) {
	transactions, err := s.transactionRepository.FindByUserID(userID)
	if err != nil {
		return nil, err
	}

	return transactionResponses(transactions)
}

func (s *TransactionService) Update(userID, transactionID uuid.UUID, req UpdateTransactionRequest) (*TransactionResponse, error) {
	transaction, err := s.transactionRepository.FindByID(transactionID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTransactionNotFound
		}
		return nil, err
	}
	if transaction == nil || transaction.UserID != userID {
		return nil, ErrTransactionNotFound
	}

	if err := validateTransaction(
		req.Name,
		req.Amount,
		req.AccountID,
		req.CategoryID,
		req.Date,
		req.Frequency,
		req.Type,
	); err != nil {
		return nil, err
	}

	if err := s.validateRelations(userID, req.AccountID, req.CategoryID); err != nil {
		return nil, err
	}

	transaction.Name = strings.TrimSpace(req.Name)
	transaction.Amount = req.Amount
	transaction.AccountID = req.AccountID
	transaction.CategoryID = req.CategoryID
	transaction.Date = calendar.Day(req.Date)
	transaction.Frequency = req.Frequency
	transaction.Type = req.Type

	if err := s.transactionRepository.Update(userID, transaction); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTransactionNotFound
		}
		return nil, err
	}

	return transactionResponse(transaction)
}

func (s *TransactionService) Delete(userID, transactionID uuid.UUID) error {
	transaction, err := s.transactionRepository.FindByID(transactionID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrTransactionNotFound
		}
		return err
	}
	if transaction == nil || transaction.UserID != userID {
		return ErrTransactionNotFound
	}

	err = s.transactionRepository.Delete(userID, transactionID)
	if errors.Is(err, repository.ErrNotFound) {
		return ErrTransactionNotFound
	}
	return err
}

func (s *TransactionService) FindByAccountID(userID, accountID uuid.UUID) ([]*TransactionResponse, error) {
	if err := s.validateAccount(userID, accountID); err != nil {
		return nil, err
	}

	transactions, err := s.transactionRepository.FindByAccountID(userID, accountID)
	if err != nil {
		return nil, err
	}

	return transactionResponses(transactions)
}

func (s *TransactionService) FindByCategoryID(userID, categoryID uuid.UUID) ([]*TransactionResponse, error) {
	if err := s.validateCategory(categoryID); err != nil {
		return nil, err
	}

	transactions, err := s.transactionRepository.FindByCategoryID(userID, categoryID)
	if err != nil {
		return nil, err
	}

	return transactionResponses(transactions)
}

func (s *TransactionService) FindByDateRange(userID uuid.UUID, startDate, endDate time.Time) ([]*TransactionResponse, error) {
	startDate, endDate = calendar.Day(startDate), calendar.Day(endDate)
	if !calendar.Valid(startDate) || !calendar.Valid(endDate) || startDate.After(endDate) {
		return nil, ErrTransactionDateRange
	}

	transactions, err := s.transactionRepository.FindByDateRange(userID, startDate, endDate)
	if err != nil {
		return nil, err
	}

	return transactionResponses(transactions)
}

func (s *TransactionService) FindByFrequency(userID uuid.UUID, frequency models.Frequency) ([]*TransactionResponse, error) {
	if !validFrequency(frequency) {
		return nil, ErrTransactionFrequency
	}

	transactions, err := s.transactionRepository.FindByFrequency(userID, frequency)
	if err != nil {
		return nil, err
	}

	return transactionResponses(transactions)
}

func (s *TransactionService) validateRelations(userID, accountID, categoryID uuid.UUID) error {
	if err := s.validateAccount(userID, accountID); err != nil {
		return err
	}

	return s.validateCategory(categoryID)
}

func (s *TransactionService) validateAccount(userID, accountID uuid.UUID) error {
	if accountID == uuid.Nil {
		return ErrTransactionAccountRequired
	}

	account, err := s.accountRepository.GetByID(accountID)
	if errors.Is(err, money.ErrRange) && account != nil {
		if account.UserID != userID {
			return ErrAccountNotFound
		}
		err = nil
	}
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

func (s *TransactionService) validateCategory(categoryID uuid.UUID) error {
	if categoryID == uuid.Nil {
		return ErrCategoryRequired
	}

	category, err := s.categoryRepository.FindByID(categoryID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrCategoryNotFound
		}
		return err
	}
	if category == nil {
		return ErrCategoryNotFound
	}

	return nil
}

func validateTransaction(
	name string,
	amount int64,
	accountID uuid.UUID,
	categoryID uuid.UUID,
	date time.Time,
	frequency models.Frequency,
	transactionType models.TransactionType,
) error {
	if strings.TrimSpace(name) == "" {
		return ErrTransactionNameRequired
	}
	if !money.Valid(amount) {
		return money.ErrRange
	}
	if amount <= 0 {
		return ErrTransactionAmountPositive
	}
	if accountID == uuid.Nil {
		return ErrTransactionAccountRequired
	}
	if categoryID == uuid.Nil {
		return ErrCategoryRequired
	}
	if !calendar.Valid(date) {
		return ErrTransactionDateRequired
	}
	if !validFrequency(frequency) {
		return ErrTransactionFrequency
	}
	if !validTransactionType(transactionType) {
		return ErrTransactionType
	}

	return nil
}

func validFrequency(frequency models.Frequency) bool {
	switch frequency {
	case models.OneOff, models.Daily, models.Weekly, models.Monthly, models.Yearly:
		return true
	default:
		return false
	}
}

func validTransactionType(transactionType models.TransactionType) bool {
	return transactionType == models.Income || transactionType == models.Expense
}

func transactionResponse(transaction *models.Transaction) (*TransactionResponse, error) {
	if !money.Valid(transaction.Amount) {
		return nil, money.ErrRange
	}
	return &TransactionResponse{
		ID:         transaction.ID,
		Name:       transaction.Name,
		Amount:     transaction.Amount,
		AccountID:  transaction.AccountID,
		CategoryID: transaction.CategoryID,
		Date:       calendar.Day(transaction.Date.UTC()),
		Frequency:  transaction.Frequency,
		Type:       transaction.Type,
	}, nil
}

func transactionResponses(transactions []*models.Transaction) ([]*TransactionResponse, error) {
	responses := make([]*TransactionResponse, 0, len(transactions))
	for _, transaction := range transactions {
		response, err := transactionResponse(transaction)
		if err != nil {
			return nil, err
		}
		responses = append(responses, response)
	}

	return responses, nil
}
