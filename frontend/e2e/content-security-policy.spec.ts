import { readFile } from "node:fs/promises";
import { test, expect, openPage, currentDate } from "./fixtures";
import type { Account, Category } from "../src/api/types";

const enforce = process.env.E2E_CSP_MODE === "enforce";
const header = enforce
  ? "content-security-policy"
  : "content-security-policy-report-only";
const config = await readFile(
  new URL("../nginx.prod.conf", import.meta.url),
  "utf8",
);
const policy = config.match(
  /add_header Content-Security-Policy(?:-Report-Only)? "([^"]+)" always;/,
)?.[1];
if (!policy) throw new Error("Missing production CSP");

test("Nginx and both Caddy hosts preserve CSP on entry points, assets and errors", async ({
  request,
}) => {
  const html = await (await request.get("/index.html")).text();
  const asset = html.match(/src="(\/assets\/[^"]+\.js)"/)?.[1];
  expect(asset).toBeTruthy();
  const targets: { origin: string; headers: Record<string, string> }[] = [
    { origin: process.env.E2E_NGINX_URL!, headers: {} },
    { origin: "http://localhost:15173", headers: {} },
    { origin: "http://localhost:15173", headers: { Host: "staging.test" } },
  ];
  for (const target of targets) {
    for (const [path, status] of [
      ["/", 200],
      ["/index.html", 200],
      ["/login", 200],
      ["/forgot-password", 200],
      ["/reset-password", 200],
      ["/analytics", 200],
      ["/nested/spa/route", 200],
      ["/healthz", 200],
      [asset!, 200],
      ["/assets/missing.js", 404],
    ] as const) {
      const response = await request.get(target.origin + path, {
        headers: target.headers,
      });
      expect(response.status()).toBe(status);
      expect(response.headers()[header]).toBe(policy);
      expect(
        response.headers()[
          enforce
            ? "content-security-policy-report-only"
            : "content-security-policy"
        ],
      ).toBeUndefined();
      if (
        status === 200 &&
        path !== "/healthz" &&
        !path.startsWith("/assets/")
      ) {
        // Recovery gets the same privacy headers from both Nginx and Caddy.
        expect(
          new Set(response.headers()["cache-control"].split(/,\s*/)),
        ).toEqual(new Set(["no-store"]));
        expect(
          new Set(response.headers()["referrer-policy"].split(/,\s*/)),
        ).toEqual(new Set(["no-referrer"]));
      }
    }
    const post = await request.post(target.origin + "/index.html", {
      headers: target.headers,
    });
    expect(post.status()).toBe(405);
    expect(post.headers()[header]).toBe(policy);
    const assetResponse = await request.get(target.origin + asset, {
      headers: target.headers,
    });
    expect(assetResponse.headers()["cache-control"]).toContain("immutable");
    const unchanged = await request.get(target.origin + asset, {
      headers: {
        ...target.headers,
        "If-Modified-Since": assetResponse.headers()["last-modified"],
      },
    });
    expect(unchanged.status()).toBe(304);
    expect(unchanged.headers()[header]).toBe(policy);
  }
});

test("production charts, progress and SPA history retain their rendered styles", async ({
  page,
  finance,
}) => {
  const account = await finance.create<Account>("accounts", {
    name: "CSP account",
  });
  const categories = await finance.list<Category>("categories");
  for (const [name, type, amount, category] of [
    ["Salary", "income", 200000, "Income"],
    ["Food", "expense", 25000, "Groceries"],
  ] as const) {
    await finance.create("transactions", {
      name,
      type,
      amount,
      account_id: account.id,
      category_id: categories.find((item) => item.name === category)!.id,
      date: currentDate(),
      frequency: "one-off",
    });
  }
  await finance.create("budgets", {
    category_id: categories.find((item) => item.name === "Groceries")!.id,
    allocated_amount: 50000,
  });
  await openPage(page, "/analytics", "Analytics");
  await expect(page.locator(".donut-chart")).toHaveCSS(
    "background-image",
    /conic-gradient/,
  );
  for (const selector of [".bar-pair .income-bar", ".bar-pair .expense-bar"]) {
    const heights = await page
      .locator(selector)
      .evaluateAll((elements) =>
        elements.map((element) => parseFloat(getComputedStyle(element).height)),
      );
    expect(Math.max(...heights)).toBeGreaterThan(0);
  }
  const menu = page.getByRole("button", {
    name: "Open workspace navigation",
    exact: true,
  });
  if (await menu.isVisible()) await menu.click();
  await page.getByRole("link", { name: "Budgets", exact: true }).click();
  const progress = page.locator(".budget-summary").getByRole("progressbar");
  await expect(progress).toHaveAttribute("aria-valuenow", "50");
  const ratio = await progress.evaluate(
    (element) =>
      element.firstElementChild!.getBoundingClientRect().width /
      element.getBoundingClientRect().width,
  );
  expect(ratio).toBeCloseTo(0.5, 1);
  await page.goBack();
  await expect(
    page.getByRole("heading", { name: "Analytics", exact: true }),
  ).toBeVisible();
  await page.goForward();
  await expect(
    page.getByRole("heading", { name: "Budgets", exact: true }),
  ).toBeVisible();
  await page.reload();
  await expect(progress).toHaveAttribute("aria-valuenow", "50");
});

