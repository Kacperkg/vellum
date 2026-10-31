import { readFile } from "node:fs/promises";
import type { Account, Category, Transaction } from "../src/api/types";
import { test, expect, apiURL, openPage } from "./fixtures";

for (const timezoneId of [
  "Etc/GMT+12",
  "Pacific/Kiritimati",
  "Europe/London",
]) {
  test.describe(`financial calendar in ${timezoneId}`, () => {
    test.use({ timezoneId });
    test("ledger display, filters, edit, CSV, budgets and summaries agree on the calendar month", async ({
      page,
      finance,
    }) => {
      await page.clock.setFixedTime(new Date("2026-10-15T12:00:00Z"));
      const account = await finance.create<Account>("accounts", {
        name: "Calendar account",
      });
      const categories = await finance.list<Category>("categories");
      const category = categories.find((item) => item.name !== "Income")!;
      const common = {
        account_id: account.id,
        category_id: category.id,
        type: "expense",
        frequency: "one-off",
      };
      const october = await finance.create<Transaction>("transactions", {
        ...common,
        name: "October boundary",
        amount: 12345,
        date: "2026-10-31T23:30:00-12:00",
      });
      await finance.create("transactions", {
        ...common,
        name: "November boundary",
        amount: 6789,
        date: "2026-11-01T00:30:00+14:00",
      });
      await finance.create("budgets", {
        category_id: category.id,
        allocated_amount: 20000,
      });
      expect(october.date.slice(0, 10)).toBe("2026-10-31");
      await openPage(page, "/transactions", "Transactions");
      const octoberRow = page
        .getByRole("row")
        .filter({ hasText: "October boundary" });
      await expect(octoberRow).toContainText("31/10/2026");
      await expect(
        page.getByRole("row").filter({ hasText: "November boundary" }),
      ).toContainText("01/11/2026");
      await page.getByRole("button", { name: "Filters", exact: true }).click();
      await page.getByLabel("From", { exact: true }).fill("2026-10-31");
      await page.getByLabel("To", { exact: true }).fill("2026-10-31");
      await expect(octoberRow).toBeVisible();
      await expect(
        page.getByRole("row").filter({ hasText: "November boundary" }),
      ).toHaveCount(0);
      await expect(page.locator(".transactions-page")).toContainText(
        "net -£123.45",
      );
      const downloadPromise = page.waitForEvent("download");
      await page.getByRole("button", { name: "Export", exact: true }).click();
      const download = await downloadPromise;
      const csv = await readFile((await download.path())!, "utf8");
      expect(csv).toContain('"2026-10-31"');
      expect(csv).not.toContain("November boundary");
      await page
        .getByRole("button", { name: "Edit transactions", exact: true })
        .click();
      await page.getByLabel("Select October boundary", { exact: true }).check();
      await page
        .getByRole("button", { name: "Edit selected", exact: true })
        .click();
      const dialog = page.getByRole("dialog");
      await expect(dialog.getByLabel("Date", { exact: true })).toHaveValue(
        "2026-10-31",
      );
      await dialog
        .getByRole("button", { name: "Save changes", exact: true })
        .click();
      await expect(dialog).toHaveCount(0);
      expect(
        (await finance.list<Transaction>("transactions"))
          .find((item) => item.id === october.id)
          ?.date.slice(0, 10),
      ).toBe("2026-10-31");
      await openPage(page, "/budgets", "Budgets");
      await expect(page.locator(".budget-card")).toContainText("£123.45");
      await expect(page.locator(".side-nav .month-card")).toContainText(
        "£123.45",
      );
      await page.goto("/");
      await expect(
        page.getByRole("heading", { name: /Good .*Alex/ }),
      ).toBeVisible();
      await expect(
        page.getByText("Loading your finance data…", { exact: true }),
      ).toHaveCount(0);
      await expect(page.locator(".balance-summaries")).toContainText("£123");
      await expect(page.locator(".mini-budget-list")).toContainText("£123.45");
      await openPage(page, "/analytics", "Analytics");
      await expect(
        page.getByRole("button", {
          name: "October 2026. Income £0.00. Expenses £123.45.",
          exact: true,
        }),
      ).toBeVisible();
    });
  });
}

