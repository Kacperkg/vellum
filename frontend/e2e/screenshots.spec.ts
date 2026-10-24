import { mkdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import type { Account, Category } from "../src/api/types";
import { test, expect, openPage } from "./fixtures";

// Explicit opt-in only. Every record belongs to this disposable test database;
// the generated images contain synthetic data, never a development user's data.
test("capture documented desktop and mobile views using synthetic records", async ({
  page,
  finance,
}) => {
  const checking = await finance.create<Account>("accounts", {
    name: "Everyday checking",
  });
  const savings = await finance.create<Account>("accounts", {
    name: "Savings",
  });
  const bills = await finance.create<Account>("accounts", {
    name: "Bills & subscriptions",
  });
  const categories = await finance.list<Category>("categories");
  const categoryId = (name: string) =>
    categories.find((category) => category.name === name)!.id;
  const now = new Date();
  const dateInMonth = (offset: number, day = 2) =>
    new Date(
      Date.UTC(
        now.getFullYear(),
        now.getMonth() - offset,
        Math.min(day, now.getDate()),
        12,
      ),
    ).toISOString();
  const transaction = (
    name: string,
    amount: number,
    category: string,
    offset = 0,
    type = "expense",
    frequency = "one-off",
    account_id = checking.id,
  ) =>
    finance.create("transactions", {
      name,
      amount,
      category_id: categoryId(category),
      account_id,
      date: dateInMonth(offset),
      type,
      frequency,
    });
  for (let offset = 5; offset >= 0; offset--) {
    await transaction(
      "Acme — payroll",
      [642000, 710000, 640000, 766000, 650000, 640000][offset]!,
      "Income",
      offset,
      "income",
      "monthly",
    );
    await transaction("Apartment rent", 185000, "Housing", offset);
    await transaction(
      "Weekly groceries",
      [29334, 39000, 46000, 51000, 34000, 42000][offset]!,
      "Groceries",
      offset,
    );
    await transaction(
      "Everyday purchases",
      [95000, 110000, 130000, 105000, 125000, 135000][offset]!,
      "Shopping",
      offset,
    );
  }
  await transaction("Whole Foods", 8234, "Groceries");
  await transaction("Blue Bottle Coffee", 675, "Dining");
  await transaction("Dinner with friends", 4300, "Dining");
  await transaction("Uber", 1840, "Transport");
  await transaction("Electricity", 17270, "Utilities");
  await transaction("Cinema", 3200, "Entertainment");
  await transaction(
    "Netflix",
    1599,
    "Subscriptions",
    0,
    "expense",
    "monthly",
    bills.id,
  );
  await transaction(
    "Spotify",
    1099,
    "Subscriptions",
    0,
    "expense",
    "monthly",
    bills.id,
  );
  await finance.create("transfers", {
    name: "Savings contribution",
    amount: 300000,
    from_account_id: checking.id,
    to_account_id: savings.id,
    date: dateInMonth(0),
    frequency: "one-off",
  });
  await finance.create("transfers", {
    name: "Monthly bills funding",
    amount: 40000,
    from_account_id: checking.id,
    to_account_id: bills.id,
    date: dateInMonth(0),
    frequency: "monthly",
  });
  for (const [category, allocated_amount] of [
    ["Housing", 190000],
    ["Groceries", 50000],
    ["Dining", 22000],
    ["Transport", 18000],
    ["Utilities", 22000],
    ["Entertainment", 12000],
  ] as const) {
    await finance.create("budgets", {
      category_id: categoryId(category),
      allocated_amount,
    });
  }
  for (const [name, target_amount, saved_amount, monthly_contribution] of [
    ["Japan trip", 120000, 78000, 20000],
    ["New MacBook", 120000, 52000, 15000],
  ] as const) {
    await finance.create("goals", {
      name,
      target_amount,
      saved_amount,
      monthly_contribution,
      account_id: savings.id,
    });
  }
  const directory = fileURLToPath(
    new URL("../../docs/screenshots/", import.meta.url),
  );
  await mkdir(directory, { recursive: true });
  for (const [route, title, filename] of [
    ["/", "", "overview.png"],
    ["/accounts", "Accounts", "accounts.png"],
    ["/analytics", "Analytics", "analytics.png"],
    ["/transactions", "Transactions", "transactions.png"],
  ]) {
    if (title) await openPage(page, route!, title);
    else {
      await page.goto("/");
      await expect(page.locator(".balance-amount")).toBeVisible();
    }
    await page.screenshot({
      path: `${directory}${filename}`,
      animations: "disabled",
    });
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await openPage(page, "/settings", "Settings");
  await page.screenshot({
    path: `${directory}settings-mobile.png`,
    animations: "disabled",
  });
  await openPage(page, "/transactions", "Transactions");
  await page.screenshot({
    path: `${directory}transactions-mobile.png`,
    animations: "disabled",
  });
});
