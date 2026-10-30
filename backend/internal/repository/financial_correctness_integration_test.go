package repository_test

import (
	"errors"
	"math"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/apperrors"
	"github.com/kacperkg/vellum/internal/calendar"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/money"
	"github.com/kacperkg/vellum/internal/repository"
	"github.com/kacperkg/vellum/internal/services"
	"gorm.io/gorm"
)

type financialFixture struct {
	db           *gorm.DB
	user         *models.User
	accounts     [2]*models.Account
	category     *models.Category
	transactions *services.TransactionService
	goals        *services.GoalService
	transfers    *services.TransferService
}

func financialDB(t *testing.T) financialFixture {
	t.Helper()
	db := integrationDB(t)
	f := financialFixture{db: db, user: &models.User{FirstName: "Financial", LastName: "Test", Email: uuid.NewString() + "@example.com", PasswordHash: "hash"}, category: &models.Category{Name: uuid.NewString()}}
	for _, value := range []any{f.user, f.category} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := range f.accounts {
		f.accounts[i] = &models.Account{UserID: f.user.ID, Name: "Test account"}
		if err := db.Create(f.accounts[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, model := range []any{&models.Transaction{}, &models.Transfer{}, &models.Goal{}, &models.Budget{}, &models.Account{}} {
			db.Where("user_id = ?", f.user.ID).Delete(model)
		}
		db.Delete(f.user)
		db.Delete(f.category)
	})
	accounts := repository.NewAccountRepository(db)
	f.transactions = services.NewTransactionService(repository.NewTransactionRepository(db), accounts, repository.NewCategoryRepository(db))
	f.goals = services.NewGoalService(repository.NewGoalRepository(db), accounts)
	f.transfers = services.NewTransferService(repository.NewTransferRepository(db), accounts)
	return f
}

func (f financialFixture) transaction(amount int64, kind models.TransactionType, account int) services.CreateTransactionRequest {
	return services.CreateTransactionRequest{Name: "Entry", Amount: amount, Type: kind, AccountID: f.accounts[account].ID, CategoryID: f.category.ID, Date: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Frequency: models.OneOff}
}
func requireMoneyError(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, apperrors.ErrInvalidInput) {
		t.Fatalf("expected controlled money validation error: %v", err)
	}
}
func requireCount(t *testing.T, db *gorm.DB, model any, user uuid.UUID, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(model).Where("user_id = ?", user).Count(&count).Error; err != nil || count != want {
		t.Fatalf("count=%d want=%d error=%v", count, want, err)
	}
}

