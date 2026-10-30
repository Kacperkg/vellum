import type {
  DateFormat,
  NumberFormat,
  Transaction,
  UserSettings,
} from "@/api/types";

// JSON numbers must remain exact in every supported browser. The API applies
// the same bound to persisted fields and calculated account summaries.
export const MAX_MONEY_MINOR = Number.MAX_SAFE_INTEGER;
export const MAX_MONEY_MAJOR = "90071992547409.91";
export const MONEY_INPUT_ERROR =
  "Enter an amount from 0.01 to 90071992547409.91 with at most two decimal places.";

function localeFor(numberFormat: NumberFormat): string {
  if (numberFormat === "dot_decimal") return "de-DE";
  if (numberFormat === "apostrophe") return "de-CH";
  return "en-US";
}

export function moneyInteger(amount: number | bigint): bigint {
  if (typeof amount === "bigint") return amount;
  if (!Number.isSafeInteger(amount))
    throw new RangeError(
      "The monetary value exceeds the supported exact range.",
    );
  return BigInt(amount);
}

// Parse the input text before any Number conversion. Extra decimal places are
// rejected rather than rounded, including at the upper representation boundary.
export function majorToMinor(amount: string, allowZero = false): number {
  if (!/^(?:\d+(?:\.\d{1,2})?|\.\d{1,2})$/.test(amount))
    throw new RangeError(MONEY_INPUT_ERROR);
  const [whole, fraction = ""] = amount.split(".");
  const minor = BigInt(whole || "0") * 100n + BigInt(fraction.padEnd(2, "0"));
  if (minor < (allowZero ? 0n : 1n) || minor > BigInt(MAX_MONEY_MINOR)) {
    throw new RangeError(
      allowZero
        ? "Enter an amount from 0 to 90071992547409.91 with at most two decimal places."
        : MONEY_INPUT_ERROR,
    );
  }
  return Number(minor);
}

export function minorToMajor(amount: number | bigint): string {
  const minor = moneyInteger(amount);
  const absolute = minor < 0n ? -minor : minor;
  return `${minor < 0n ? "-" : ""}${absolute / 100n}.${String(absolute % 100n).padStart(2, "0")}`;
}

// Derived totals use BigInt, including cancellation across many accepted rows.
// Only chart proportions cross into floating point; monetary labels stay exact.
export function moneyRatio(
  numerator: number | bigint,
  denominator: number | bigint,
): number {
  const bottom = moneyInteger(denominator);
  return bottom === 0n ? 0 : Number(moneyInteger(numerator)) / Number(bottom);
}

export function maxMoney(...amounts: bigint[]): bigint {
  return amounts.reduce(
    (maximum, amount) => (amount > maximum ? amount : maximum),
    amounts[0],
  );
}

export function compareMoney(first: bigint, second: bigint): number {
  return first < second ? -1 : first > second ? 1 : 0;
}

export function formatMoney(
  amount: number | bigint,
  settings: UserSettings,
  maximumFractionDigits: 0 | 2 = 2,
): string {
  const minor = moneyInteger(amount);
  const absolute = minor < 0n ? -minor : minor;
  const whole =
    maximumFractionDigits === 0 ? (absolute + 50n) / 100n : absolute / 100n;
  const formatter = new Intl.NumberFormat(localeFor(settings.number_format), {
    style: "currency",
    currency: settings.currency,
    minimumFractionDigits: maximumFractionDigits,
    maximumFractionDigits,
  });
  // formatToParts(BigInt) preserves all grouping/currency conventions without
  // dividing a large integer into an inexact binary fractional Number.
  const signedWhole = minor < 0n ? -whole : whole;
  let parts = formatter.formatToParts(signedWhole);
  if (minor < 0n && whole === 0n) parts = formatter.formatToParts(-0);
  return parts
    .map((part) =>
      part.type === "fraction"
        ? String(absolute % 100n).padStart(2, "0")
        : part.value,
    )
    .join("");
}

export function formatSignedMoney(
  amount: number,
  type: Transaction["type"],
  settings: UserSettings,
): string {
  return `${type === "income" ? "+" : "−"}${formatMoney(Math.abs(amount), settings)}`;
}

// Ledger dates are financial calendar days, not instants. RFC3339 values keep
// their written day; existing database timestamps expose their persisted UTC day.
export function calendarDate(value: string): string {
  const match = value.match(
    /^(\d{4}-\d{2}-\d{2})(?:T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:Z|[+-](\d{2}):(\d{2})))?$/,
  );
  const day = match?.[1];
  if (
    !day ||
    day === "0001-01-01" ||
    Number(day.slice(0, 4)) < 1 ||
    (match[2] !== undefined &&
      (Number(match[2]) > 23 ||
        Number(match[3]) > 59 ||
        Number(match[4]) > 59 ||
        Number(match[5] ?? 0) > 23 ||
        Number(match[6] ?? 0) > 59)) ||
    !Number.isFinite(Date.parse(`${day}T00:00:00Z`)) ||
    new Date(`${day}T00:00:00Z`).toISOString().slice(0, 10) !== day
  ) {
    throw new RangeError(
      "Enter a valid calendar date from 0001-01-02 to 9999-12-31.",
    );
  }
  return day;
}

export function localCalendarDate(value = new Date()): string {
  return `${value.getFullYear()}-${String(value.getMonth() + 1).padStart(2, "0")}-${String(value.getDate()).padStart(2, "0")}`;
}

export function formatDate(value: string, format: DateFormat): string {
  const day = calendarDate(value);
  if (format === "YYYY-MM-DD") return day;
  const [year, month, date] = day.split("-");
  return format === "DD/MM/YYYY"
    ? `${date}/${month}/${year}`
    : `${month}/${date}/${year}`;
}

export function monthKey(value: Date | string): string {
  return (
    typeof value === "string" ? calendarDate(value) : localCalendarDate(value)
  ).slice(0, 7);
}

export function isCurrentMonth(value: string, now = new Date()): boolean {
  return monthKey(value) === monthKey(now);
}

export function netAmount(transactions: Transaction[]): bigint {
  return transactions.reduce(
    (total, transaction) =>
      total +
      (transaction.type === "income"
        ? moneyInteger(transaction.amount)
        : -moneyInteger(transaction.amount)),
    0n,
  );
}

export function validateMoneyRows<T extends object>(
  rows: T[],
  fields: (keyof T)[],
): T[] {
  for (const row of rows)
    for (const field of fields) {
      const amount = row[field];
      if (typeof amount !== "number")
        throw new RangeError("Unsupported monetary response.");
      moneyInteger(amount);
    }
  return rows;
}
