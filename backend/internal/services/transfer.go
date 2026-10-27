package services

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repository"
)

type TransferService struct {
	transferRepository repository.TransferRepository
	accountRepository  repository.AccountRepository
}

func NewTransferService(transfers repository.TransferRepository, accounts repository.AccountRepository) *TransferService {
	return &TransferService{transferRepository: transfers, accountRepository: accounts}
}

func (s *TransferService) Create(userID uuid.UUID, req CreateTransferRequest) (*TransferResponse, error) {
	req.Frequency = transferFrequency(req.Frequency)
	if err := s.validate(userID, req); err != nil {
		return nil, err
	}
	transfer := &models.Transfer{UserID: userID, Name: strings.TrimSpace(req.Name), Amount: req.Amount, FromAccountID: req.FromAccountID, ToAccountID: req.ToAccountID, Date: req.Date, Frequency: req.Frequency}
	if err := s.transferRepository.Create(transfer); err != nil {
		return nil, err
	}
	return transferResponse(transfer), nil
}

func (s *TransferService) GetByID(userID, transferID uuid.UUID) (*TransferResponse, error) {
	transfer, err := s.owned(userID, transferID)
	if err != nil {
		return nil, err
	}
	return transferResponse(transfer), nil
}

func (s *TransferService) FindByUserID(userID uuid.UUID) ([]*TransferResponse, error) {
	transfers, err := s.transferRepository.FindByUserID(userID)
	if err != nil {
		return nil, err
	}
	responses := make([]*TransferResponse, 0, len(transfers))
	for _, transfer := range transfers {
		responses = append(responses, transferResponse(transfer))
	}
	return responses, nil
}

func (s *TransferService) Update(userID, transferID uuid.UUID, req UpdateTransferRequest) (*TransferResponse, error) {
	transfer, err := s.owned(userID, transferID)
	if err != nil {
		return nil, err
	}
	req.Frequency = transferFrequency(req.Frequency)
	if err := s.validate(userID, req); err != nil {
		return nil, err
	}
	transfer.Name = strings.TrimSpace(req.Name)
	transfer.Amount = req.Amount
	transfer.FromAccountID = req.FromAccountID
	transfer.ToAccountID = req.ToAccountID
	transfer.Date = req.Date
	transfer.Frequency = req.Frequency
	if err := s.transferRepository.Update(userID, transfer); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTransferNotFound
		}
		return nil, err
	}
	return transferResponse(transfer), nil
}

func (s *TransferService) Delete(userID, transferID uuid.UUID) error {
	if _, err := s.owned(userID, transferID); err != nil {
		return err
	}
	err := s.transferRepository.Delete(userID, transferID)
	if errors.Is(err, repository.ErrNotFound) {
		return ErrTransferNotFound
	}
	return err
}

func (s *TransferService) owned(userID, transferID uuid.UUID) (*models.Transfer, error) {
	transfer, err := s.transferRepository.FindByID(transferID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTransferNotFound
		}
		return nil, err
	}
	if transfer == nil || transfer.UserID != userID {
		return nil, ErrTransferNotFound
	}
	return transfer, nil
}

func (s *TransferService) validate(userID uuid.UUID, req CreateTransferRequest) error {
	if err := validateResourceName(req.Name); err != nil {
		return err
	}
	if strings.TrimSpace(req.Name) == "" {
		return ErrTransferNameRequired
	}
	if req.Amount <= 0 {
		return ErrTransferAmountPositive
	}
	if req.FromAccountID == uuid.Nil || req.ToAccountID == uuid.Nil {
		return ErrTransferAccountsRequired
	}
	if req.FromAccountID == req.ToAccountID {
		return ErrTransferAccountsDistinct
	}
	if req.Date.IsZero() {
		return ErrTransferDateRequired
	}
	if !validFrequency(req.Frequency) {
		return ErrTransferFrequency
	}
	for _, id := range []uuid.UUID{req.FromAccountID, req.ToAccountID} {
		account, err := s.accountRepository.GetByID(id)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return ErrAccountNotFound
			}
			return err
		}
		if account == nil || account.UserID != userID {
			return ErrAccountNotFound
		}
	}
	return nil
}

func transferResponse(transfer *models.Transfer) *TransferResponse {
	return &TransferResponse{ID: transfer.ID, Name: transfer.Name, Amount: transfer.Amount, FromAccountID: transfer.FromAccountID, ToAccountID: transfer.ToAccountID, Date: transfer.Date, Frequency: transferFrequency(transfer.Frequency)}
}

func transferFrequency(frequency models.Frequency) models.Frequency {
	if frequency == "" {
		return models.OneOff
	}
	return frequency
}

// WithContext returns a request-scoped copy; the shared service is never mutated.
func (s *TransferService) WithContext(ctx context.Context) *TransferService {
	clone := *s
	clone.transferRepository = s.transferRepository.WithContext(ctx)
	clone.accountRepository = s.accountRepository.WithContext(ctx)
	return &clone
}
