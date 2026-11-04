package repository_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/auth"
	"github.com/kacperkg/vellum/internal/database"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repository"
	"github.com/kacperkg/vellum/internal/services"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func resetIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := integrationDB(t)
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(12)
	pool.SetMaxIdleConns(2)
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Errorf("close reset test database pool: %v", err)
		}
	})
	return db
}

// These tests use committed rows and separate connections: wrapping a concurrent
// test in one outer transaction would hide PostgreSQL's real row-lock behavior.
func resetTestUser(t *testing.T, db *gorm.DB) *models.User {
	t.Helper()
	user := &models.User{FirstName: "Reset", LastName: "Test", Email: uuid.NewString() + "@example.com", PasswordHash: "old-password-hash"}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Where("user_id = ?", user.ID).Delete(&models.RefreshToken{})
		db.Where("user_id = ?", user.ID).Delete(&models.PasswordResetToken{})
		db.Delete(user)
	})
	return user
}

func resetTestHash() string { return strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "") }

func issueReset(t *testing.T, db *gorm.DB, user *models.User) string {
	t.Helper()
	hash := resetTestHash()
	if _, _, err := repository.NewPasswordResetRepository(db).Issue(context.Background(), user.Email, hash, 20*time.Minute, 0); err != nil {
		t.Fatal(err)
	}
	return hash
}

func assertResetState(t *testing.T, db *gorm.DB, userID uuid.UUID, wantVersion int64, wantHash string) {
	t.Helper()
	var user models.User
	if err := db.First(&user, "id = ?", userID).Error; err != nil {
		t.Fatal(err)
	}
	if user.AuthVersion != wantVersion || user.PasswordHash != wantHash {
		t.Fatalf("reset state version=%d hashMatches=%v; want version=%d", user.AuthVersion, user.PasswordHash == wantHash, wantVersion)
	}
	var active int64
	if err := db.Model(&models.RefreshToken{}).Where("user_id = ? AND revoked_at IS NULL", userID).Count(&active).Error; err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatalf("%d refresh sessions escaped password reset", active)
	}
}

func TestPasswordResetAtomicSingleUseAndOutstandingTokens(t *testing.T) {
	db := resetIntegrationDB(t)
	user := resetTestUser(t, db)
	first := issueReset(t, db, user)
	second := issueReset(t, db, user)
	session := &models.RefreshToken{UserID: user.ID, TokenHash: resetTestHash(), ExpiresAt: time.Now().Add(time.Hour)}
	if err := repository.NewRefreshTokenRepository(db).Create(session); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewPasswordResetRepository(db)
	const count = 8
	start := make(chan struct{})
	results := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- repo.Consume(context.Background(), second, "new-password-hash")
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("unexpected redemption error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful concurrent redemptions=%d; want 1", successes)
	}
	if err := repo.Consume(context.Background(), first, "another-hash"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("other outstanding token remains usable: %v", err)
	}
	assertResetState(t, db, user.ID, 1, "new-password-hash")
}

func TestPasswordResetRejectsInvalidExpiredAndConsumed(t *testing.T) {
	db := resetIntegrationDB(t)
	user := resetTestUser(t, db)
	repo := repository.NewPasswordResetRepository(db)
	if err := repo.Consume(context.Background(), resetTestHash(), "new"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unknown token: %v", err)
	}
	hash := issueReset(t, db, user)
	if err := db.Model(&models.PasswordResetToken{}).Where("token_hash = ?", hash).Update("expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.Consume(context.Background(), hash, "new"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expired token: %v", err)
	}
	valid := issueReset(t, db, user)
	if err := repo.Consume(context.Background(), valid, "new"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Consume(context.Background(), valid, "different"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("consumed token: %v", err)
	}
	assertResetState(t, db, user.ID, 1, "new")
}

func TestPasswordResetDatabaseCooldownAndDuplicateHashes(t *testing.T) {
	db := resetIntegrationDB(t)
	user := resetTestUser(t, db)
	repo := repository.NewPasswordResetRepository(db)
	hash := resetTestHash()
	if _, _, err := repo.Issue(context.Background(), user.Email, hash, 20*time.Minute, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.Issue(context.Background(), user.Email, resetTestHash(), 20*time.Minute, time.Minute); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cooldown did not suppress repeated request: %v", err)
	}
	other := resetTestUser(t, db)
	if _, _, err := repo.Issue(context.Background(), other.Email, hash, 20*time.Minute, 0); err == nil {
		t.Fatal("duplicate reset hash accepted")
	}
	if _, _, err := repo.Issue(context.Background(), uuid.NewString()+"@example.com", resetTestHash(), 20*time.Minute, 0); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unknown email: %v", err)
	}
}

