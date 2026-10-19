import type {
  DateFormat,
  NumberFormat,
  Transaction,
  UserSettings,
} from "../api/types";

function localeFor(numberFormat: NumberFormat): string {
  if (numberFormat === "dot_decimal") return "de-DE";
  if (numberFormat === "apostrophe") return "de-CH";
  return "en-US";
}

export function majorToMinor(amount: number): number {
  return Math.round(amount * 100);
}

export function minorToMajor(amount: number): number {
  return amount / 100;
}

export function formatMoney(
  amount: number,
  settings: UserSettings,
  maximumFractionDigits = 2,
): string {
  return new Intl.NumberFormat(localeFor(settings.number_format), {
    style: "currency",
    currency: settings.currency,
    minimumFractionDigits: maximumFractionDigits,
    maximumFractionDigits,
  }).format(minorToMajor(amount));
}

export function formatSignedMoney(
  amount: number,
  type: Transaction["type"],
  settings: UserSettings,
): string {
  const sign = type === "income" ? "+" : "−";
  return `${sign}${formatMoney(Math.abs(amount), settings)}`;
}

export function formatDate(value: string, format: DateFormat): string {
  const date = new Date(value);
  if (format === "YYYY-MM-DD") return date.toISOString().slice(0, 10);
  const locale = format === "DD/MM/YYYY" ? "en-GB" : "en-US";
  return new Intl.DateTimeFormat(locale, {
    month: "short",
    day: "numeric",
    year: "numeric",
  }).format(date);
}

export function monthKey(value: Date | string): string {
  const date = typeof value === "string" ? new Date(value) : value;
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}`;
}

export function isCurrentMonth(value: string, now = new Date()): boolean {
  return monthKey(value) === monthKey(now);
}

export function netAmount(transactions: Transaction[]): number {
  return transactions.reduce(
    (total, transaction) =>
      total +
      (transaction.type === "income"
        ? transaction.amount
        : -transaction.amount),
    0,
  );
}
