import {
  test as base,
  expect,
  type Page,
  type APIRequestContext,
} from "@playwright/test";
import { randomUUID } from "node:crypto";

export const apiURL = "http://localhost:15173";
export const appURL = "http://localhost:15173";
export const password = "Synthetic-E2E-Password-2026!";
export type Session = {
  access_token: string;
  refresh_token: string;
  user: { first_name: string; email: string };
};
type RecordData = Record<string, unknown>;

export class FinanceAPI {
  constructor(
    readonly request: APIRequestContext,
    readonly session: Session,
  ) {}
  async create<T = RecordData>(resource: string, data: RecordData): Promise<T> {
    const response = await this.request.post(`${apiURL}/api/${resource}`, {
      headers: { Authorization: `Bearer ${this.session.access_token}` },
      data,
    });
    expect(response.ok(), `${resource}: ${await response.text()}`).toBeTruthy();
    return response.json() as Promise<T>;
  }
  async list<T = RecordData>(resource: string): Promise<T[]> {
    const response = await this.request.get(`${apiURL}/api/${resource}`, {
      headers: { Authorization: `Bearer ${this.session.access_token}` },
    });
    expect(response.ok()).toBeTruthy();
    return response.json() as Promise<T[]>;
  }
}

export async function signUpAPI(request: APIRequestContext) {
  const email = `e2e-${randomUUID()}@example.test`;
  const response = await request.post(`${apiURL}/register`, {
    data: { first_name: "Alex", last_name: "Demo", email, password },
  });
  expect(response.status()).toBe(201);
  return { email, session: (await response.json()) as Session };
}

export async function enterSession(page: Page, session: Session) {
  await page.goto("/login");
  // The load event can precede the lazy login route's initial auth check. Seed
  // only after it has rendered, so that check cannot race the next navigation.
  await expect(
    page.getByRole("heading", {
      name: "Welcome back.",
      exact: true,
      level: 1,
    }),
  ).toBeVisible();
  await page.evaluate((value) => {
    localStorage.setItem(
      "vellum:session",
      JSON.stringify({
        generation: crypto.randomUUID(),
        revision: crypto.randomUUID(),
        credentials: value,
      }),
    );
    window.dispatchEvent(new Event("vellum:session-change"));
  }, session);
}

export const test = base.extend<{
  finance: FinanceAPI;
  allowCSPViolations: boolean;
  cspAudit: void;
}>({
  allowCSPViolations: [false, { option: true }],
  cspAudit: [
    async ({ page, allowCSPViolations }, use) => {
      const violations: string[] = [];
      // Only these two non-sensitive fields cross into test diagnostics.
      await page.exposeFunction(
        "recordCSPViolation",
        (directive: string, disposition: string) => {
          violations.push(`${disposition}: ${directive}`);
        },
      );
      await page.addInitScript(() => {
        document.addEventListener("securitypolicyviolation", (event) => {
          void (
            window as unknown as {
              recordCSPViolation: (
                directive: string,
                disposition: string,
              ) => Promise<void>;
            }
          ).recordCSPViolation(event.effectiveDirective, event.disposition);
        });
      });
      await use();
      if (!allowCSPViolations)
        expect(violations, "Unexpected CSP violations").toEqual([]);
    },
    { auto: true },
  ],
  finance: async ({ request, page }, provide) => {
    const { session } = await signUpAPI(request);
    await enterSession(page, session);
    await provide(new FinanceAPI(request, session));
  },
});
export { expect };

export async function openPage(page: Page, path: string, title: string) {
  await page.goto(path);
  await expect(
    page.getByRole("heading", { name: title, exact: true, level: 1 }),
  ).toBeVisible();
  await expect(
    page.getByText("Loading your finance data…", { exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByText("Unable to load data", { exact: true }),
  ).toHaveCount(0);
}

export async function addAccount(page: Page, name: string) {
  await page.getByRole("button", { name: "Add account", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "New Account", exact: true });
  await dialog.getByLabel("Name", { exact: true }).fill(name);
  await dialog
    .getByRole("button", { name: "Add account", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
}

export function currentDate() {
  const date = new Date();
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}T12:00:00Z`;
}

export async function signOut(page: Page) {
  const button = page.getByRole("button", { name: "Sign out", exact: true });
  if (!(await button.isVisible()))
    await page
      .getByRole("button", { name: "Open workspace navigation", exact: true })
      .click();
  await button.click();
}

// Simulate an expired/revoked credential without changing login identity.
export async function expireSession(page: Page, revokeRefresh = false) {
  await page.evaluate((revoke) => {
    const state = JSON.parse(localStorage.getItem("vellum:session")!);
    state.credentials.access_token = "expired-test-access";
    if (revoke) state.credentials.refresh_token = "revoked-test-refresh";
    localStorage.setItem("vellum:session", JSON.stringify(state));
  }, revokeRefresh);
}

export async function storedSession(page: Page): Promise<Session | null> {
  return page.evaluate(
    () =>
      JSON.parse(localStorage.getItem("vellum:session") ?? "null")
        ?.credentials ?? null,
  );
}
