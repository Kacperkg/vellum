import process from "node:process";
import { defineConfig, devices } from "@playwright/test";

// Tests are deliberately launched through the isolated stack runner, never
// against a developer's active application or an arbitrary environment URL.
if (
  process.env.VELLUM_E2E !== "1" ||
  process.env.E2E_APP_URL !== "http://localhost:15173" ||
  process.env.E2E_API_URL !== "http://localhost:18080"
) {
  throw new Error("Use pnpm test:e2e to start the isolated Vellum test stack.");
}

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  timeout: 45_000,
  expect: { timeout: 10_000 },
  reporter: [["list"], ["html", { open: "never" }]],
  use: {
    baseURL: process.env.E2E_APP_URL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
  },
  projects: [
    {
      name: "chromium",
      testIgnore: ["**/mobile.spec.ts", "**/screenshots.spec.ts"],
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 1440, height: 1000 },
      },
    },
    {
      name: "mobile",
      testMatch: "**/mobile.spec.ts",
      use: { ...devices["iPhone 13"], defaultBrowserType: "chromium" },
    },
    ...(process.env.VELLUM_CAPTURE_SCREENSHOTS === "1"
      ? [
          {
            name: "screenshots",
            testMatch: "**/screenshots.spec.ts",
            use: {
              ...devices["Desktop Chrome"],
              viewport: { width: 1440, height: 1000 },
            },
          },
        ]
      : []),
  ],
});
