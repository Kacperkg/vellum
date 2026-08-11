package services

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/dto"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repositories"
)

type TransactionService struct {
	transactionRepository repositories.TransactionRepository
	accountRepository     repositories.AccountRepository
	categoryRepository    repositories.CategoryRepository
}

func NewTransactionService(
	transactionRepository repositories.TransactionRepository,
	accountRepository repositories.AccountRepository,
	categoryRepository repositories.CategoryRepository,
) *TransactionService {
	return &TransactionService{
		transactionRepository: transactionRepository,
		accountRepository:     accountRepository,
		categoryRepository:    categoryRepository,
	}
}

func (s *TransactionService) Create(userID uuid.UUID, req dto.CreateTransactionRequest) (*dto.TransactionResponse, error) {
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
		Date:       req.Date,
		Frequency:  req.Frequency,
		Type:       req.Type,
	}

	if err := s.transactionRepository.Create(transaction); err != nil {
		return nil, err
	}

	return transactionResponse(transaction), nil
}

func (s *TransactionService) GetByID(userID, transactionID uuid.UUID) (*dto.TransactionResponse, error) {
	transaction, err := s.transactionRepository.FindByID(transactionID)
	if err != nil {
		return nil, err
	}
	if transaction == nil || transaction.UserID != userID {
		return nil, ErrTransactionNotFound
	}

	return transactionResponse(transaction), nil
}

func (s *TransactionService) FindByUserID(userID uuid.UUID) ([]*dto.TransactionResponse, error) {
	transactions, err := s.transactionRepository.FindByUserID(userID)
	if err != nil {
		return nil, err
	}

	return transactionResponses(transactions), nil
}

func (s *TransactionService) Update(userID, transactionID uuid.UUID, req dto.UpdateTransactionRequest) (*dto.TransactionResponse, error) {
	transaction, err := s.transactionRepository.FindByID(transactionID)
	if err != nil {
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
	transaction.Date = req.Date
	transaction.Frequency = req.Frequency
	transaction.Type = req.Type

	if err := s.transactionRepository.Update(transaction); err != nil {
		return nil, err
	}

	return transactionResponse(transaction), nil
}

func (s *TransactionService) Delete(userID, transactionID uuid.UUID) error {
	transaction, err := s.transactionRepository.FindByID(transactionID)
	if err != nil {
		return err
	}
	if transaction == nil || transaction.UserID != userID {
		return ErrTransactionNotFound
	}

	return s.transactionRepository.Delete(transactionID)
}

func (s *TransactionService) FindByAccountID(userID, accountID uuid.UUID) ([]*dto.TransactionResponse, error) {
	if err := s.validateAccount(userID, accountID); err != nil {
		return nil, err
	}

	transactions, err := s.transactionRepository.FindByAccountID(accountID)
	if err != nil {
		return nil, err
	}

	return transactionResponses(transactions), nil
}

func (s *TransactionService) FindByCategoryID(userID, categoryID uuid.UUID) ([]*dto.TransactionResponse, error) {
	if err := s.validateCategory(categoryID); err != nil {
		return nil, err
	}

	transactions, err := s.transactionRepository.FindByCategoryID(userID, categoryID)
	if err != nil {
		return nil, err
	}

	return transactionResponses(transactions), nil
}

func (s *TransactionService) FindByDateRange(userID uuid.UUID, startDate, endDate time.Time) ([]*dto.TransactionResponse, error) {
	if startDate.IsZero() || endDate.IsZero() || startDate.After(endDate) {
		return nil, ErrTransactionDateRange
	}

	transactions, err := s.transactionRepository.FindByDateRange(userID, startDate, endDate)
	if err != nil {
		return nil, err
	}

	return transactionResponses(transactions), nil
}

func (s *TransactionService) FindByFrequency(userID uuid.UUID, frequency models.Frequency) ([]*dto.TransactionResponse, error) {
	if !validFrequency(frequency) {
		return nil, ErrTransactionFrequency
	}

	transactions, err := s.transactionRepository.FindByFrequency(userID, frequency)
	if err != nil {
		return nil, err
	}

	return transactionResponses(transactions), nil
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
	if err != nil {
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
	if amount <= 0 {
		return ErrTransactionAmountPositive
	}
	if accountID == uuid.Nil {
		return ErrTransactionAccountRequired
	}
	if categoryID == uuid.Nil {
		return ErrCategoryRequired
	}
	if date.IsZero() {
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

func transactionResponse(transaction *models.Transaction) *dto.TransactionResponse {
	return &dto.TransactionResponse{
		ID:         transaction.ID,
		Name:       transaction.Name,
		Amount:     transaction.Amount,
		AccountID:  transaction.AccountID,
		CategoryID: transaction.CategoryID,
		Date:       transaction.Date,
		Frequency:  transaction.Frequency,
		Type:       transaction.Type,
	}
}

func transactionResponses(transactions []*models.Transaction) []*dto.TransactionResponse {
	responses := make([]*dto.TransactionResponse, 0, len(transactions))
	for _, transaction := range transactions {
		responses = append(responses, transactionResponse(transaction))
	}

	return responses
}
