import type { Page, Route } from "@playwright/test";
import {
  test,
  expect,
  apiURL,
  password,
  signUpAPI,
  openPage,
  signOut,
  expireSession,
  storedSession,
  FinanceAPI,
  type Session,
} from "./fixtures";

function barrier() {
  let release!: () => void;
  const promise = new Promise<void>((resolve) => {
    release = resolve;
  });
  return { promise, release };
}

// route.fetch inherits Firefox's zstd advertisement, but Playwright's API
// response JSON reader does not decode it. Only the test transport requests an
// uncompressed response; the request still runs the real backend transaction.
function fetchAuthResponse(route: Route) {
  return route.fetch({
    headers: { ...route.request().headers(), "accept-encoding": "identity" },
  });
}

async function focus(page: Page) {
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
}

async function signIn(page: Page, email: string) {
  await expect(page).toHaveURL(/\/login$/);
  await page.getByLabel("Email", { exact: true }).fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).not.toHaveURL(/\/login$/);
}

async function flushResponses(page: Page) {
  await page.evaluate(
    () =>
      new Promise((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(resolve)),
      ),
  );
}

test("logout revokes a real rotated session before its delayed response reaches either tab", async ({
  page,
  context,
  request,
  finance,
}) => {
  await finance.create("accounts", { name: "Previous session" });
  await openPage(page, "/accounts", "Accounts");
  const other = await context.newPage();
  await openPage(other, "/accounts", "Accounts");
  const committed = barrier();
  const deliver = barrier();
  const delivered = barrier();
  let successor!: Session;
  await page.route(`${apiURL}/refresh`, async (route) => {
    const response = await fetchAuthResponse(route); // Real PostgreSQL transaction has committed.
    expect(response.status()).toBe(200);
    successor = (await response.json()) as Session;
    committed.release();
    await deliver.promise;
    await route.fulfill({ response });
    delivered.release();
  });
  try {
    await expireSession(page);
    await focus(page);
    await committed.promise;
    const revoked = other.waitForResponse(`${apiURL}/logout`);
    await signOut(other); // Must not wait for the refresh lock/network response.
    await expect(other).toHaveURL(/\/login$/);
    await expect(page).toHaveURL(/\/login$/);
    expect((await revoked).status()).toBe(204);
    expect((await storedSession(page)) === null).toBe(true);
    const refresh = await request.post(`${apiURL}/refresh`, {
      data: { refresh_token: successor.refresh_token },
    });
    expect(refresh.status()).toBe(401);
    const access = await request.get(`${apiURL}/api/accounts`, {
      headers: { Authorization: `Bearer ${successor.access_token}` },
    });
    expect(access.status()).toBe(401);
    deliver.release();
    await delivered.promise;
    await flushResponses(page);
    expect((await storedSession(page)) === null).toBe(true);
    await page.reload();
    await expect(page).toHaveURL(/\/login$/);
    await other.goto("/accounts");
    await expect(other).toHaveURL(/\/login$/);
    await expect(page.locator(".app-frame")).toHaveCount(0);
    await expect(other.locator(".app-frame")).toHaveCount(0);
  } finally {
    deliver.release();
  }
});

test("two tabs share one real refresh and keep the same replacement credentials", async ({
  page,
  context,
  finance,
}) => {
  await finance.create("accounts", { name: "Shared session" });
  await openPage(page, "/accounts", "Accounts");
  const other = await context.newPage();
  await openPage(other, "/accounts", "Accounts");
  const entered = barrier();
  const deliver = barrier();
  let refreshes = 0;
  await context.route(`${apiURL}/refresh`, async (route) => {
    refreshes++;
    entered.release();
    await deliver.promise;
    await route.continue();
  });
  try {
    await expireSession(page);
    await Promise.all([focus(page), focus(other)]);
    await entered.promise;
    deliver.release();
    await expect
      .poll(
        async () =>
          (await storedSession(page))?.access_token !== "expired-test-access",
      )
      .toBe(true);
    await Promise.all([flushResponses(page), flushResponses(other)]);
    expect(refreshes).toBe(1);
    expect(
      JSON.stringify(await storedSession(page)) ===
        JSON.stringify(await storedSession(other)),
    ).toBe(true);
    expect((await storedSession(page)) !== null).toBe(true);
    await page.reload();
    await expect(
      page.getByRole("heading", { name: "Shared session", exact: true }),
    ).toBeVisible();
    await expect(
      other.getByRole("heading", { name: "Shared session", exact: true }),
    ).toBeVisible();
  } finally {
    deliver.release();
  }
});

