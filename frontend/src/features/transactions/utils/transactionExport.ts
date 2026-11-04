import type { Account, Category, UserSettings } from "@/api/types";
import { minorToMajor, calendarDate } from "@/utils/finance";
import { activityImpact, type FinancialActivity } from "./activity";

function csvText(value: string): string {
  const safe = /^[\s]*[=+\-@]/.test(value) ? `'${value}` : value;
  return `"${safe.replaceAll('"', '""')}"`;
}

export function exportTransactions(
  transactions: FinancialActivity[],
  accounts: Account[],
  categories: Category[],
  settings: UserSettings,
  accountId = "all",
): void {
  const rows = [
    [
      "Name",
      "Category",
      "Account",
      "To account",
      "Date",
      "Frequency",
      "Type",
      "Amount",
      "Net impact",
      "Currency",
    ].join(","),
    ...transactions.map((item) =>
      [
        csvText(item.name),
        csvText(
          item.type === "transfer"
            ? "Transfer"
            : (categories.find((category) => category.id === item.category_id)
                ?.name ?? ""),
        ),
        csvText(
          accounts.find((account) => account.id === item.account_id)?.name ??
            "",
        ),
        csvText(
          item.type === "transfer"
            ? (accounts.find((account) => account.id === item.to_account_id)
                ?.name ?? "")
            : "",
        ),
        csvText(calendarDate(item.date)),
        csvText(item.frequency),
        csvText(item.type),
        minorToMajor(item.amount * (item.type === "expense" ? -1 : 1)),
        minorToMajor(activityImpact(item, accountId)),
        csvText(settings.currency),
      ].join(","),
    ),
  ];
  const url = URL.createObjectURL(
    new Blob(["\uFEFF", rows.join("\r\n")], {
      type: "text/csv;charset=utf-8;",
    }),
  );
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = "vellum-transactions.csv";
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
