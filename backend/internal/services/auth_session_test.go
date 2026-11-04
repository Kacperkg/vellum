package services_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/services"
)

func TestSessionValidationFailsClosed(t *testing.T) {
	id, family := uuid.New(), uuid.New()
	users := &fakeUserRepository{User: &models.User{BaseModel: models.BaseModel{ID: id}, AuthVersion: 4}}
	tokens := &fakeRefreshTokenRepository{Token: &models.RefreshToken{UserID: id, AuthVersion: 4, FamilyID: &family, ExpiresAt: time.Now().Add(time.Hour)}}
	service := services.NewUserService(users, tokens, "test-secret", time.Hour, time.Hour)
	if valid, err := service.ValidateSession(context.Background(), id, 4, &family); !valid || err != nil {
		t.Fatal("live family was rejected")
	}
	tokens.Err = errors.New("synthetic session query failure")
	if valid, err := service.ValidateSession(context.Background(), id, 4, &family); valid || !errors.Is(err, tokens.Err) {
		t.Fatal("session lookup failure authenticated a JWT")
	}
	tokens.Err = nil
	users.Err = errors.New("synthetic user query failure")
	for _, claim := range []*uuid.UUID{&family, nil} {
		if valid, err := service.ValidateSession(context.Background(), id, 4, claim); valid || !errors.Is(err, users.Err) {
			t.Fatal("user lookup failure authenticated a JWT")
		}
	}
	users.Err = nil
	zero, other := uuid.Nil, uuid.New()
	for _, invalidFamily := range []*uuid.UUID{&zero, &other} {
		if valid, err := service.ValidateSession(context.Background(), id, 4, invalidFamily); valid || err != nil {
			t.Fatal("invalid family authenticated a JWT")
		}
	}
	if valid, err := service.ValidateSession(context.Background(), id, 3, &family); valid || err != nil {
		t.Fatal("old authentication version authenticated a JWT")
	}
}
