package services_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repository"
	"github.com/kacperkg/vellum/internal/services"
)

type fakeTransferRepository struct {
	transfer         *models.Transfer
	transfers        []*models.Transfer
	err              error
	created, updated *models.Transfer
	deleted          uuid.UUID
}

func (r *fakeTransferRepository) Create(value *models.Transfer) error {
	if r.err == nil {
		value.ID = uuid.New()
		r.created = value
	}
	return r.err
}
func (r *fakeTransferRepository) FindByID(uuid.UUID) (*models.Transfer, error) {
	return r.transfer, r.err
}
func (r *fakeTransferRepository) FindByUserID(uuid.UUID) ([]*models.Transfer, error) {
	return r.transfers, r.err
}
func (r *fakeTransferRepository) Update(_ uuid.UUID, value *models.Transfer) error {
	if r.err == nil {
		r.updated = value
	}
	return r.err
}
func (r *fakeTransferRepository) Delete(_ uuid.UUID, id uuid.UUID) error {
	if r.err == nil {
		r.deleted = id
	}
	return r.err
}

type transferAccounts struct {
	fakeAccountRepository
	byID map[uuid.UUID]*models.Account
}

func (r *transferAccounts) GetByID(id uuid.UUID) (*models.Account, error) {
	value, ok := r.byID[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return value, nil
}

func TestTransferService(t *testing.T) {
	userID, fromID, toID := uuid.New(), uuid.New(), uuid.New()
	accounts := &transferAccounts{byID: map[uuid.UUID]*models.Account{
		fromID: {BaseModel: models.BaseModel{ID: fromID}, UserID: userID},
		toID:   {BaseModel: models.BaseModel{ID: toID}, UserID: userID},
	}}
	request := services.CreateTransferRequest{Name: "  Subscription funding  ", Amount: 12599, FromAccountID: fromID, ToAccountID: toID, Date: time.Now()}

	t.Run("single record maps both sides and list", func(t *testing.T) {
		repo := &fakeTransferRepository{}
		service := services.NewTransferService(repo, accounts)
		response, err := service.Create(userID, request)
		if err != nil || response.Name != "Subscription funding" || response.Amount != 12599 || response.FromAccountID != fromID || response.ToAccountID != toID || repo.created.UserID != userID {
			t.Fatalf("response=%#v created=%#v error=%v", response, repo.created, err)
		}
		repo.transfer = repo.created
		repo.transfers = []*models.Transfer{repo.created}
		found, err := service.GetByID(userID, response.ID)
		if err != nil || found.ID != response.ID {
			t.Fatalf("found=%#v error=%v", found, err)
		}
		listed, err := service.FindByUserID(userID)
		if err != nil || len(listed) != 1 || listed[0].Amount != 12599 {
			t.Fatalf("listed=%#v error=%v", listed, err)
		}
		changed := request
		changed.Amount = 25000
		changed.FromAccountID, changed.ToAccountID = toID, fromID
		updated, err := service.Update(userID, response.ID, changed)
		if err != nil || updated.Amount != 25000 || repo.updated.FromAccountID != toID || repo.updated.ToAccountID != fromID {
			t.Fatalf("updated=%#v error=%v", updated, err)
		}
		if err := service.Delete(userID, response.ID); err != nil || repo.deleted != response.ID {
			t.Fatalf("deleted=%v error=%v", repo.deleted, err)
		}
	})

	for _, test := range []struct {
		name   string
		change func(*services.CreateTransferRequest)
		want   error
	}{
		{"name", func(r *services.CreateTransferRequest) { r.Name = " " }, services.ErrTransferNameRequired},
		{"zero amount", func(r *services.CreateTransferRequest) { r.Amount = 0 }, services.ErrTransferAmountPositive},
		{"negative amount", func(r *services.CreateTransferRequest) { r.Amount = -1 }, services.ErrTransferAmountPositive},
		{"source required", func(r *services.CreateTransferRequest) { r.FromAccountID = uuid.Nil }, services.ErrTransferAccountsRequired},
		{"destination required", func(r *services.CreateTransferRequest) { r.ToAccountID = uuid.Nil }, services.ErrTransferAccountsRequired},
		{"same account", func(r *services.CreateTransferRequest) { r.ToAccountID = r.FromAccountID }, services.ErrTransferAccountsDistinct},
		{"date required", func(r *services.CreateTransferRequest) { r.Date = time.Time{} }, services.ErrTransferDateRequired},
		{"frequency invalid", func(r *services.CreateTransferRequest) { r.Frequency = "fortnightly" }, services.ErrTransferFrequency},
		{"source missing", func(r *services.CreateTransferRequest) { r.FromAccountID = uuid.New() }, services.ErrAccountNotFound},
		{"destination missing", func(r *services.CreateTransferRequest) { r.ToAccountID = uuid.New() }, services.ErrAccountNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &fakeTransferRepository{}
			service := services.NewTransferService(repo, accounts)
			invalid := request
			test.change(&invalid)
			_, err := service.Create(userID, invalid)
			if !errors.Is(err, test.want) || repo.created != nil {
				t.Fatalf("error=%v created=%#v want=%v", err, repo.created, test.want)
			}
			repo.transfer = &models.Transfer{BaseModel: models.BaseModel{ID: uuid.New()}, UserID: userID}
			_, err = service.Update(userID, repo.transfer.ID, invalid)
			if !errors.Is(err, test.want) || repo.updated != nil {
				t.Fatalf("update error=%v want=%v", err, test.want)
			}
		})
	}

	t.Run("frequency defaults and every supported cadence persist through create read and update", func(t *testing.T) {
		for _, frequency := range []models.Frequency{"", models.OneOff, models.Daily, models.Weekly, models.Monthly, models.Yearly} {
			t.Run(string(frequency), func(t *testing.T) {
				repo := &fakeTransferRepository{}
				service := services.NewTransferService(repo, accounts)
				req := request
				req.Frequency = frequency
				want := frequency
				if want == "" {
					want = models.OneOff
				}
				created, err := service.Create(userID, req)
				if err != nil || created.Frequency != want || repo.created.Frequency != want {
					t.Fatalf("create=%#v persisted=%#v error=%v", created, repo.created, err)
				}
				repo.transfer = repo.created
				found, err := service.GetByID(userID, created.ID)
				if err != nil || found.Frequency != want {
					t.Fatalf("read=%#v error=%v", found, err)
				}
				updated, err := service.Update(userID, created.ID, req)
				if err != nil || updated.Frequency != want || repo.updated.Frequency != want {
					t.Fatalf("update=%#v persisted=%#v error=%v", updated, repo.updated, err)
				}
			})
		}
	})

	t.Run("foreign account on either side cannot receive or send funds", func(t *testing.T) {
		foreignID := uuid.New()
		accounts.byID[foreignID] = &models.Account{UserID: uuid.New()}
		for _, source := range []bool{true, false} {
			invalid := request
			if source {
				invalid.FromAccountID = foreignID
			} else {
				invalid.ToAccountID = foreignID
			}
			_, err := services.NewTransferService(&fakeTransferRepository{}, accounts).Create(userID, invalid)
			if !errors.Is(err, services.ErrAccountNotFound) {
				t.Fatalf("error=%v", err)
			}
		}
	})

	t.Run("foreign transfer hidden for read update delete", func(t *testing.T) {
		repo := &fakeTransferRepository{transfer: &models.Transfer{BaseModel: models.BaseModel{ID: uuid.New()}, UserID: uuid.New()}}
		service := services.NewTransferService(repo, accounts)
		if _, err := service.GetByID(userID, repo.transfer.ID); !errors.Is(err, services.ErrTransferNotFound) {
			t.Fatal(err)
		}
		if _, err := service.Update(userID, repo.transfer.ID, request); !errors.Is(err, services.ErrTransferNotFound) {
			t.Fatal(err)
		}
		if err := service.Delete(userID, repo.transfer.ID); !errors.Is(err, services.ErrTransferNotFound) {
			t.Fatal(err)
		}
		if repo.updated != nil || repo.deleted != uuid.Nil {
			t.Fatal("foreign transfer was mutated")
		}
	})
}

func TestAccountDeleteReferencedLedgerIsConflict(t *testing.T) {
	userID, accountID := uuid.New(), uuid.New()
	repo := &transferAccounts{fakeAccountRepository: fakeAccountRepository{Err: repository.ErrConflict}, byID: map[uuid.UUID]*models.Account{
		accountID: {BaseModel: models.BaseModel{ID: accountID}, UserID: userID},
	}}
	if err := services.NewAccountService(repo).Delete(userID, accountID); !errors.Is(err, services.ErrAccountInUse) {
		t.Fatalf("error=%v", err)
	}
}
