package repository_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repository"
	"github.com/kacperkg/vellum/internal/services"
)

// Both service pre-checks finish before either insertion. The unique index,
// rather than the friendly pre-check, must arbitrate the competing requests.
type concurrentBudgetRepository struct {
	repository.BudgetRepository
	ready   chan<- struct{}
	release <-chan struct{}
}

func (r concurrentBudgetRepository) FindByUserID(userID uuid.UUID) ([]*models.Budget, error) {
	budgets, err := r.BudgetRepository.FindByUserID(userID)
	r.ready <- struct{}{}
	<-r.release
	return budgets, err
}

type staleBudgetListRepository struct{ repository.BudgetRepository }

func (r staleBudgetListRepository) FindByUserID(uuid.UUID) ([]*models.Budget, error) {
	return nil, nil
}

func TestBudgetUniqueCategoryConcurrentCreatesAndUpdates(t *testing.T) {
	db := integrationDB(t)
	// These fixtures are committed in the isolated test database so separate
	// pooled connections can exercise real unique-index contention.
	users := []*models.User{
		{FirstName: "Budget", LastName: "A", Email: uuid.NewString() + "@example.test", PasswordHash: "test-only-hash"},
		{FirstName: "Budget", LastName: "B", Email: uuid.NewString() + "@example.test", PasswordHash: "test-only-hash"},
	}
	categories := []*models.Category{{Name: "budget-a-" + uuid.NewString()}, {Name: "budget-b-" + uuid.NewString()}}
	if err := db.Create(users).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		userIDs := []uuid.UUID{users[0].ID, users[1].ID}
		if err := db.Where("user_id IN ?", userIDs).Delete(&models.Budget{}).Error; err != nil {
			t.Error(err)
		}
		if err := db.Where("id IN ?", userIDs).Delete(&models.User{}).Error; err != nil {
			t.Error(err)
		}
		if err := db.Where("name IN ?", []string{categories[0].Name, categories[1].Name}).Delete(&models.Category{}).Error; err != nil {
			t.Error(err)
		}
	})
	if err := db.Create(categories).Error; err != nil {
		t.Fatal(err)
	}
	repo := repository.NewBudgetRepository(db)
	ready, release := make(chan struct{}, 2), make(chan struct{})
	service := services.NewBudgetService(concurrentBudgetRepository{repo, ready, release}, repository.NewCategoryRepository(db))
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := service.Create(users[0].ID, services.CreateBudgetRequest{CategoryID: categories[0].ID, AllocatedAmount: 120000})
			results <- err
		}()
	}
	for range 2 {
		select {
		case <-ready:
		case <-time.After(10 * time.Second):
			close(release)
			t.Fatal("concurrent budget checks did not finish")
		}
	}
	close(release)
	successes, conflicts := 0, 0
	for range 2 {
		select {
		case err := <-results:
			if err == nil {
				successes++
			} else if errors.Is(err, services.ErrBudgetCategoryExists) && errors.Is(err, services.ErrConflict) {
				conflicts++
			} else {
				t.Fatalf("unexpected concurrent create error: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("concurrent budget writes did not finish")
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d, want one of each", successes, conflicts)
	}
	budgets, err := repo.FindByUserID(users[0].ID)
	if err != nil || len(budgets) != 1 {
		t.Fatalf("stored budgets=%#v error=%v", budgets, err)
	}
	plainService := services.NewBudgetService(repo, repository.NewCategoryRepository(db))
	if _, err := plainService.Create(users[1].ID, services.CreateBudgetRequest{CategoryID: categories[0].ID, AllocatedAmount: 75000}); err != nil {
		t.Fatalf("different user could not use same category: %v", err)
	}
	other, err := plainService.Create(users[0].ID, services.CreateBudgetRequest{CategoryID: categories[1].ID, AllocatedAmount: 50000})
	if err != nil {
		t.Fatal(err)
	}
	// Model a request whose duplicate pre-check became stale before its update.
	staleService := services.NewBudgetService(staleBudgetListRepository{repo}, repository.NewCategoryRepository(db))
	if _, err := staleService.Update(users[0].ID, other.ID, services.UpdateBudgetRequest{CategoryID: categories[0].ID, AllocatedAmount: 99999}); !errors.Is(err, services.ErrBudgetCategoryExists) {
		t.Fatalf("update collision error=%v", err)
	}
	unchanged, err := repo.FindByID(other.ID)
	if err != nil || unchanged.CategoryID != categories[1].ID || unchanged.AllocatedAmount != 50000 {
		t.Fatalf("failed collision changed budget=%#v error=%v", unchanged, err)
	}
	if _, err := plainService.Update(users[0].ID, other.ID, services.UpdateBudgetRequest{CategoryID: categories[1].ID, AllocatedAmount: 60000}); err != nil {
		t.Fatalf("updating own unchanged category failed: %v", err)
	}
}