test.describe("intentional injection", () => {
  test.use({ allowCSPViolations: true });

  test("eval from an allowed script and unrelated inline styles remain restricted", async ({
    page,
  }) => {
    await page.route("**/assets/csp-eval-probe.js", (route) =>
      route.fulfill({
        contentType: "application/javascript",
        body: "try { eval('window.cspEvalRan = true'); } catch { window.cspEvalRan = false; }",
      }),
    );
    await page.goto("/login");
    await page.evaluate(async () => {
      const probe = window as unknown as { violations: string[] };
      probe.violations = [];
      document.addEventListener("securitypolicyviolation", (event) => {
        probe.violations.push(
          `${event.disposition}:${event.effectiveDirective}`,
        );
      });
      const style = document.createElement("style");
      style.textContent = ":root { --csp-block: 1 }";
      document.head.append(style);
      const element = document.createElement("div");
      element.id = "csp-style-probe";
      element.setAttribute("style", "--csp-untrusted: 1");
      document.body.append(element);
      // The fixed WebKit compatibility hash must not permit this other value.
      await new Promise<void>((resolve, reject) => {
        const script = document.createElement("script");
        script.src = "/assets/csp-eval-probe.js";
        script.onload = () => resolve();
        script.onerror = () =>
          reject(new Error("Allowed probe script did not load"));
        document.head.append(script);
      });
    });
    const disposition = enforce ? "enforce" : "report";
    await expect
      .poll(() =>
        page.evaluate(
          () => (window as unknown as { violations: string[] }).violations,
        ),
      )
      .toEqual(
        expect.arrayContaining([
          // WebKit does not emit an eval violation in report-only mode. The
          // execution marker below still verifies report-only semantics; all
          // engines must report AND block eval in the enforcing run.
          ...(enforce ? [`${disposition}:script-src`] : []),
          `${disposition}:style-src-elem`,
          `${disposition}:style-src-attr`,
        ]),
      );
    expect(
      await page.evaluate(
        () => (window as unknown as { cspEvalRan: boolean }).cspEvalRan,
      ),
    ).toBe(!enforce);
    await expect(page.locator("#csp-style-probe")).toHaveCSS(
      "--csp-untrusted",
      enforce ? "" : "1",
    );
    await expect(page.locator("html")).toHaveCSS(
      "--csp-block",
      enforce ? "" : "1",
    );
  });

  test("inline script, handler and foreign connection report or block as configured", async ({
    page,
  }) => {
    await page.goto("/login");
    await page.route("https://csp-probe.invalid/**", (route) =>
      route.fulfill({ body: "synthetic" }),
    );
    await page.evaluate(() => {
      const probe = window as unknown as {
        cspProbe: number;
        violations: string[];
      };
      probe.cspProbe = 0;
      probe.violations = [];
      document.addEventListener("securitypolicyviolation", (event) => {
        probe.violations.push(
          `${event.disposition}:${event.effectiveDirective}`,
        );
      });
      const script = document.createElement("script");
      script.textContent = "window.cspProbe += 1";
      document.body.append(script);
      const button = document.createElement("button");
      button.setAttribute("onclick", "window.cspProbe += 1");
      document.body.append(button);
      button.click();
      void fetch("https://csp-probe.invalid/probe").catch(() => {});
    });
    const disposition = enforce ? "enforce" : "report";
    await expect
      .poll(() =>
        page.evaluate(
          () => (window as unknown as { violations: string[] }).violations,
        ),
      )
      .toEqual(
        expect.arrayContaining([
          `${disposition}:script-src-elem`,
          `${disposition}:script-src-attr`,
          `${disposition}:connect-src`,
        ]),
      );
    expect(
      await page.evaluate(
        () => (window as unknown as { cspProbe: number }).cspProbe,
      ),
    ).toBe(enforce ? 0 : 2);
  });
});
