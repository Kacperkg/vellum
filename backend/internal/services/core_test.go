package services_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/services"
	"golang.org/x/crypto/bcrypt"
)

func TestAccountService(t *testing.T) {
	userID, accountID := uuid.New(), uuid.New()

	t.Run("create trims and persists", func(t *testing.T) {
		repo := &fakeAccountRepository{}
		response, err := services.NewAccountService(repo).Create(userID, services.CreateAccountRequest{Name: "  Main  "})
		if err != nil || repo.Created == nil || repo.Created.Name != "Main" || response.ID == uuid.Nil {
			t.Fatalf("response=%#v created=%#v err=%v", response, repo.Created, err)
		}
	})

	t.Run("rejects blank name", func(t *testing.T) {
		_, err := services.NewAccountService(&fakeAccountRepository{}).Create(userID, services.CreateAccountRequest{Name: " "})
		if !errors.Is(err, services.ErrAccountNameRequired) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("hides foreign account", func(t *testing.T) {
		repo := &fakeAccountRepository{Account: &models.Account{UserID: uuid.New()}}
		_, err := services.NewAccountService(repo).GetByID(userID, accountID)
		if !errors.Is(err, services.ErrAccountNotFound) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("lists maps responses", func(t *testing.T) {
		repo := &fakeAccountRepository{Accounts: []*models.Account{{BaseModel: models.BaseModel{ID: accountID}, Name: "Main"}}}
		response, err := services.NewAccountService(repo).FindByUserID(userID)
		if err != nil || len(response) != 1 || response[0].Name != "Main" {
			t.Fatalf("response=%#v err=%v", response, err)
		}
	})

	t.Run("updates and deletes owned account", func(t *testing.T) {
		repo := &fakeAccountRepository{Account: &models.Account{BaseModel: models.BaseModel{ID: accountID}, UserID: userID}}
		service := services.NewAccountService(repo)
		if _, err := service.Update(userID, accountID, services.UpdateAccountRequest{Name: " Savings "}); err != nil {
			t.Fatal(err)
		}
		if repo.Updated == nil || repo.Updated.Name != "Savings" {
			t.Fatalf("updated=%#v", repo.Updated)
		}
		if err := service.Delete(userID, accountID); err != nil || repo.Deleted != accountID {
			t.Fatalf("deleted=%v err=%v", repo.Deleted, err)
		}
	})
}

func TestBudgetService(t *testing.T) {
	userID, budgetID, categoryID := uuid.New(), uuid.New(), uuid.New()
	categoryRepo := &fakeCategoryRepository{Category: &models.Category{BaseModel: models.BaseModel{ID: categoryID}}}

	t.Run("create", func(t *testing.T) {
		repo := &fakeBudgetRepository{}
		response, err := services.NewBudgetService(repo, categoryRepo).Create(userID, services.CreateBudgetRequest{CategoryID: categoryID, AllocatedAmount: 15000})
		if err != nil || repo.Created == nil || response.AllocatedAmount != 15000 {
			t.Fatalf("response=%#v created=%#v err=%v", response, repo.Created, err)
		}
	})

	for _, test := range []struct {
		name string
		req  services.CreateBudgetRequest
		want error
	}{
		{"category required", services.CreateBudgetRequest{AllocatedAmount: 1}, services.ErrCategoryRequired},
		{"amount positive", services.CreateBudgetRequest{CategoryID: categoryID}, services.ErrAllocatedAmountPositive},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := services.NewBudgetService(&fakeBudgetRepository{}, categoryRepo).Create(userID, test.req)
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}

	t.Run("category must exist", func(t *testing.T) {
		_, err := services.NewBudgetService(&fakeBudgetRepository{}, &fakeCategoryRepository{}).Create(userID, services.CreateBudgetRequest{CategoryID: categoryID, AllocatedAmount: 1})
		if !errors.Is(err, services.ErrCategoryNotFound) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("rejects duplicate category", func(t *testing.T) {
		repo := &fakeBudgetRepository{Budgets: []*models.Budget{{
			BaseModel:  models.BaseModel{ID: uuid.New()},
			UserID:     userID,
			CategoryID: categoryID,
		}}}
		_, err := services.NewBudgetService(repo, categoryRepo).Create(userID, services.CreateBudgetRequest{CategoryID: categoryID, AllocatedAmount: 1})
		if !errors.Is(err, services.ErrBudgetCategoryExists) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("ownership update delete", func(t *testing.T) {
		repo := &fakeBudgetRepository{Budget: &models.Budget{BaseModel: models.BaseModel{ID: budgetID}, UserID: userID}}
		service := services.NewBudgetService(repo, categoryRepo)
		if _, err := service.Update(userID, budgetID, services.UpdateBudgetRequest{CategoryID: categoryID, AllocatedAmount: 20000}); err != nil {
			t.Fatal(err)
		}
		if repo.Updated == nil || repo.Updated.AllocatedAmount != 20000 {
			t.Fatalf("updated=%#v", repo.Updated)
		}
		if err := service.Delete(userID, budgetID); err != nil || repo.Deleted != budgetID {
			t.Fatalf("deleted=%v err=%v", repo.Deleted, err)
		}
		repo.Budget.UserID = uuid.New()
		if _, err := service.FindByID(userID, budgetID); !errors.Is(err, services.ErrBudgetNotFound) {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestCategoryService(t *testing.T) {
	categoryID := uuid.New()
	t.Run("find and list", func(t *testing.T) {
		repo := &fakeCategoryRepository{Category: &models.Category{BaseModel: models.BaseModel{ID: categoryID}, Name: "Food"}, Categories: []*models.Category{{Name: "Food"}}}
		service := services.NewCategoryService(repo)
		found, err := service.FindByID(categoryID)
		if err != nil || found.Name != "Food" {
			t.Fatalf("found=%#v err=%v", found, err)
		}
		list, err := service.ListAll()
		if err != nil || len(list) != 1 {
			t.Fatalf("list=%#v err=%v", list, err)
		}
	})
	t.Run("not found", func(t *testing.T) {
		_, err := services.NewCategoryService(&fakeCategoryRepository{}).FindByID(categoryID)
		if !errors.Is(err, services.ErrCategoryNotFound) {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestUserService(t *testing.T) {
	const secret = "test-secret"
	request := services.RegisterRequest{FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.com", Password: "password123"}

	t.Run("register creates user settings and token", func(t *testing.T) {
		users := &fakeUserRepository{}
		response, err := services.NewUserService(users, &fakeRefreshTokenRepository{}, secret, time.Hour, 30*24*time.Hour).Register(request)
		if err != nil || response.AccessToken == "" || response.RefreshToken == "" || response.User.Email != request.Email || users.Created == nil || users.CreatedSettings == nil || users.CreatedRefreshToken == nil {
			t.Fatalf("response=%#v err=%v", response, err)
		}
		if users.CreatedSettings.Currency != models.GBP || users.CreatedSettings.UserID != users.Created.ID {
			t.Fatalf("settings=%#v", users.CreatedSettings)
		}
	})

	t.Run("duplicate registration", func(t *testing.T) {
		users := &fakeUserRepository{User: &models.User{}}
		_, err := services.NewUserService(users, &fakeRefreshTokenRepository{}, secret, time.Hour, 30*24*time.Hour).Register(request)
		if !errors.Is(err, services.ErrUserEmailExists) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("login", func(t *testing.T) {
		hash, err := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.MinCost)
		if err != nil {
			t.Fatal(err)
		}
		users := &fakeUserRepository{User: &models.User{BaseModel: models.BaseModel{ID: uuid.New()}, Email: request.Email, PasswordHash: string(hash)}}
		refreshTokens := &fakeRefreshTokenRepository{}
		service := services.NewUserService(users, refreshTokens, secret, time.Hour, 30*24*time.Hour)
		response, err := service.Login(services.LoginRequest{Email: request.Email, Password: request.Password})
		if err != nil || response.AccessToken == "" || response.RefreshToken == "" || response.User.ID != users.User.ID || response.User.Email != request.Email {
			t.Fatalf("response=%#v err=%v", response, err)
		}
		_, err = service.Login(services.LoginRequest{Email: request.Email, Password: "wrong"})
		if !errors.Is(err, services.ErrInvalidCredentials) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("refresh rotates token and rejects reuse", func(t *testing.T) {
		userID := uuid.New()
		hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
		if err != nil {
			t.Fatal(err)
		}
		users := &fakeUserRepository{User: &models.User{BaseModel: models.BaseModel{ID: userID}, Email: "ada@example.com", PasswordHash: string(hash)}}
		refreshTokens := &fakeRefreshTokenRepository{}
		service := services.NewUserService(users, refreshTokens, secret, time.Hour, 30*24*time.Hour)

		initial, err := service.Login(services.LoginRequest{Email: "ada@example.com", Password: "password123"})
		if err != nil {
			t.Fatal(err)
		}
		rotated, err := service.Refresh(services.RefreshRequest{RefreshToken: initial.RefreshToken})
		if err != nil || rotated.AccessToken == "" || rotated.RefreshToken == "" || rotated.RefreshToken == initial.RefreshToken || rotated.User.ID != userID {
			t.Fatalf("rotated=%#v error=%v", rotated, err)
		}
		if _, err := service.Refresh(services.RefreshRequest{RefreshToken: initial.RefreshToken}); !errors.Is(err, services.ErrInvalidRefreshToken) {
			t.Fatalf("reused token error=%v", err)
		}
		if err := service.Logout(services.RefreshRequest{RefreshToken: rotated.RefreshToken}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Refresh(services.RefreshRequest{RefreshToken: rotated.RefreshToken}); !errors.Is(err, services.ErrInvalidRefreshToken) {
			t.Fatalf("logged-out token error=%v", err)
		}
	})
}

func TestUserSettingsService(t *testing.T) {
	userID, settingsID := uuid.New(), uuid.New()
	settings := &models.UserSettings{
		BaseModel:    models.BaseModel{ID: settingsID},
		UserID:       userID,
		Currency:     models.GBP,
		DateFormat:   models.DDMMYYYY,
		NumberFormat: models.CommaDecimal,
		WeekStartsOn: models.Monday,
	}
	repository := &fakeUserSettingsRepository{Settings: settings}
	service := services.NewUserSettingsService(repository)

	response, err := service.Get(userID)
	if err != nil || response.ID != settingsID || response.Currency != models.GBP {
		t.Fatalf("Get() response=%#v error=%v", response, err)
	}

	request := services.UpdateUserSettingsRequest{
		Currency:     models.EUR,
		DateFormat:   models.YYYYMMDD,
		NumberFormat: models.DotDecimal,
		WeekStartsOn: models.Sunday,
	}
	response, err = service.Update(userID, request)
	if err != nil || repository.Updated == nil || response.Currency != models.EUR {
		t.Fatalf("Update() response=%#v updated=%#v error=%v", response, repository.Updated, err)
	}

	t.Run("not found", func(t *testing.T) {
		_, err := services.NewUserSettingsService(&fakeUserSettingsRepository{}).Get(userID)
		if !errors.Is(err, services.ErrSettingsNotFound) {
			t.Fatalf("error=%v", err)
		}
	})

	for _, test := range []struct {
		name   string
		mutate func(*services.UpdateUserSettingsRequest)
		want   error
	}{
		{"currency", func(r *services.UpdateUserSettingsRequest) { r.Currency = "CAD" }, services.ErrSettingsCurrencyInvalid},
		{"date format", func(r *services.UpdateUserSettingsRequest) { r.DateFormat = "invalid" }, services.ErrSettingsDateFormatInvalid},
		{"number format", func(r *services.UpdateUserSettingsRequest) { r.NumberFormat = "invalid" }, services.ErrSettingsNumberFormatInvalid},
		{"week start", func(r *services.UpdateUserSettingsRequest) { r.WeekStartsOn = "Friday" }, services.ErrSettingsWeekStartInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := request
			test.mutate(&req)
			_, err := service.Update(userID, req)
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}
