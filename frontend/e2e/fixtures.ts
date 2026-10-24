import {
  test as base,
  expect,
  type Page,
  type APIRequestContext,
} from "@playwright/test";
import { randomUUID } from "node:crypto";

export const apiURL = "http://localhost:18080";
export const appURL = "http://localhost:15173";
export const password = "Synthetic-E2E-Password-2026!";
type Session = {
  access_token: string;
  refresh_token: string;
  user: { first_name: string };
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
  await page.evaluate((value) => {
    localStorage.setItem("vellum:access_token", value.access_token);
    localStorage.setItem("vellum:refresh_token", value.refresh_token);
    localStorage.setItem("vellum:user", JSON.stringify(value.user));
  }, session);
}

export const test = base.extend<{ finance: FinanceAPI }>({
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
