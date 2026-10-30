import { randomUUID } from "node:crypto";
import { readFile } from "node:fs/promises";
import type { Account, Category, Goal } from "../src/api/types";
import {
  test,
  expect,
  apiURL,
  password,
  openPage,
  addAccount,
  currentDate,
  signOut,
  expireSession,
  storedSession,
} from "./fixtures";

test("registration, logout, login and protected-route redirects", async ({
  page,
}) => {
  await page.goto("/accounts");
  await expect(page).toHaveURL(/\/login$/);
  await page.getByRole("button", { name: "Create one", exact: true }).click();
  const email = `browser-${randomUUID()}@example.test`;
  await page.getByLabel("First name", { exact: true }).fill("Alex");
  await page.getByLabel("Last name", { exact: true }).fill("Demo");
  await page.getByLabel("Email", { exact: true }).fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page
    .getByRole("button", { name: "Create account", exact: true })
    .click();
  await expect(
    page.getByRole("heading", {
      name: "Add your first bank account",
      exact: true,
    }),
  ).toBeVisible();
  await page.reload();
  await expect(
    page.getByRole("heading", {
      name: "Add your first bank account",
      exact: true,
    }),
  ).toBeVisible();
  for (const path of ["/transactions", "/settings", "/accounts", "/"]) {
    await page.goto(path);
    await expect(
      page.getByRole("heading", {
        name: "Add your first bank account",
        exact: true,
      }),
    ).toBeVisible();
    await expect(page.locator(".app-frame")).toHaveCount(0);
  }
  await signOut(page);
  await page.getByLabel("Email", { exact: true }).fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(
    page.getByRole("heading", {
      name: "Add your first bank account",
      exact: true,
    }),
  ).toBeVisible();
  await page.getByLabel("Account name", { exact: true }).fill("Everyday");
  await page
    .getByRole("button", { name: "Add account and continue", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: /Good .*Alex/ }),
  ).toBeVisible();
  await signOut(page);
  await expect(page).toHaveURL(/\/login$/);
  await page.getByLabel("Email", { exact: true }).fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: /Good .*Alex/ }),
  ).toBeVisible();
  await page.reload();
  await expect(
    page.getByRole("heading", { name: /Good .*Alex/ }),
  ).toBeVisible();
});