func TestPasswordResetPreventsStaleLoginAndRefresh(t *testing.T) {
	db := resetIntegrationDB(t)
	user := resetTestUser(t, db)
	refreshRepo := repository.NewRefreshTokenRepository(db)
	current := &models.RefreshToken{UserID: user.ID, TokenHash: resetTestHash(), ExpiresAt: time.Now().Add(time.Hour)}
	if err := refreshRepo.Create(current); err != nil {
		t.Fatal(err)
	}
	hash := issueReset(t, db, user)
	if err := repository.NewPasswordResetRepository(db).Consume(context.Background(), hash, "new-password-hash"); err != nil {
		t.Fatal(err)
	}
	// A login may already have compared the old hash before reset commits.
	staleLogin := &models.RefreshToken{UserID: user.ID, TokenHash: resetTestHash(), ExpiresAt: time.Now().Add(time.Hour), AuthVersion: 0, PasswordHashSnapshot: user.PasswordHash}
	if err := refreshRepo.Create(staleLogin); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("stale login committed after reset: %v", err)
	}
	replacement := &models.RefreshToken{UserID: user.ID, TokenHash: resetTestHash(), ExpiresAt: time.Now().Add(time.Hour), AuthVersion: 0}
	if err := refreshRepo.Rotate(current.ID, replacement, time.Now()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("stale refresh committed after reset: %v", err)
	}
	assertResetState(t, db, user.ID, 1, "new-password-hash")
}

func TestPasswordResetRacesLoginRefreshAndLogout(t *testing.T) {
	db := resetIntegrationDB(t)
	for _, operation := range []string{"login", "refresh", "logout"} {
		t.Run(operation, func(t *testing.T) {
			user := resetTestUser(t, db)
			refreshRepo := repository.NewRefreshTokenRepository(db)
			current := &models.RefreshToken{UserID: user.ID, TokenHash: resetTestHash(), ExpiresAt: time.Now().Add(time.Hour)}
			if err := refreshRepo.Create(current); err != nil {
				t.Fatal(err)
			}
			hash := issueReset(t, db, user)
			start := make(chan struct{})
			resetResult := make(chan error, 1)
			sessionResult := make(chan error, 1)
			go func() {
				<-start
				resetResult <- repository.NewPasswordResetRepository(db).Consume(context.Background(), hash, "new-password-hash")
			}()
			go func() {
				<-start
				switch operation {
				case "login":
					sessionResult <- refreshRepo.Create(&models.RefreshToken{UserID: user.ID, TokenHash: resetTestHash(), ExpiresAt: time.Now().Add(time.Hour), AuthVersion: 0, PasswordHashSnapshot: user.PasswordHash})
				case "refresh":
					sessionResult <- refreshRepo.Rotate(current.ID, &models.RefreshToken{UserID: user.ID, TokenHash: resetTestHash(), ExpiresAt: time.Now().Add(time.Hour), AuthVersion: 0}, time.Now())
				case "logout":
					sessionResult <- refreshRepo.RevokeByHash(current.TokenHash, time.Now())
				}
			}()
			close(start)
			if err := <-resetResult; err != nil {
				t.Fatalf("reset during %s: %v", operation, err)
			}
			if err := <-sessionResult; err != nil && !errors.Is(err, repository.ErrNotFound) {
				t.Fatalf("%s race: %v", operation, err)
			}
			assertResetState(t, db, user.ID, 1, "new-password-hash")
		})
	}
}

