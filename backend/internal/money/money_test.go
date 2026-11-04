package money_test

import (
	"errors"
	"math"
	"testing"

	"github.com/kacperkg/vellum/internal/apperrors"
	"github.com/kacperkg/vellum/internal/money"
)

func TestMoneyBoundaries(t *testing.T) {
	for _, value := range []int64{-money.Max, 0, 1, money.Max} {
		if !money.Valid(value) {
			t.Fatalf("supported %d", value)
		}
	}
	for _, value := range []int64{math.MinInt64, -money.Max - 1, money.Max + 1, math.MaxInt64} {
		if money.Valid(value) {
			t.Fatalf("unsupported %d", value)
		}
	}
	for _, value := range []string{"9007199254740992", "18446744073709551614", "-18446744073709551616"} {
		if _, err := money.Parse(value); !errors.Is(err, apperrors.ErrInvalidInput) {
			t.Fatalf("parse %s: %v", value, err)
		}
	}
	for _, pair := range [][2]int64{{-money.Max, 1}, {money.Max, -1}, {math.MinInt64, 2}} {
		if _, err := money.Subtract(pair[0], pair[1]); !errors.Is(err, money.ErrRange) {
			t.Fatalf("subtract %v: %v", pair, err)
		}
	}
	if got, err := money.Subtract(money.Max, money.Max); err != nil || got != 0 {
		t.Fatalf("cancellation=%d %v", got, err)
	}
}
