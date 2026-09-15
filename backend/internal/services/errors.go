package services

import "github.com/kacperkg/vellum/internal/apperrors"

var (
	// Re-export application categories so handlers only import services.
	ErrNotFound     = apperrors.ErrNotFound
	ErrConflict     = apperrors.ErrConflict
	ErrUnauthorized = apperrors.ErrUnauthorized
	ErrInvalidInput = apperrors.ErrInvalidInput

	ErrAccountNameRequired         = invalid("account name required")
	ErrAccountNotFound             = notFound("account not found")
	ErrAllocatedAmountPositive     = invalid("allocated amount must be positive")
	ErrBudgetNotFound              = notFound("budget not found")
	ErrCategoryNotFound            = notFound("category not found")
	ErrCategoryRequired            = invalid("category is required")
	ErrGoalContributionNegative    = invalid("goal contribution cannot be negative")
	ErrGoalNameRequired            = invalid("goal name required")
	ErrGoalNotFound                = notFound("goal not found")
	ErrGoalSavedAmountNegative     = invalid("goal saved amount cannot be negative")
	ErrGoalSavedExceedsTarget      = invalid("goal saved amount cannot exceed target amount")
	ErrGoalTargetPositive          = invalid("goal target must be positive")
	ErrInvalidCredentials          = unauthorized("invalid email or password")
	ErrSettingsCurrencyInvalid     = invalid("settings currency is invalid")
	ErrSettingsDateFormatInvalid   = invalid("settings date format is invalid")
	ErrSettingsNotFound            = notFound("user settings not found")
	ErrSettingsNumberFormatInvalid = invalid("settings number format is invalid")
	ErrSettingsWeekStartInvalid    = invalid("settings week start is invalid")
	ErrTransactionAccountRequired  = invalid("transaction account is required")
	ErrTransactionAmountPositive   = invalid("transaction amount must be positive")
	ErrTransactionDateRange        = invalid("transaction date range is invalid")
	ErrTransactionDateRequired     = invalid("transaction date is required")
	ErrTransactionFrequency        = invalid("transaction frequency is invalid")
	ErrTransactionNameRequired     = invalid("transaction name required")
	ErrTransactionNotFound         = notFound("transaction not found")
	ErrTransactionType             = invalid("transaction type is invalid")
	ErrUserEmailExists             = conflict("user with this email already exists")
)

type classifiedError struct {
	message string
	kind    error
}

func (e *classifiedError) Error() string { return e.message }
func (e *classifiedError) Unwrap() error { return e.kind }

func notFound(message string) error     { return &classifiedError{message, ErrNotFound} }
func conflict(message string) error     { return &classifiedError{message, ErrConflict} }
func unauthorized(message string) error { return &classifiedError{message, ErrUnauthorized} }
func invalid(message string) error      { return &classifiedError{message, ErrInvalidInput} }