test("income, recurring transfer and expense preserve the correct account totals", async ({
  page,
  finance,
}) => {
  await finance.create("accounts", { name: "Salary" });
  await openPage(page, "/accounts", "Accounts");
  await addAccount(page, "Bills");
  await openPage(page, "/transactions", "Transactions");
  await page
    .locator(".page-actions")
    .getByRole("button", { name: "Add transaction", exact: true })
    .click();
  let dialog = page.getByRole("dialog");
  await dialog.getByLabel("Type", { exact: true }).selectOption("income");
  await dialog.getByLabel("Name", { exact: true }).fill("Monthly salary");
  await dialog.getByLabel("Amount", { exact: true }).fill("3000");
  await dialog
    .getByLabel("Account", { exact: true })
    .selectOption({ label: "Salary" });
  await dialog
    .getByLabel("Category", { exact: true })
    .selectOption({ label: "Income" });
  await dialog
    .getByRole("button", { name: "Add transaction", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  // The navbar's New action opens this same transaction form.
  await page
    .locator(".app-topbar")
    .getByRole("button", { name: "Add transaction" })
    .click();
  dialog = page.getByRole("dialog");
  await dialog.getByLabel("Type", { exact: true }).selectOption("transfer");
  await dialog.getByLabel("Frequency", { exact: true }).selectOption("monthly");
  await dialog.getByLabel("Description", { exact: true }).fill("Bills funding");
  await dialog.getByLabel("Amount", { exact: true }).fill("600");
  await dialog
    .getByLabel("From account", { exact: true })
    .selectOption({ label: "Salary" });
  await dialog
    .getByLabel("To account", { exact: true })
    .selectOption({ label: "Bills" });
  await dialog
    .getByRole("button", { name: "Record transfer", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  await page
    .locator(".page-actions")
    .getByRole("button", { name: "Add transaction", exact: true })
    .click();
  dialog = page.getByRole("dialog");
  await dialog.getByLabel("Name", { exact: true }).fill("Internet bill");
  await dialog.getByLabel("Amount", { exact: true }).fill("100");
  await dialog
    .getByLabel("Account", { exact: true })
    .selectOption({ label: "Bills" });
  await dialog
    .getByLabel("Category", { exact: true })
    .selectOption({ label: "Utilities" });
  await dialog
    .getByRole("button", { name: "Add transaction", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  const accounts = await finance.list<Account>("accounts");
  expect(accounts.find((account) => account.name === "Salary")?.balance).toBe(
    240000,
  );
  expect(accounts.find((account) => account.name === "Bills")?.balance).toBe(
    50000,
  );
  expect(accounts.reduce((sum, account) => sum + account.balance, 0)).toBe(
    290000,
  );
  await openPage(page, "/accounts", "Accounts");
  await expect(
    page
      .locator(".account-card")
      .filter({
        has: page.getByRole("heading", { name: "Salary", exact: true }),
      })
      .locator(".account-balance"),
  ).toHaveText("£2,400.00");
  await openPage(page, "/subscriptions", "Subscriptions");
  await expect(
    page.getByRole("heading", { name: "Bills funding", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Internet bill", exact: true }),
  ).toHaveCount(0);
});

test("linked goals reserve full targets and unlinking restores availability", async ({
  page,
  finance,
}) => {
  const account = await finance.create<Account>("accounts", {
    name: "Savings",
  });
  const categories = await finance.list<Category>("categories");
  await finance.create("transactions", {
    name: "Savings income",
    amount: 300000,
    account_id: account.id,
    category_id: categories.find((item) => item.name === "Income")!.id,
    date: currentDate(),
    frequency: "one-off",
    type: "income",
  });
  await openPage(page, "/goals", "Goals");
  for (const name of ["Japan trip", "Laptop"]) {
    await page.getByRole("button", { name: "New goal", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "New Goal", exact: true });
    await dialog.getByLabel("Name", { exact: true }).fill(name);
    await dialog.getByLabel("Target", { exact: true }).fill("1200");
    await dialog
      .getByLabel("Linked account", { exact: true })
      .selectOption(account.id);
    await dialog
      .getByRole("button", { name: "Create goal", exact: true })
      .click();
    await expect(dialog).toHaveCount(0);
  }
  expect((await finance.list<Account>("accounts"))[0]?.unallocated_amount).toBe(
    60000,
  );
  await openPage(page, "/accounts", "Accounts");
  await expect(
    page.getByText("£600.00 is not allocated to a goal.", { exact: true }),
  ).toBeVisible();
  await openPage(page, "/goals", "Goals");
  await page
    .getByRole("button", { name: "Edit Japan trip", exact: true })
    .click();
  const dialog = page.getByRole("dialog", { name: "Edit Goal", exact: true });
  await dialog.getByLabel("Linked account", { exact: true }).selectOption("");
  await dialog
    .getByRole("button", { name: "Save changes", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  expect(
    (await finance.list<Goal>("goals")).find(
      (goal) => goal.name === "Japan trip",
    )?.account_id,
  ).toBeNull();
  expect((await finance.list<Account>("accounts"))[0]?.unallocated_amount).toBe(
    180000,
  );
});

test("budgets persist and duplicate categories return a conflict", async ({
  page,
  finance,
}) => {
  await finance.create("accounts", { name: "Everyday" });
  await openPage(page, "/budgets", "Budgets");
  await page.getByRole("button", { name: "New budget", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "New Budget", exact: true });
  await dialog
    .getByLabel("Category", { exact: true })
    .selectOption({ label: "Groceries" });
  await dialog.getByLabel("Monthly allocation", { exact: true }).fill("500");
  await dialog
    .getByRole("button", { name: "Create budget", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "Groceries", exact: true }),
  ).toBeVisible();
  const categories = await finance.list<Category>("categories");
  const duplicate = await finance.request.post(`${apiURL}/api/budgets`, {
    headers: { Authorization: `Bearer ${finance.session.access_token}` },
    data: {
      category_id: categories.find((item) => item.name === "Groceries")!.id,
      allocated_amount: 10000,
    },
  });
  expect(duplicate.status()).toBe(409);
  expect(await finance.list("budgets")).toHaveLength(1);
  await page.getByRole("button", { name: "New budget", exact: true }).click();
  await expect(
    page
      .getByRole("dialog")
      .getByLabel("Category", { exact: true })
      .locator("option", { hasText: "Groceries" }),
  ).toHaveCount(0);
});

test("transaction filter, sort, edit mode and CSV reflect the persisted activity", async ({
  page,
  finance,
}) => {
  const account = await finance.create<Account>("accounts", {
    name: "Everyday",
  });
  const categories = await finance.list<Category>("categories");
  for (const [name, amount] of [
    ["Zebra store", 2000],
    ["Apple shop", 1000],
  ] as const) {
    await finance.create("transactions", {
      name,
      amount,
      account_id: account.id,
      category_id: categories.find((item) => item.name === "Shopping")!.id,
      date: currentDate(),
      frequency: "one-off",
      type: "expense",
    });
  }
  await openPage(page, "/transactions", "Transactions");
  await page
    .getByRole("button", { name: "Sort by merchant, ascending", exact: true })
    .click();
  await expect(page.getByRole("table").getByRole("row").nth(1)).toContainText(
    "Apple shop",
  );
  await page
    .getByRole("button", { name: "Edit transactions", exact: true })
    .click();
  await page
    .getByRole("radio", { name: "Select Apple shop", exact: true })
    .check();
  await page
    .getByRole("button", { name: "Edit selected", exact: true })
    .click();
  const dialog = page.getByRole("dialog", {
    name: "Edit Transaction",
    exact: true,
  });
  await dialog.getByLabel("Amount", { exact: true }).fill("12.50");
  await dialog
    .getByRole("button", { name: "Save changes", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  await page
    .getByRole("textbox", {
      name: "Search merchant, category or account",
      exact: true,
    })
    .fill("Apple");
  await expect(page.getByRole("table").getByRole("row")).toHaveCount(2);
  await expect(page.getByRole("table")).toContainText("−£12.50");
  const downloadPromise = page.waitForEvent("download");
  await page.getByRole("button", { name: "Export", exact: true }).click();
  const download = await downloadPromise;
  const csv = await readFile((await download.path())!, "utf8");
  expect(csv).toContain('"Apple shop"');
  expect(csv).toContain("-12.50");
  expect(csv).not.toContain("Zebra store");
  await page.reload();
  await expect(
    page.getByRole("table").getByRole("row").filter({ hasText: "Apple shop" }),
  ).toContainText("−£12.50");
});

test("display preferences save and survive a reload", async ({
  page,
  finance,
}) => {
  await finance.create("accounts", { name: "Everyday" });
  await openPage(page, "/settings", "Settings");
  await page.getByLabel("Currency", { exact: true }).selectOption("EUR");
  await expect(
    page.getByRole("status").filter({ hasText: "Preferences saved" }),
  ).toHaveText("Preferences saved");
  await page.reload();
  await expect(page.getByLabel("Currency", { exact: true })).toHaveValue("EUR");
  await page
    .getByRole("button", { name: "Restore defaults", exact: true })
    .click();
  await expect(
    page.getByRole("status").filter({ hasText: "Preferences saved" }),
  ).toHaveText("Preferences saved");
  await expect(page.getByLabel("Currency", { exact: true })).toHaveValue("GBP");
});

test("concurrent expired-session requests share a successful refresh", async ({
  page,
  finance,
}) => {
  await finance.create("accounts", { name: "Everyday" });
  await expireSession(page);
  let refreshes = 0;
  page.on("request", (request) => {
    if (request.url() === `${apiURL}/refresh`) refreshes++;
  });
  await openPage(page, "/accounts", "Accounts");
  await expect(
    page.getByRole("heading", { name: "Everyday", exact: true }),
  ).toBeVisible();
  expect(refreshes).toBe(1);
});

test("a rejected refresh clears the session and returns to sign in", async ({
  page,
  finance,
}) => {
  void finance;
  await expireSession(page, true);
  await page.goto("/accounts");
  await expect(page).toHaveURL(/\/login$/);
  await expect(
    page.getByRole("heading", { name: "Welcome back.", exact: true }),
  ).toBeVisible();
  expect((await storedSession(page)) === null).toBe(true);
});

test("a late stale-token 401 retries without a second refresh", async ({
  page,
  finance,
}) => {
  await finance.create("accounts", { name: "Everyday" });
  await expireSession(page);
  let refreshes = 0;
  let releaseLateResponse!: () => void;
  const release = new Promise<void>((resolve) => {
    releaseLateResponse = resolve;
  });
  page.on("request", (request) => {
    if (request.url() === `${apiURL}/refresh`) refreshes++;
  });
  await page.route(`${apiURL}/api/goals`, async (route) => {
    if (
      route.request().headers().authorization === "Bearer expired-test-access"
    )
      await release;
    await route.continue();
  });
  try {
    await page.goto("/accounts");
    await page.waitForFunction(() => {
      const token = JSON.parse(localStorage.getItem("vellum:session") ?? "null")
        ?.credentials?.access_token;
      return !!token && token !== "expired-test-access";
    });
    releaseLateResponse();
    await expect(
      page.getByRole("heading", { name: "Everyday", exact: true }),
    ).toBeVisible();
    expect(refreshes).toBe(1);
  } finally {
    releaseLateResponse();
  }
});

test("account checks fail closed and retry without showing the dashboard", async ({
  page,
  finance,
}) => {
  await finance.create("accounts", { name: "Existing account" });
  await page.route(`${apiURL}/api/accounts`, (route) =>
    route.fulfill({ status: 500, json: { error: "Synthetic outage" } }),
  );
  await page.goto("/");
  await expect(
    page.getByRole("heading", {
      name: "Unable to check your accounts",
      exact: true,
    }),
  ).toBeVisible();
  await expect(page.locator(".app-frame")).toHaveCount(0);
  await expect(page.getByLabel("Account name", { exact: true })).toHaveCount(0);
  await page.unroute(`${apiURL}/api/accounts`);
  await page.getByRole("button", { name: "Try again", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: /Good .*Alex/ }),
  ).toBeVisible();
});

test("a committed account with a lost response is recovered from server state", async ({
  page,
  finance,
}) => {
  await page.goto("/");
  await expect(
    page.getByRole("heading", {
      name: "Add your first bank account",
      exact: true,
    }),
  ).toBeVisible();
  await page.route(`${apiURL}/api/accounts`, async (route) => {
    if (route.request().method() !== "POST") {
      await route.continue();
      return;
    }
    const response = await route.fetch();
    expect(response.status()).toBe(201);
    await route.abort("failed");
  });
  await page
    .getByLabel("Account name", { exact: true })
    .fill("Recovered account");
  await page
    .getByRole("button", { name: "Add account and continue", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: /Good .*Alex/ }),
  ).toBeVisible();
  expect(await finance.list("accounts")).toHaveLength(1);
  await page.reload();
  await expect(
    page.getByRole("heading", { name: /Good .*Alex/ }),
  ).toBeVisible();
});

test("failed account creation stays in setup and can be retried", async ({
  page,
  finance,
}) => {
  void finance;
  await page.goto("/");
  await expect(
    page.getByRole("heading", {
      name: "Add your first bank account",
      exact: true,
    }),
  ).toBeVisible();
  await page.getByLabel("Account name", { exact: true }).fill("   ");
  await expect(
    page.getByRole("button", { name: "Add account and continue", exact: true }),
  ).toBeDisabled();
  await page.route(`${apiURL}/api/accounts`, async (route) => {
    if (route.request().method() === "POST")
      await route.fulfill({
        status: 500,
        json: { error: "Please retry creation" },
      });
    else await route.continue();
  });
  await page.getByLabel("Account name", { exact: true }).fill("Retry account");
  await page
    .getByRole("button", { name: "Add account and continue", exact: true })
    .click();
  await expect(page.getByRole("alert")).toHaveText("Please retry creation");
  await expect(page.locator(".app-frame")).toHaveCount(0);
  await page.unroute(`${apiURL}/api/accounts`);
  await page
    .getByRole("button", { name: "Add account and continue", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: /Good .*Alex/ }),
  ).toBeVisible();
});

test("deleting the last account returns to setup and refresh preserves it", async ({
  page,
  finance,
}) => {
  await finance.create("accounts", { name: "Only account" });
  await openPage(page, "/accounts", "Accounts");
  await page
    .getByRole("button", { name: "Delete Only account", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Delete", exact: true })
    .click();
  await expect(
    page.getByRole("heading", {
      name: "Add your first bank account",
      exact: true,
    }),
  ).toBeVisible();
  await expect(page.locator(".app-frame")).toHaveCount(0);
  await page.reload();
  await expect(
    page.getByRole("heading", {
      name: "Add your first bank account",
      exact: true,
    }),
  ).toBeVisible();
});

test("an expired access token refreshes back into unfinished account setup", async ({
  page,
  finance,
}) => {
  void finance;
  await expireSession(page);
  await page.goto("/transactions");
  await expect(
    page.getByRole("heading", {
      name: "Add your first bank account",
      exact: true,
    }),
  ).toBeVisible();
  await expect(page.locator(".app-frame")).toHaveCount(0);
});

test("a failed setup confirmation offers a read retry instead of another creation", async ({
  page,
  finance,
}) => {
  let created = false;
  await page.route(`${apiURL}/api/accounts`, async (route) => {
    if (route.request().method() === "POST") {
      const response = await route.fetch();
      expect(response.status()).toBe(201);
      created = true;
      await route.fulfill({ response });
    } else if (created) {
      await route.fulfill({
        status: 500,
        json: { error: "Synthetic read outage" },
      });
    } else await route.continue();
  });
  await page.goto("/");
  await page
    .getByLabel("Account name", { exact: true })
    .fill("Confirmed later");
  await page
    .getByRole("button", { name: "Add account and continue", exact: true })
    .click();
  await expect(
    page.getByRole("heading", {
      name: "Unable to check your accounts",
      exact: true,
    }),
  ).toBeVisible();
  await expect(page.getByLabel("Account name", { exact: true })).toHaveCount(0);
  await expect(page.locator(".app-frame")).toHaveCount(0);
  await page.unroute(`${apiURL}/api/accounts`);
  await page.getByRole("button", { name: "Try again", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: /Good .*Alex/ }),
  ).toBeVisible();
  expect(await finance.list("accounts")).toHaveLength(1);
});

test("focus rechecks server setup and ignores an older account response", async ({
  page,
  finance,
}) => {
  await page.goto("/");
  await expect(
    page.getByRole("heading", {
      name: "Add your first bank account",
      exact: true,
    }),
  ).toBeVisible();
  let releaseStale!: () => void;
  const staleReleased = new Promise<void>((resolve) => {
    releaseStale = resolve;
  });
  let markStaleStarted!: () => void;
  const staleStarted = new Promise<void>((resolve) => {
    markStaleStarted = resolve;
  });
  let first = true;
  await page.route(`${apiURL}/api/accounts`, async (route) => {
    if (!first) {
      await route.continue();
      return;
    }
    first = false;
    markStaleStarted();
    await staleReleased;
    await route.fulfill({ status: 200, json: [] });
  });
  try {
    await page.evaluate(() => window.dispatchEvent(new Event("focus")));
    await staleStarted;
    // Simulates setup completed in another browser tab while a prior read waits.
    await finance.create("accounts", { name: "Created elsewhere" });
    await page.evaluate(() => window.dispatchEvent(new Event("focus")));
    await expect(
      page.getByRole("heading", { name: /Good .*Alex/ }),
    ).toBeVisible();
    const staleResponse = page.waitForResponse(
      (response) => response.url() === `${apiURL}/api/accounts`,
    );
    releaseStale();
    await staleResponse;
    // Flush the response handler before checking the older empty list was ignored.
    await page.evaluate(
      () =>
        new Promise((resolve) =>
          requestAnimationFrame(() => requestAnimationFrame(resolve)),
        ),
    );
    await expect(
      page.getByRole("heading", { name: /Good .*Alex/ }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", {
        name: "Add your first bank account",
        exact: true,
      }),
    ).toHaveCount(0);
  } finally {
    releaseStale();
  }
});