func TestPasswordResetTransactionRollback(t *testing.T) {
	db := resetIntegrationDB(t)
	user := resetTestUser(t, db)
	hash := issueReset(t, db, user)
	current := &models.RefreshToken{UserID: user.ID, TokenHash: resetTestHash(), ExpiresAt: time.Now().Add(time.Hour)}
	if err := repository.NewRefreshTokenRepository(db).Create(current); err != nil {
		t.Fatal(err)
	}
	// A PostgreSQL trigger fails the last part of the operation. Earlier user and
	// reset-token writes must roll back rather than leave a partially reset account.
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	fn := "reset_failure_" + suffix
	trigger := "reset_failure_" + suffix
	statement := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.user_id = '%s'::uuid THEN RAISE EXCEPTION 'synthetic reset rollback'; END IF; RETURN NEW; END $$`, fn, user.ID)
	if err := db.Exec(statement).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec("DROP TRIGGER IF EXISTS " + trigger + " ON password_reset_tokens")
		db.Exec("DROP FUNCTION IF EXISTS " + fn + "()")
	})
	if err := db.Exec("CREATE TRIGGER " + trigger + " BEFORE UPDATE ON password_reset_tokens FOR EACH ROW EXECUTE FUNCTION " + fn + "()").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.NewPasswordResetRepository(db).Consume(context.Background(), hash, "new-password-hash"); err == nil {
		t.Fatal("injected failure unexpectedly succeeded")
	}
	var actual models.User
	if err := db.First(&actual, "id = ?", user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if actual.AuthVersion != 0 || actual.PasswordHash != user.PasswordHash {
		t.Fatal("user update escaped transaction rollback")
	}
	var token models.PasswordResetToken
	if err := db.First(&token, "token_hash = ?", hash).Error; err != nil {
		t.Fatal(err)
	}
	if token.ConsumedAt != nil {
		t.Fatal("token consumption escaped transaction rollback")
	}
	var refreshed models.RefreshToken
	if err := db.First(&refreshed, "id = ?", current.ID).Error; err != nil {
		t.Fatal(err)
	}
	if refreshed.RevokedAt != nil {
		t.Fatal("session revocation escaped transaction rollback")
	}
	if err := db.Exec("DROP TRIGGER " + trigger + " ON password_reset_tokens").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.NewPasswordResetRepository(db).Consume(context.Background(), hash, "new-password-hash"); err != nil {
		t.Fatalf("token cannot be retried after rollback: %v", err)
	}
	assertResetState(t, db, user.ID, 1, "new-password-hash")
}

func TestPasswordResetMigrationPreservesLegacyUsers(t *testing.T) {
	db := resetIntegrationDB(t)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })
	schema := "reset_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	for _, sql := range []string{
		"CREATE SCHEMA " + schema,
		"SET LOCAL search_path TO " + schema,
		`CREATE TABLE users (id uuid PRIMARY KEY, created_at timestamptz, updated_at timestamptz, first_name text NOT NULL, last_name text NOT NULL, email text NOT NULL, password_hash text NOT NULL)`,
		`CREATE UNIQUE INDEX idx_users_email ON users(email)`,
		`CREATE TABLE refresh_tokens (id uuid PRIMARY KEY, created_at timestamptz, updated_at timestamptz, user_id uuid NOT NULL, token_hash varchar(64) NOT NULL, expires_at timestamptz NOT NULL, revoked_at timestamptz)`,
	} {
		if err := tx.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	userID, refreshID := uuid.New(), uuid.New()
	if err := tx.Exec("INSERT INTO users(id,first_name,last_name,email,password_hash) VALUES(?,?,?,?,?)", userID, "Legacy", "User", "legacy@example.com", "legacy-hash").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("INSERT INTO refresh_tokens(id,user_id,token_hash,expires_at) VALUES(?,?,?,?)", refreshID, userID, resetTestHash(), time.Now().Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(tx); err != nil {
		t.Fatal(err)
	}
	var user models.User
	if err := tx.First(&user, "id = ?", userID).Error; err != nil {
		t.Fatal(err)
	}
	if user.AuthVersion != 0 || user.PasswordHash != "legacy-hash" {
		t.Fatal("migration changed legacy account authentication")
	}
	var refresh models.RefreshToken
	if err := tx.First(&refresh, "id = ?", refreshID).Error; err != nil {
		t.Fatal(err)
	}
	if refresh.AuthVersion != 0 || refresh.RevokedAt != nil || refresh.FamilyID != nil {
		t.Fatal("migration invalidated legacy refresh session unexpectedly")
	}
	if !tx.Migrator().HasColumn(&models.RefreshToken{}, "family_id") {
		t.Fatal("session family column was not migrated")
	}
	if !tx.Migrator().HasTable(&models.PasswordResetToken{}) {
		t.Fatal("reset-token table was not migrated")
	}
}

func TestPasswordResetExpiryRecheckedAfterWaitingForUserLock(t *testing.T) {
	db := resetIntegrationDB(t)
	user := resetTestUser(t, db)
	hash := issueReset(t, db, user)
	holder := db.Begin()
	if holder.Error != nil {
		t.Fatal(holder.Error)
	}
	defer holder.Rollback()
	if err := holder.Exec("SELECT id FROM users WHERE id = ? FOR UPDATE", user.ID).Error; err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		result <- repository.NewPasswordResetRepository(db).Consume(context.Background(), hash, "new-hash")
	}()
	deadline := time.Now().Add(5 * time.Second)
	blocked := false
	for time.Now().Before(deadline) {
		var count int64
		if err := db.Raw("SELECT COUNT(*) FROM pg_stat_activity WHERE datname = current_database() AND pid <> pg_backend_pid() AND wait_event_type = 'Lock' AND query ILIKE '%users%'").Scan(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			blocked = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		holder.Rollback()
		<-result
		t.Fatal("redemption did not wait for the user row lock")
	}
	if err := holder.Model(&models.PasswordResetToken{}).Where("token_hash = ?", hash).Update("expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if err := holder.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("token expired during lock wait accepted: %v", err)
	}
	var actual models.User
	if err := db.First(&actual, "id = ?", user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if actual.AuthVersion != 0 || actual.PasswordHash != user.PasswordHash {
		t.Fatal("expired reset changed account")
	}
}

func TestPasswordResetForeignKeyAndCleanup(t *testing.T) {
	db := resetIntegrationDB(t)
	orphan := &models.PasswordResetToken{UserID: uuid.New(), TokenHash: resetTestHash(), ExpiresAt: time.Now().Add(time.Minute)}
	if err := db.Create(orphan).Error; err == nil {
		t.Fatal("reset token with missing user accepted")
	}
	user := resetTestUser(t, db)
	hash := issueReset(t, db, user)
	// Cleanup is limited to expired records; it must preserve a valid link.
	if err := repository.NewPasswordResetRepository(db).Cleanup(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	var active int64
	if err := db.Model(&models.PasswordResetToken{}).Where("token_hash = ?", hash).Count(&active).Error; err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatal("cleanup removed active reset link")
	}
	if err := db.Model(&models.PasswordResetToken{}).Where("token_hash = ?", hash).Update("expires_at", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.NewPasswordResetRepository(db).Cleanup(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.PasswordResetToken{}).Where("token_hash = ?", hash).Count(&active).Error; err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatal("cleanup retained expired reset record")
	}
}

func TestPasswordResetPostgreSQLSessionLifecycle(t *testing.T) {
	db := resetIntegrationDB(t)
	user := resetTestUser(t, db)
	oldHash, err := bcrypt.GenerateFromPassword([]byte("old-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(user).Update("password_hash", string(oldHash)).Error; err != nil {
		t.Fatal(err)
	}
	const secret = "isolated-test-jwt-secret"
	service := services.NewUserService(repository.NewUserRepository(db), repository.NewRefreshTokenRepository(db), secret, time.Minute, time.Hour)
	oldSession, err := service.Login(services.LoginRequest{Email: user.Email, Password: "old-password"})
	if err != nil {
		t.Fatal(err)
	}
	oldClaims, err := auth.ValidateToken(oldSession.AccessToken, secret)
	if err != nil || !service.ValidateAuthVersion(context.Background(), user.ID, oldClaims.AuthVersion) {
		t.Fatalf("initial access session invalid: %v", err)
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte("new-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	hash := issueReset(t, db, user)
	if err := repository.NewPasswordResetRepository(db).Consume(context.Background(), hash, string(newHash)); err != nil {
		t.Fatal(err)
	}
	if service.ValidateAuthVersion(context.Background(), user.ID, oldClaims.AuthVersion) {
		t.Fatal("old access JWT remains authorized after reset")
	}
	if _, err := service.Refresh(services.RefreshRequest{RefreshToken: oldSession.RefreshToken}); !errors.Is(err, services.ErrInvalidRefreshToken) {
		t.Fatalf("old refresh session accepted: %v", err)
	}
	if _, err := service.Login(services.LoginRequest{Email: user.Email, Password: "old-password"}); !errors.Is(err, services.ErrInvalidCredentials) {
		t.Fatalf("old password still accepted: %v", err)
	}
	newSession, err := service.Login(services.LoginRequest{Email: user.Email, Password: "new-password"})
	if err != nil {
		t.Fatalf("legitimate login after reset failed: %v", err)
	}
	newClaims, err := auth.ValidateToken(newSession.AccessToken, secret)
	if err != nil || newClaims.AuthVersion != 1 || !service.ValidateAuthVersion(context.Background(), user.ID, newClaims.AuthVersion) {
		t.Fatalf("new access session invalid: %v", err)
	}
	rotated, err := service.Refresh(services.RefreshRequest{RefreshToken: newSession.RefreshToken})
	if err != nil || rotated == nil {
		t.Fatalf("new refresh session cannot rotate: %v", err)
	}
}

func TestPasswordResetConcurrentIssuanceRespectsDatabaseCooldown(t *testing.T) {
	db := resetIntegrationDB(t)
	user := resetTestUser(t, db)
	start := make(chan struct{})
	results := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			// Separate repository instances model independent application workers.
			_, _, err := repository.NewPasswordResetRepository(db).Issue(context.Background(), user.Email, resetTestHash(), 20*time.Minute, time.Minute)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("issuance: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent deliveries admitted=%d; want 1", successes)
	}
	var count int64
	if err := db.Model(&models.PasswordResetToken{}).Where("user_id = ?", user.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("persisted tokens=%d; want 1", count)
	}
}
