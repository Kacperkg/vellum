import type { Transaction, Transfer, UserSettings } from "@/api/types";
import {
  formatMoney,
  formatSignedMoney,
  moneyInteger,
  calendarDate,
} from "@/utils/finance";

export type TransferActivity = Transfer & {
  type: "transfer";
  account_id: string;
  category_id: null;
};

export type FinancialActivity = Transaction | TransferActivity;
export type ActivitySortKey =
  "merchant" | "category" | "account" | "date" | "amount";
export type ActivitySortDirection = "asc" | "desc";

export function sortFinancialActivity(
  items: FinancialActivity[],
  key: ActivitySortKey,
  direction: ActivitySortDirection,
  accounts: ReadonlyMap<string, string>,
  categories: ReadonlyMap<string, string>,
  accountId = "all",
): FinancialActivity[] {
  const value = (item: FinancialActivity): string | number => {
    switch (key) {
      case "merchant":
        return item.name;
      case "category":
        return item.type === "transfer"
          ? "Transfer"
          : (categories.get(item.category_id) ?? "Uncategorised");
      case "account":
        return `${accounts.get(item.account_id) ?? "Unknown account"}${item.type === "transfer" ? ` → ${accounts.get(item.to_account_id) ?? "Unknown account"}` : ""}`;
      case "date":
        return calendarDate(item.date);
      // A global transfer displays its face amount with a neutral arrow, while
      // an account-filtered transfer displays that account's signed movement.
      case "amount":
        return item.type === "transfer" && accountId === "all"
          ? item.amount
          : activityImpact(item, accountId);
    }
  };
  return [...items].sort((a, b) => {
    const first = value(a),
      second = value(b);
    const comparison =
      typeof first === "number" && typeof second === "number"
        ? first - second
        : String(first).localeCompare(String(second), "en", {
            numeric: true,
            sensitivity: "base",
          });
    return (
      comparison * (direction === "asc" ? 1 : -1) ||
      calendarDate(b.date).localeCompare(calendarDate(a.date)) ||
      `${a.type}:${a.id}`.localeCompare(`${b.type}:${b.id}`)
    );
  });
}

export function financialActivity(
  transactions: Transaction[],
  transfers: Transfer[],
): FinancialActivity[] {
  return [
    ...transactions,
    ...transfers.map((transfer): TransferActivity => ({
      ...transfer,
      type: "transfer",
      account_id: transfer.from_account_id,
      category_id: null,
      frequency: transfer.frequency ?? "one-off",
    })),
  ].sort((a, b) => calendarDate(b.date).localeCompare(calendarDate(a.date)));
}

export function activityImpact(
  item: FinancialActivity,
  accountId = "all",
): number {
  if (item.type === "transfer") {
    if (item.from_account_id === accountId) return -item.amount;
    if (item.to_account_id === accountId) return item.amount;
    return 0;
  }
  if (accountId !== "all" && item.account_id !== accountId) return 0;
  return item.type === "income" ? item.amount : -item.amount;
}

export function activityNetAmount(
  items: FinancialActivity[],
  accountId = "all",
): bigint {
  return items.reduce(
    (total, item) => total + moneyInteger(activityImpact(item, accountId)),
    0n,
  );
}

export function recordedAccountBalance(
  accountId: string,
  transactions: Transaction[],
  transfers: Transfer[],
): bigint {
  const recorded = transactions.reduce(
    (total, item) => total + moneyInteger(activityImpact(item, accountId)),
    0n,
  );
  return transfers.reduce(
    (total, transfer) =>
      total -
      moneyInteger(
        transfer.from_account_id === accountId ? transfer.amount : 0,
      ) +
      moneyInteger(transfer.to_account_id === accountId ? transfer.amount : 0),
    recorded,
  );
}

export function formatActivityAmount(
  item: FinancialActivity,
  settings: UserSettings,
  accountId = "all",
): string {
  if (item.type !== "transfer")
    return formatSignedMoney(item.amount, item.type, settings);
  const impact = activityImpact(item, accountId);
  return `${impact < 0 ? "−" : impact > 0 ? "+" : "↔ "}${formatMoney(item.amount, settings)}`;
}
