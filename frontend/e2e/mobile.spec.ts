import type { Account, Category } from "../src/api/types";
import { test, expect, openPage, currentDate } from "./fixtures";

test("mobile navigation, transaction sorting and account creation", async ({
  page,
  finance,
}) => {
  const account = await finance.create<Account>("accounts", {
    name: "Mobile account",
  });
  const categories = await finance.list<Category>("categories");
  await finance.create("transactions", {
    name: "Coffee",
    amount: 450,
    account_id: account.id,
    category_id: categories.find((category) => category.name === "Dining")!.id,
    date: currentDate(),
    type: "expense",
    frequency: "one-off",
  });
  await finance.create("transactions", {
    name: "Apple tea",
    amount: 300,
    account_id: account.id,
    category_id: categories.find((category) => category.name === "Dining")!.id,
    date: currentDate(),
    type: "expense",
    frequency: "one-off",
  });
  await openPage(page, "/settings", "Settings");
  await page
    .getByRole("button", { name: "Open workspace navigation", exact: true })
    .click();
  await page.getByRole("link", { name: "Transactions", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Transactions", exact: true }),
  ).toBeVisible();
  await page.getByLabel("Sort by", { exact: true }).selectOption("merchant");
  await page
    .getByRole("button", { name: "Sort ascending", exact: true })
    .click();
  await expect(page.getByRole("table").getByRole("row").first()).toContainText(
    "Apple tea",
  );
  await expect(page.getByRole("table")).toContainText("Coffee");
  await expect(
    page.getByRole("button", {
      name: "Open workspace navigation",
      exact: true,
    }),
  ).toHaveAttribute("aria-expanded", "false");
  expect(
    await page
      .locator("body")
      .evaluate((body) => body.scrollWidth <= window.innerWidth),
  ).toBe(true);
  await page
    .getByRole("button", { name: "Open workspace navigation", exact: true })
    .click();
  await page.getByRole("link", { name: "Accounts", exact: true }).click();
  await page.getByRole("button", { name: "Add account", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByLabel("Name", { exact: true })
    .fill("Mobile savings");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Add account", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Mobile savings", exact: true }),
  ).toBeVisible();
});