func TestFinancialMoneyWritesAndAggregates(t *testing.T) {
	t.Run("input bounds and rejected writes", func(t *testing.T) {
		f := financialDB(t)
		for _, amount := range []int64{money.Max + 1, math.MaxInt64, 0, -1, math.MinInt64} {
			_, err := f.transactions.Create(f.user.ID, f.transaction(amount, models.Income, 0))
			requireMoneyError(t, err)
		}
		requireCount(t, f.db, &models.Transaction{}, f.user.ID, 0)
		penny, err := f.transactions.Create(f.user.ID, f.transaction(1, models.Income, 0))
		if err != nil || penny.Amount != 1 {
			t.Fatalf("minimum=%v %v", penny, err)
		}
		_, err = f.transactions.Create(f.user.ID, f.transaction(money.Max-1, models.Income, 0))
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.transactions.Create(f.user.ID, f.transaction(1, models.Income, 0))
		requireMoneyError(t, err)
		requireCount(t, f.db, &models.Transaction{}, f.user.ID, 2)
		got, err := repository.NewAccountRepository(f.db).GetByID(f.accounts[0].ID)
		if err != nil || got.Balance != money.Max {
			t.Fatalf("balance=%v %v", got, err)
		}
		req := services.UpdateTransactionRequest(f.transaction(2, models.Income, 0))
		_, err = f.transactions.Update(f.user.ID, penny.ID, req)
		requireMoneyError(t, err)
		unchanged, err := f.transactions.GetByID(f.user.ID, penny.ID)
		if err != nil || unchanged.Amount != 1 {
			t.Fatalf("partial update=%v %v", unchanged, err)
		}
	})
	t.Run("available balance and linked goal allocation", func(t *testing.T) {
		f := financialDB(t)
		_, err := f.transactions.Create(f.user.ID, f.transaction(money.Max, models.Expense, 0))
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.goals.Create(f.user.ID, services.CreateGoalRequest{Name: "Goal", AccountID: &f.accounts[0].ID, TargetAmount: 1})
		requireMoneyError(t, err)
		requireCount(t, f.db, &models.Goal{}, f.user.ID, 0)
		// Allocation alone is also bounded when several individually valid goals combine.
		_, err = f.goals.Create(f.user.ID, services.CreateGoalRequest{Name: "Maximum", AccountID: &f.accounts[1].ID, TargetAmount: money.Max})
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.goals.Create(f.user.ID, services.CreateGoalRequest{Name: "Extra", AccountID: &f.accounts[1].ID, TargetAmount: 1})
		requireMoneyError(t, err)
		requireCount(t, f.db, &models.Goal{}, f.user.ID, 1)
	})
	t.Run("transfer conservation and atomic update delete", func(t *testing.T) {
		f := financialDB(t)
		income, err := f.transactions.Create(f.user.ID, f.transaction(money.Max, models.Income, 0))
		if err != nil {
			t.Fatal(err)
		}
		transfer, err := f.transfers.Create(f.user.ID, services.CreateTransferRequest{Name: "Move", Amount: money.Max, FromAccountID: f.accounts[0].ID, ToAccountID: f.accounts[1].ID, Date: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		rows, err := repository.NewAccountRepository(f.db).FindByUserID(f.user.ID)
		if err != nil {
			t.Fatal(err)
		}
		total := int64(0)
		for _, row := range rows {
			total += row.Balance
		}
		if total != money.Max {
			t.Fatalf("transfer changed total: %d", total)
		}
		_, err = f.transactions.Create(f.user.ID, f.transaction(money.Max, models.Expense, 0))
		if err != nil {
			t.Fatal(err)
		}
		requireMoneyError(t, f.transactions.Delete(f.user.ID, income.ID))
		requireCount(t, f.db, &models.Transaction{}, f.user.ID, 2)
		_, err = f.transfers.Update(f.user.ID, transfer.ID, services.UpdateTransferRequest{Name: "Reverse", Amount: money.Max, FromAccountID: f.accounts[1].ID, ToAccountID: f.accounts[0].ID, Date: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		rows, err = repository.NewAccountRepository(f.db).FindByUserID(f.user.ID)
		if err != nil {
			t.Fatal(err)
		}
		if rows[0].Balance+rows[1].Balance != 0 {
			t.Fatal("reversed transfer did not conserve net zero")
		}
	})
	t.Run("large frontend totals do not impose lifetime cap", func(t *testing.T) {
		f := financialDB(t)
		for i := range f.accounts {
			if _, err := f.transactions.Create(f.user.ID, f.transaction(money.Max, models.Income, i)); err != nil {
				t.Fatal(err)
			}
		}
		rows, err := repository.NewAccountRepository(f.db).FindByUserID(f.user.ID)
		if err != nil {
			t.Fatal(err)
		}
		sum := new(big.Int)
		for _, row := range rows {
			sum.Add(sum, big.NewInt(row.Balance))
		}
		if sum.String() != "18014398509481982" {
			t.Fatal(sum)
		}
	})
}

func TestFinancialConcurrentLimit(t *testing.T) {
	f := financialDB(t)
	if _, err := f.transactions.Create(f.user.ID, f.transaction(money.Max-1, models.Income, 0)); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := f.transactions.Create(f.user.ID, f.transaction(1, models.Income, 0))
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	success, rejected := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, money.ErrRange) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("success=%d rejected=%d", success, rejected)
	}
	requireCount(t, f.db, &models.Transaction{}, f.user.ID, 2)
}

func TestFinancialLegacyOverflowControlledAndRepairable(t *testing.T) {
	f := financialDB(t)
	// Existing records remain unchanged. Their numeric aggregates exceed int64.
	var legacy []*models.Transaction
	for i := 0; i < 2; i++ {
		row := &models.Transaction{UserID: f.user.ID, Name: "Legacy", Amount: math.MaxInt64, AccountID: f.accounts[0].ID, CategoryID: f.category.ID, Date: time.Now(), Frequency: models.OneOff, Type: models.Income}
		if err := f.db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
		legacy = append(legacy, row)
	}
	_, err := repository.NewAccountRepository(f.db).GetByID(f.accounts[0].ID)
	requireMoneyError(t, err)
	_, err = f.transactions.FindByUserID(f.user.ID)
	requireMoneyError(t, err)
	// A foreign relation must still be hidden, even when its summary is invalid.
	_, err = f.transactions.Create(uuid.New(), f.transaction(1, models.Income, 0))
	if !errors.Is(err, services.ErrAccountNotFound) {
		t.Fatalf("foreign account=%v", err)
	}
	_, err = f.transactions.Update(f.user.ID, legacy[0].ID, services.UpdateTransactionRequest(f.transaction(1, models.Income, 0)))
	if err != nil {
		t.Fatalf("repair blocked: %v", err)
	}
	if err := f.transactions.Delete(f.user.ID, legacy[1].ID); err != nil {
		t.Fatal(err)
	}
	got, err := repository.NewAccountRepository(f.db).GetByID(f.accounts[0].ID)
	if err != nil || got.Balance != 1 {
		t.Fatalf("repaired=%v %v", got, err)
	}
	// MinInt64 expense negation occurs as numeric, never overflowing PostgreSQL bigint.
	row := &models.Transaction{UserID: f.user.ID, Name: "Legacy negative", Amount: math.MinInt64, AccountID: f.accounts[1].ID, CategoryID: f.category.ID, Date: time.Now(), Frequency: models.OneOff, Type: models.Expense}
	if err := f.db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	_, err = repository.NewAccountRepository(f.db).GetByID(f.accounts[1].ID)
	requireMoneyError(t, err)
	if err := f.transactions.Delete(f.user.ID, row.ID); err != nil {
		t.Fatal(err)
	}
}

func TestFinancialCalendarListFilterAndAccountAgreement(t *testing.T) {
	f := financialDB(t)
	for _, input := range []string{"2026-11-01T00:30:00+14:00", "2026-11-30T23:30:00-12:00", "2026-11-15"} {
		date, err := calendar.Parse(input)
		if err != nil {
			t.Fatal(err)
		}
		req := f.transaction(100, models.Expense, 0)
		req.Date = date
		got, err := f.transactions.Create(f.user.ID, req)
		if err != nil || got.Date.Month() != time.November {
			t.Fatalf("%s: %v %v", input, got, err)
		}
	}
	// A historical non-midnight UTC record belongs to its persisted UTC day and
	// the whole last day is included, independent of PostgreSQL session timezone.
	old := &models.Transaction{UserID: f.user.ID, Name: "Existing", Amount: 100, AccountID: f.accounts[0].ID, CategoryID: f.category.ID, Date: time.Date(2026, 11, 30, 23, 59, 59, 0, time.UTC), Frequency: models.OneOff, Type: models.Expense}
	if err := f.db.Create(old).Error; err != nil {
		t.Fatal(err)
	}
	list, err := f.transactions.FindByUserID(f.user.ID)
	if err != nil || len(list) != 4 {
		t.Fatalf("list=%v %v", list, err)
	}
	start, _ := calendar.Parse("2026-11-01")
	end, _ := calendar.Parse("2026-11-30")
	filtered, err := f.transactions.FindByDateRange(f.user.ID, start, end)
	if err != nil || len(filtered) != 4 {
		t.Fatalf("filter=%v %v", filtered, err)
	}
	for _, row := range list {
		if row.Date.Month() != time.November || row.Date.Hour() != 0 || row.Date.Location() != time.UTC {
			t.Fatalf("display month/date=%v", row.Date)
		}
	}
	account, err := repository.NewAccountRepository(f.db).GetByID(f.accounts[0].ID)
	if err != nil || account.Balance != -400 {
		t.Fatalf("account=%v %v", account, err)
	}
}

func TestFinancialDatabaseSessionTimezones(t *testing.T) {
	f := financialDB(t)
	for _, zone := range []string{"Etc/GMT+12", "Pacific/Kiritimati", "Europe/London"} {
		t.Run(zone, func(t *testing.T) {
			err := f.db.Connection(func(conn *gorm.DB) error {
				conn = conn.Session(&gorm.Session{NewDB: true})
				if err := conn.Exec("SELECT set_config('TimeZone', ?, false)", zone).Error; err != nil {
					return err
				}
				defer conn.Exec("SET TIME ZONE 'UTC'")
				service := services.NewTransactionService(repository.NewTransactionRepository(conn), repository.NewAccountRepository(conn), repository.NewCategoryRepository(conn))
				date, _ := calendar.Parse("2026-12-31T23:30:00-12:00")
				req := f.transaction(100, models.Expense, 0)
				req.Date = date
				row, err := service.Create(f.user.ID, req)
				if err != nil {
					return err
				}
				defer service.Delete(f.user.ID, row.ID)
				list, err := service.FindByUserID(f.user.ID)
				if err != nil {
					return err
				}
				start, _ := calendar.Parse("2026-12-01")
				end, _ := calendar.Parse("2026-12-31")
				filtered, err := service.FindByDateRange(f.user.ID, start, end)
				if err != nil {
					return err
				}
				if len(list) != 1 || len(filtered) != 1 || list[0].Date.Format(time.RFC3339) != "2026-12-31T00:00:00Z" {
					t.Fatalf("timezone %s list=%v filtered=%v", zone, list, filtered)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFinancialLegacyAvailableDoesNotWrap(t *testing.T) {
	f := financialDB(t)
	row := &models.Transaction{UserID: f.user.ID, Name: "Legacy expense", Amount: math.MaxInt64, AccountID: f.accounts[0].ID, CategoryID: f.category.ID, Date: time.Now(), Frequency: models.OneOff, Type: models.Expense}
	goal := &models.Goal{UserID: f.user.ID, Name: "Legacy target", AccountID: &f.accounts[0].ID, TargetAmount: math.MaxInt64}
	if err := f.db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(goal).Error; err != nil {
		t.Fatal(err)
	}
	accountService := services.NewAccountService(repository.NewAccountRepository(f.db))
	_, err := accountService.GetByID(f.user.ID, f.accounts[0].ID)
	requireMoneyError(t, err)
	_, err = accountService.GetByID(uuid.New(), f.accounts[0].ID)
	if !errors.Is(err, services.ErrAccountNotFound) {
		t.Fatalf("foreign summary revealed: %v", err)
	}
	// The old subtraction returned +2. No numeric response is emitted now.
	if err := f.goals.Delete(f.user.ID, goal.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.transactions.Delete(f.user.ID, row.ID); err != nil {
		t.Fatal(err)
	}
}

func TestFinancialConcurrentExpenseAndGoal(t *testing.T) {
	f := financialDB(t)
	if _, err := f.transactions.Create(f.user.ID, f.transaction(money.Max-1, models.Expense, 0)); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, err := f.transactions.Create(f.user.ID, f.transaction(1, models.Expense, 0))
		results <- err
	}()
	go func() {
		<-start
		_, err := f.goals.Create(f.user.ID, services.CreateGoalRequest{Name: "Reserve", AccountID: &f.accounts[0].ID, TargetAmount: 1})
		results <- err
	}()
	close(start)
	success, rejected := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, money.ErrRange) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("success=%d rejected=%d", success, rejected)
	}
	account, err := services.NewAccountService(repository.NewAccountRepository(f.db)).GetByID(f.user.ID, f.accounts[0].ID)
	if err != nil || account.UnallocatedAmount != -money.Max {
		t.Fatalf("available=%v %v", account, err)
	}
}

func TestFinancialBudgetAndGoalExactFields(t *testing.T) {
	f := financialDB(t)
	category := &models.Category{Name: uuid.NewString()}
	if err := f.db.Create(category).Error; err != nil {
		t.Fatal(err)
	}
	defer f.db.Delete(category)
	budgetService := services.NewBudgetService(repository.NewBudgetRepository(f.db), repository.NewCategoryRepository(f.db))
	for _, categoryID := range []uuid.UUID{f.category.ID, category.ID} {
		if _, err := budgetService.Create(f.user.ID, services.CreateBudgetRequest{CategoryID: categoryID, AllocatedAmount: money.Max}); err != nil {
			t.Fatal(err)
		}
	}
	budgets, err := budgetService.FindByUserID(f.user.ID)
	if err != nil || len(budgets) != 2 {
		t.Fatalf("budgets=%v %v", budgets, err)
	}
	total := new(big.Int)
	for _, budget := range budgets {
		total.Add(total, big.NewInt(budget.AllocatedAmount))
	}
	if total.String() != "18014398509481982" {
		t.Fatal(total)
	}
	// Unlinked goals are not account reservations. Their aggregate fields likewise
	// remain exact in the frontend without imposing an arbitrary pooled cap.
	for i := 0; i < 2; i++ {
		if _, err := f.goals.Create(f.user.ID, services.CreateGoalRequest{Name: "Large goal", TargetAmount: money.Max, SavedAmount: money.Max, MonthlyContribution: money.Max}); err != nil {
			t.Fatal(err)
		}
	}
	goals, err := f.goals.FindByUserID(f.user.ID)
	if err != nil || len(goals) != 2 || goals[0].SavedAmount != money.Max || goals[1].MonthlyContribution != money.Max {
		t.Fatalf("goals=%v %v", goals, err)
	}
	// Delete second category only after budget cleanup; fixture cleanup handles all owner rows.
	if err := f.db.Where("user_id = ? AND category_id = ?", f.user.ID, category.ID).Delete(&models.Budget{}).Error; err != nil {
		t.Fatal(err)
	}
}