test("monetary forms, exact maximum display/edit/export and aggregate rejection preserve pennies", async ({
  page,
  finance,
}) => {
  await finance.create("accounts", { name: "Exact account" });
  await openPage(page, "/transactions", "Transactions");
  const add = async (name: string, amount: string) => {
    await page
      .locator(".page-actions")
      .getByRole("button", { name: "Add transaction", exact: true })
      .click();
    const dialog = page.getByRole("dialog");
    await dialog.getByLabel("Type", { exact: true }).selectOption("income");
    await dialog.getByLabel("Name", { exact: true }).fill(name);
    await dialog.getByLabel("Amount", { exact: true }).fill(amount);
    await dialog
      .getByLabel("Account", { exact: true })
      .selectOption({ label: "Exact account" });
    await dialog
      .getByLabel("Category", { exact: true })
      .selectOption({ label: "Income" });
    return dialog;
  };
  let dialog = await add("Exact boundary", "90071992547409.92");
  await dialog
    .getByRole("button", { name: "Add transaction", exact: true })
    .click();
  await expect(dialog.getByRole("alert")).toContainText("90071992547409.91");
  expect(await finance.list<Transaction>("transactions")).toHaveLength(0);
  await dialog.getByLabel("Amount", { exact: true }).fill("1.005");
  await dialog
    .getByRole("button", { name: "Add transaction", exact: true })
    .click();
  await expect(dialog.getByRole("alert")).toContainText("two decimal places");
  expect(await finance.list<Transaction>("transactions")).toHaveLength(0);
  await dialog.getByLabel("Amount", { exact: true }).fill("90071992547409.91");
  await dialog
    .getByRole("button", { name: "Add transaction", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  expect((await finance.list<Transaction>("transactions"))[0].amount).toBe(
    Number.MAX_SAFE_INTEGER,
  );
  await expect(
    page.getByRole("row").filter({ hasText: "Exact boundary" }),
  ).toContainText("£90,071,992,547,409.91");
  const downloadPromise = page.waitForEvent("download");
  await page.getByRole("button", { name: "Export", exact: true }).click();
  const download = await downloadPromise;
  expect(await readFile((await download.path())!, "utf8")).toContain(
    "90071992547409.91",
  );
  await page
    .getByRole("button", { name: "Edit transactions", exact: true })
    .click();
  await page.getByLabel("Select Exact boundary", { exact: true }).check();
  await page
    .getByRole("button", { name: "Edit selected", exact: true })
    .click();
  dialog = page.getByRole("dialog");
  await expect(dialog.getByLabel("Amount", { exact: true })).toHaveValue(
    "90071992547409.91",
  );
  await dialog.getByLabel("Amount", { exact: true }).fill("90071992547409.90");
  await dialog
    .getByRole("button", { name: "Save changes", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  expect((await finance.list<Transaction>("transactions"))[0].amount).toBe(
    Number.MAX_SAFE_INTEGER - 1,
  );
  dialog = await add("One penny", "0.01");
  await dialog
    .getByRole("button", { name: "Add transaction", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  dialog = await add("Overflow penny", "0.01");
  await dialog
    .getByRole("button", { name: "Add transaction", exact: true })
    .click();
  await expect(dialog.getByRole("alert")).toContainText(
    /range|supported|monetary/i,
  );
  expect(await finance.list<Transaction>("transactions")).toHaveLength(2);
  await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
  await openPage(page, "/accounts", "Accounts");
  await expect(page.locator(".account-summary")).toContainText(
    "£90,071,992,547,409.91",
  );
  // Real API rejection agrees with the browser's upper-bound contract.
  const accounts = await finance.list<Account>("accounts");
  const categories = await finance.list<Category>("categories");
  const response = await finance.request.post(`${apiURL}/api/transactions`, {
    headers: { Authorization: `Bearer ${finance.session.access_token}` },
    data: {
      name: "Unsafe wire amount",
      account_id: accounts[0].id,
      category_id: categories[0].id,
      type: "income",
      frequency: "one-off",
      date: "2026-10-31",
      amount: Number.MAX_SAFE_INTEGER + 1,
    },
  });
  expect(response.status()).toBe(400);
  expect(await finance.list<Transaction>("transactions")).toHaveLength(2);
});

test("legacy unsupported money has an explicit data error without rendering rounded totals", async ({
  page,
  finance,
}) => {
  await finance.create("accounts", { name: "Legacy account" });
  await page.route("**/api/accounts", (route) =>
    route.fulfill({
      status: 400,
      contentType: "application/json",
      body: JSON.stringify({
        error:
          "money must remain within +/-9007199254740991 minor units: invalid input",
      }),
    }),
  );
  await page.goto("/accounts");
  await expect(page.getByRole("alert")).toContainText(
    "supported exact monetary range",
  );
  await expect(page.getByRole("alert")).toContainText(
    "Existing records have not been changed",
  );
  await expect(page.locator(".account-summary")).toHaveCount(0);
  await page.unroute("**/api/accounts");
  await page.route("**/api/transactions", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify([
        {
          id: "legacy",
          account_id: "legacy",
          name: "Unsafe legacy entry",
          amount: 9007199254740992,
          type: "income",
          frequency: "one-off",
          category_id: "income",
          date: "2026-10-31T00:00:00Z",
        },
      ]),
    }),
  );
  await page.goto("/transactions");
  await expect(page.getByRole("alert")).toContainText(
    "supported exact monetary range",
  );
  await expect(
    page.getByRole("row").filter({ hasText: "Unsafe legacy entry" }),
  ).toHaveCount(0);
});

test("derived account, budget and analytics totals preserve cents above the API field bound", async ({
  page,
  finance,
}) => {
  const categories = (await finance.list<Category>("categories"))
    .filter((item) => item.name !== "Income")
    .slice(0, 2);
  for (const [index, category] of categories.entries()) {
    const account = await finance.create<Account>("accounts", {
      name: `Large account ${index}`,
    });
    for (const type of ["income", "expense"]) {
      await finance.create("transactions", {
        name: `Large ${type} ${index}`,
        amount: Number.MAX_SAFE_INTEGER,
        account_id: account.id,
        category_id: category.id,
        date: "2026-10-31",
        frequency: "one-off",
        type,
      });
    }
    await finance.create("goals", {
      name: `Large goal ${index}`,
      account_id: account.id,
      target_amount: Number.MAX_SAFE_INTEGER,
      saved_amount: 0,
      monthly_contribution: 0,
    });
    await finance.create("budgets", {
      category_id: category.id,
      allocated_amount: Number.MAX_SAFE_INTEGER,
    });
  }
  await page.clock.setFixedTime(new Date("2026-10-15T12:00:00Z"));
  await openPage(page, "/accounts", "Accounts");
  await expect(page.locator(".account-summary")).toContainText(
    "£180,143,985,094,819.82",
  );
  await expect(page.locator(".account-summary")).toContainText(
    "Overall shortfall",
  );
  await openPage(page, "/budgets", "Budgets");
  await expect(page.locator(".side-nav .month-card")).toContainText(
    "£180,143,985,094,819.82",
  );
  await expect(page.locator(".budget-card").first()).toContainText(
    "£90,071,992,547,409.91",
  );
  await openPage(page, "/analytics", "Analytics");
  await expect(
    page.getByRole("button", {
      name: "October 2026. Income £180,143,985,094,819.82. Expenses £180,143,985,094,819.82.",
      exact: true,
    }),
  ).toBeVisible();
});
