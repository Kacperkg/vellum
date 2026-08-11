package services

import "errors"

var (
	ErrAccountNameRequired      = errors.New("account name required")
	ErrAccountNotFound          = errors.New("account not found")
	ErrAllocatedAmountPositive  = errors.New("allocated amount must be positive")
	ErrBudgetNotFound           = errors.New("budget not found")
	ErrCategoryNotFound         = errors.New("category not found")
	ErrCategoryRequired         = errors.New("category is required")
	ErrGoalContributionNegative = errors.New("goal contribution cannot be negative")
	ErrGoalNameRequired         = errors.New("goal name required")
	ErrGoalNotFound             = errors.New("goal not found")
	ErrGoalSavedAmountNegative  = errors.New("goal saved amount cannot be negative")
	ErrGoalSavedExceedsTarget   = errors.New("goal saved amount cannot exceed target amount")
	ErrGoalTargetPositive       = errors.New("goal target must be positive")
	ErrInvalidCredentials       = errors.New("invalid email or password")
	ErrUserEmailExists          = errors.New("user with this email already exists")
)