for (const outcome of ["success", "failure"] as const) {
  test(`a delayed refresh ${outcome} cannot overwrite or clear a newer login in another tab`, async ({
    page,
    context,
    request,
    finance,
  }) => {
    await finance.create("accounts", { name: "Shared account" });
    await openPage(page, "/accounts", "Accounts");
    const other = await context.newPage();
    await openPage(other, "/accounts", "Accounts");
    const entered = barrier();
    const deliver = barrier();
    const delivered = barrier();
    let successor: Session | undefined;
    await page.route(`${apiURL}/refresh`, async (route) => {
      const response =
        outcome === "success" ? await fetchAuthResponse(route) : undefined;
      if (response) {
        expect(response.status()).toBe(200);
        successor = (await response.json()) as Session;
      }
      entered.release();
      await deliver.promise;
      if (response) await route.fulfill({ response });
      else
        await route.fulfill({
          status: 401,
          json: { error: "Expired session" },
        });
      delivered.release();
    });
    try {
      await expireSession(page);
      await focus(page);
      await entered.promise;
      const revoked = other.waitForResponse(`${apiURL}/logout`);
      await signOut(other);
      expect((await revoked).status()).toBe(204);
      await signIn(other, finance.session.user.email);
      await expect(
        other.getByRole("heading", { name: /Good .*Alex/ }),
      ).toBeVisible();
      const accepted = await storedSession(other);
      expect(accepted !== null).toBe(true);
      deliver.release();
      await delivered.promise;
      await flushResponses(page);
      expect(
        JSON.stringify(await storedSession(page)) === JSON.stringify(accepted),
      ).toBe(true);
      await openPage(page, "/accounts", "Accounts");
      await expect(
        page.getByRole("heading", { name: "Shared account", exact: true }),
      ).toBeVisible();
      const usable = await request.get(`${apiURL}/api/accounts`, {
        headers: { Authorization: `Bearer ${accepted!.access_token}` },
      });
      expect(usable.status()).toBe(200);
      if (successor) {
        const rejected = await request.post(`${apiURL}/refresh`, {
          data: { refresh_token: successor.refresh_token },
        });
        expect(rejected.status()).toBe(401);
      }
    } finally {
      deliver.release();
    }
  });
}

test("a delayed login cannot replace a newer login and its discarded server session is revoked", async ({
  page,
  context,
  request,
}) => {
  const account = await signUpAPI(request);
  await page.goto("/login");
  const other = await context.newPage();
  await other.goto("/login");
  const committed = barrier();
  const deliver = barrier();
  let discarded!: Session;
  await page.route(`${apiURL}/login`, async (route) => {
    const response = await fetchAuthResponse(route);
    expect(response.status()).toBe(200);
    discarded = (await response.json()) as Session;
    committed.release();
    await deliver.promise;
    await route.fulfill({ response });
  });
  try {
    await page.getByLabel("Email", { exact: true }).fill(account.email);
    await page.getByLabel("Password", { exact: true }).fill(password);
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await committed.promise;
    await signIn(other, account.email);
    const accepted = await storedSession(other);
    const revoked = page.waitForResponse(`${apiURL}/logout`);
    deliver.release();
    expect((await revoked).status()).toBe(204);
    await flushResponses(page);
    expect(
      JSON.stringify(await storedSession(page)) === JSON.stringify(accepted),
    ).toBe(true);
    const rejected = await request.post(`${apiURL}/refresh`, {
      data: { refresh_token: discarded.refresh_token },
    });
    expect(rejected.status()).toBe(401);
  } finally {
    deliver.release();
  }
});

test("an old protected mutation is never replayed under a different login", async ({
  page,
  context,
  request,
  finance,
}) => {
  await finance.create("accounts", { name: "Old account" });
  const newer = await signUpAPI(request);
  const newAPI = new FinanceAPI(request, newer.session);
  await newAPI.create("accounts", { name: "New account" });
  await openPage(page, "/accounts", "Accounts");
  const other = await context.newPage();
  await openPage(other, "/accounts", "Accounts");
  const entered = barrier();
  const deliver = barrier();
  const delivered = barrier();
  let mutations = 0;
  await page.route(`${apiURL}/api/accounts`, async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    mutations++;
    entered.release();
    await deliver.promise;
    await route.fulfill({ status: 401, json: { error: "Expired access" } });
    delivered.release();
  });
  try {
    await page
      .getByRole("button", { name: "Add account", exact: true })
      .click();
    const dialog = page.getByRole("dialog");
    await dialog.getByLabel("Name", { exact: true }).fill("Never replay this");
    await dialog
      .getByRole("button", { name: "Add account", exact: true })
      .click();
    await entered.promise;
    await signOut(other);
    await signIn(other, newer.email);
    deliver.release();
    await delivered.promise;
    await flushResponses(page);
    expect(mutations).toBe(1);
    const accounts = await newAPI.list<{ name: string }>("accounts");
    expect(accounts.map((account) => account.name)).toEqual(["New account"]);
  } finally {
    deliver.release();
  }
});
