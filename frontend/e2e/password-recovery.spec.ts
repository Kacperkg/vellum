import { test, expect, apiURL, storedSession } from "./fixtures";

const token = "A".repeat(43);

test("forgot password validates input, submits once and gives generic confirmation", async ({
  page,
}) => {
  let requests = 0;
  let release: () => void = () => {};
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route(`${apiURL}/forgot-password`, async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    requests++;
    expect(route.request().postDataJSON()).toEqual({
      email: "person@example.test",
    });
    await pending;
    await route.fulfill({
      json: {
        message:
          "If an account exists with this email address, password reset instructions will be sent.",
      },
    });
  });
  await page.goto("/login");
  await page.getByRole("link", { name: "Forgot password?" }).click();
  const submit = page.getByRole("button", { name: "Send reset instructions" });
  await submit.click();
  expect(requests).toBe(0);
  await page.getByLabel("Email", { exact: true }).fill("person@example.test");
  await submit.click();
  await expect(
    page.getByRole("button", { name: "Sending instructions…" }),
  ).toBeDisabled();
  release();
  await expect(page.getByRole("status")).toContainText("If an account exists");
  expect(requests).toBe(1);
  await page.getByRole("link", { name: "Back to sign in" }).click();
  await expect(page).toHaveURL(/\/login$/);
});

test("missing reset token is actionable and recovery remains public with a stored session", async ({
  page,
}) => {
  await page.goto("/reset-password");
  await expect(page.getByRole("alert")).toContainText("invalid or has expired");
  await page.evaluate(() =>
    localStorage.setItem(
      "vellum:session",
      JSON.stringify({
        generation: crypto.randomUUID(),
        revision: crypto.randomUUID(),
        credentials: {
          access_token: "old-session",
          refresh_token: "old-refresh",
          user: { id: "synthetic" },
        },
      }),
    ),
  );
  await page.goto("/forgot-password");
  await expect(
    page.getByRole("heading", { name: "Forgot your password?" }),
  ).toBeVisible();
  await expect(page.getByText("Loading your finance data…")).toHaveCount(0);
});

test("reset keeps token out of URL/storage, validates matching passwords, clears auth and returns to login", async ({
  page,
}) => {
  let calls = 0;
  let release: () => void = () => {};
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route(`${apiURL}/reset-password`, async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    calls++;
    expect(route.request().postDataJSON()).toEqual({
      token,
      new_password: "NewPassword123!",
    });
    expect(route.request().headers().referer).toBeUndefined();
    await pending;
    await route.fulfill({ json: { message: "Password reset successfully." } });
  });
  await page.goto(`/reset-password#token=${token}`);
  await expect(
    page.getByRole("heading", { name: "Reset your password" }),
  ).toBeVisible();
  await expect(page).toHaveURL(/\/reset-password$/);
  expect(
    await page.evaluate(() => ({
      local: JSON.stringify(localStorage),
      session: JSON.stringify(sessionStorage),
    })),
  ).toEqual({ local: "{}", session: "{}" });
  await page
    .getByLabel("New password", { exact: true })
    .fill("NewPassword123!");
  await page
    .getByLabel("Confirm new password", { exact: true })
    .fill("DifferentPassword!");
  await page
    .getByRole("button", { name: "Reset password", exact: true })
    .click();
  await expect(page.getByRole("alert")).toHaveText("Passwords do not match.");
  expect(calls).toBe(0);
  await page
    .getByLabel("Confirm new password", { exact: true })
    .fill("NewPassword123!");
  await page.evaluate(() => {
    localStorage.setItem(
      "vellum:session",
      JSON.stringify({
        generation: crypto.randomUUID(),
        revision: crypto.randomUUID(),
        credentials: {
          access_token: "old-session",
          refresh_token: "old-refresh",
          user: { id: "synthetic" },
        },
      }),
    );
  });
  await page
    .getByRole("button", { name: "Reset password", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Resetting password…" }),
  ).toBeDisabled();
  release();
  await expect(page.getByRole("status")).toContainText(
    "Your password has been reset",
  );
  expect((await storedSession(page)) === null).toBe(true);
  await page.getByRole("link", { name: "Back to sign in" }).click();
  await expect(page).toHaveURL(/\/login$/);
  await page.goBack();
  await expect(page).toHaveURL(/\/reset-password$/);
  // Router history may restore the completed confirmation. It must never
  // restore a bearer token or a redeemable password form.
  await expect(page.getByLabel("New password", { exact: true })).toHaveCount(0);
  expect(calls).toBe(1);
  await page.reload();
  await expect(page.getByRole("alert")).toContainText("invalid or has expired");
  await page.goto("/accounts");
  await expect(page).toHaveURL(/\/login$/);
});

test("expired token and rate limits have accessible feedback", async ({
  page,
}) => {
  await page.route(`${apiURL}/reset-password`, async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    await route.fulfill({
      status: 400,
      json: { error: "Invalid or expired reset token" },
    });
  });
  await page.goto(`/reset-password#token=${token}`);
  await page
    .getByLabel("New password", { exact: true })
    .fill("NewPassword123!");
  await page
    .getByLabel("Confirm new password", { exact: true })
    .fill("NewPassword123!");
  await page
    .getByRole("button", { name: "Reset password", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText("invalid or has expired");
  await page.getByRole("link", { name: "Request a new reset link" }).click();
  await page.route(`${apiURL}/forgot-password`, async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    await route.fulfill({ status: 429, json: { error: "Too many requests" } });
  });
  await page.getByLabel("Email", { exact: true }).fill("person@example.test");
  await page.getByRole("button", { name: "Send reset instructions" }).click();
  await expect(page.getByRole("alert")).toContainText("Too many requests");
});
