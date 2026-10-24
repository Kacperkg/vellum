package repository_test

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/kacperkg/vellum/internal/database"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repository"
	"github.com/kacperkg/vellum/internal/services"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Each test gets its own schema and connection pool. Committed registration
// writes and concurrent requests never touch other test or application rows.
func registrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}
	root, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	rootSQL, err := root.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rootSQL.Close() })
	schema := "registration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := root.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
			t.Errorf("remove isolated registration schema: %v", err)
		}
	})
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.RuntimeParams["search_path"] = schema
	pool := stdlib.OpenDB(*config)
	t.Cleanup(func() { pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func requireRegistrationCounts(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	for _, model := range []any{&models.User{}, &models.UserSettings{}, &models.RefreshToken{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != want {
			t.Fatalf("%T count = %d, want %d, error = %v", model, count, want, err)
		}
	}
}

func TestRegistrationRollsBackAndCanRetry(t *testing.T) {
	for _, table := range []string{"user_settings", "refresh_tokens"} {
		t.Run(table+" failure", func(t *testing.T) {
			db := registrationDB(t)
			if err := db.Exec("ALTER TABLE " + table + " ADD CONSTRAINT registration_test_failure CHECK (false)").Error; err != nil {
				t.Fatal(err)
			}
			service := services.NewUserService(repository.NewUserRepository(db), repository.NewRefreshTokenRepository(db), "test-secret", time.Hour, 24*time.Hour)
			request := services.RegisterRequest{FirstName: "Rollback", LastName: "Test", Email: "rollback@example.test", Password: "registration-password"}
			response, err := service.Register(request)
			if err == nil || response != nil {
				t.Fatalf("failure returned credentials = %v, error = %v", response != nil, err)
			}
			requireRegistrationCounts(t, db, 0)
			if err := db.Exec("ALTER TABLE " + table + " DROP CONSTRAINT registration_test_failure").Error; err != nil {
				t.Fatal(err)
			}
			response, err = service.Register(request)
			if err != nil || response == nil || response.User.ID == uuid.Nil || response.User.CreatedAt.IsZero() || response.AccessToken == "" || response.RefreshToken == "" {
				t.Fatalf("retry succeeded = %v, error = %v", response != nil, err)
			}
			requireRegistrationCounts(t, db, 1)
			if _, err := service.Refresh(services.RefreshRequest{RefreshToken: response.RefreshToken}); err != nil {
				t.Fatalf("committed session cannot refresh: %v", err)
			}
		})
	}
}

type synchronizedRegistrationRepository struct {
	repository.UserRepository
	reads *sync.WaitGroup
}

func (r synchronizedRegistrationRepository) FindByEmail(email string) (*models.User, error) {
	user, err := r.UserRepository.FindByEmail(email)
	r.reads.Done()
	r.reads.Wait()
	return user, err
}

func TestConcurrentRegistrationEmailConflict(t *testing.T) {
	db := registrationDB(t)
	var reads sync.WaitGroup
	reads.Add(2)
	users := synchronizedRegistrationRepository{UserRepository: repository.NewUserRepository(db), reads: &reads}
	service := services.NewUserService(users, repository.NewRefreshTokenRepository(db), "test-secret", time.Hour, 24*time.Hour)
	request := services.RegisterRequest{FirstName: "Concurrent", LastName: "Test", Email: "concurrent@example.test", Password: "registration-password"}
	results := make(chan error, 2)
	for range 2 {
		go func() {
			response, err := service.Register(request)
			if err != nil && response != nil {
				t.Error("failed registration returned credentials")
			}
			results <- err
		}()
	}
	var successful, conflicts int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			successful++
		case errors.Is(err, services.ErrUserEmailExists):
			conflicts++
		default:
			t.Fatalf("concurrent registration: %v", err)
		}
	}
	if successful != 1 || conflicts != 1 {
		t.Fatalf("successful = %d, conflicts = %d", successful, conflicts)
	}
	requireRegistrationCounts(t, db, 1)
}
