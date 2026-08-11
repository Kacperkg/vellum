package models

type Frequency string

const (
	OneOff  Frequency = "one-off"
	Daily   Frequency = "daily"
	Weekly  Frequency = "weekly"
	Monthly Frequency = "monthly"
	Yearly  Frequency = "yearly"
)

type TransactionType string

const (
	Income  TransactionType = "income"
	Expense TransactionType = "expense"
)

type NumberFormat string

const (
	CommaDecimal NumberFormat = "comma_decimal" // 1,234.56
	DotDecimal   NumberFormat = "dot_decimal"   // 1.234,56
	Apostrophe   NumberFormat = "apostrophe"    // 1'234.56
)

type Currency string

const (
	GBP Currency = "GBP"
	USD Currency = "USD"
	EUR Currency = "EUR"
)

type DateFormat string

const (
	DDMMYYYY DateFormat = "DD/MM/YYYY"
	MMDDYYYY DateFormat = "MM/DD/YYYY"
	YYYYMMDD DateFormat = "YYYY-MM-DD"
)

type WeekStartsOn string

const (
	Monday WeekStartsOn = "Monday"
	Sunday WeekStartsOn = "Sunday"
)
