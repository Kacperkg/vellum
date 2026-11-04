// Package money defines the integer minor-unit boundary shared with JSON clients.
package money

import (
	"fmt"
	"math/big"
	"strconv"

	"github.com/kacperkg/vellum/internal/apperrors"
)

// Max is JavaScript's largest exactly representable integer (2^53 - 1).
// GBP, USD and EUR all use two decimal places; no rounding occurs in the API.
const Max int64 = 9007199254740991

var ErrRange = fmt.Errorf("money must remain within +/-9007199254740991 minor units: %w", apperrors.ErrInvalidInput)

func Valid(value int64) bool { return value >= -Max && value <= Max }

func Parse(value string) (int64, error) {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || !Valid(n) {
		return 0, ErrRange
	}
	return n, nil
}

func Subtract(a, b int64) (int64, error) {
	if !Valid(a) || !Valid(b) {
		return 0, ErrRange
	}
	result := a - b // two supported operands cannot overflow int64
	if !Valid(result) {
		return 0, ErrRange
	}
	return result, nil
}

// Excess compares arbitrary-precision PostgreSQL aggregates without narrowing.
func Excess(value string) (*big.Int, error) {
	n, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return nil, fmt.Errorf("invalid monetary aggregate")
	}
	n.Abs(n).Sub(n, big.NewInt(Max))
	if n.Sign() < 0 {
		n.SetInt64(0)
	}
	return n, nil
}
