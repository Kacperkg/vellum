export type Currency = "GBP" | "USD" | "EUR";

export type DateFormat = "DD/MM/YYYY" | "MM/DD/YYYY" | "YYYY-MM-DD";

export type NumberFormat = "comma_decimal" | "dot_decimal" | "apostrophe";

export type WeekStartsOn = "Monday" | "Sunday";

export interface UserSettings {
  id: string;
  currency: Currency;
  date_format: DateFormat;
  number_format: NumberFormat;
  week_starts_on: WeekStartsOn;
}

export interface UpdateUserSettingsRequest {
  currency: Currency;
  date_format: DateFormat;
  number_format: NumberFormat;
  week_starts_on: WeekStartsOn;
}
