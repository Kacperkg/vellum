import { spawn, execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { mkdtemp, readFile, writeFile, rm, mkdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createServer } from "node:net";
import { setTimeout as delay } from "node:timers/promises";

const frontend = fileURLToPath(new URL("../", import.meta.url));
const backend = resolve(frontend, "../backend");
const workspace = await mkdtemp(join(tmpdir(), "vellum-e2e-"));
const container = `vellum-e2e-${randomUUID()}`;
const processes = [];
let containerStarted = false;
let networkStarted = false;
const network = `${container}-network`;
const webContainers = [];
const args = process.argv.slice(2);
if (args.includes("--csp-enforce") && args.includes("--csp-report-only"))
  throw new Error("Choose one CSP mode.");
const requestedMode = args.includes("--csp-enforce")
  ? "enforce"
  : args.includes("--csp-report-only")
    ? "report"
    : undefined;
let enforce = requestedMode === "enforce";
let cleanupPromise;
let logs = "";

function command(executable, args, options = {}) {
  return execFileSync(executable, args, {
    encoding: "utf8",
    ...options,
  }).trim();
}

async function unusedPort(port) {
  const server = createServer();
  await new Promise((accept, reject) => {
    server.once("error", () =>
      reject(
        new Error(
          `Port ${port} is occupied. E2E refuses to reuse an existing server.`,
        ),
      ),
    );
    server.listen(port, () => server.close(accept));
  });
}

async function ready(url) {
  const deadline = Date.now() + 60_000;
  while (Date.now() < deadline) {
    try {
      if ((await fetch(url, { signal: AbortSignal.timeout(1000) })).ok) return;
    } catch {
      /* starting */
    }
    await delay(200);
  }
  throw new Error(`Timed out starting ${url}`);
}

function cleanup() {
  cleanupPromise ??= stopStack();
  return cleanupPromise;
}

async function stopStack() {
  for (const child of processes.reverse()) {
    if (!child.pid) continue;
    try {
      process.kill(-child.pid, "SIGTERM");
    } catch {
      /* already stopped */
    }
  }
  await delay(250);
  for (const child of processes) {
    if (!child.pid) continue;
    try {
      process.kill(-child.pid, "SIGKILL");
    } catch {
      /* already stopped */
    }
  }
  for (const name of webContainers.reverse()) {
    try {
      command("docker", ["rm", "--force", name], { stdio: "pipe" });
    } catch {
      /* already removed */
    }
  }
  if (containerStarted) {
    try {
      command("docker", ["rm", "--force", container], { stdio: "pipe" });
    } catch {
      /* already removed */
    }
  }
  if (networkStarted) {
    try {
      command("docker", ["network", "rm", network], { stdio: "pipe" });
    } catch {
      /* already removed */
    }
  }
  await rm(workspace, { recursive: true, force: true });
}

for (const signal of ["SIGINT", "SIGTERM"])
  process.on(signal, () => {
    void cleanup().then(() => process.exit(130));
  });

try {
  await unusedPort(18080);
  await unusedPort(15173);
  console.log(
    "Starting isolated PostgreSQL and Vellum servers (ports 18080/15173)…",
  );
  command("docker", ["network", "create", network]);
  networkStarted = true;
  command("docker", [
    "run",
    "--detach",
    "--rm",
    "--name",
    container,
    "--network",
    network,
    "--env",
    "POSTGRES_HOST_AUTH_METHOD=trust",
    "--env",
    "POSTGRES_DB=vellum_e2e",
    "postgres:17",
  ]);
  containerStarted = true;
  let databaseReady = false;
  for (let attempt = 0; attempt < 150; attempt++) {
    try {
      command("docker", ["exec", container, "pg_isready", "-U", "postgres"], {
        stdio: "pipe",
      });
      databaseReady = true;
      break;
    } catch {
      await delay(200);
    }
  }
  if (!databaseReady)
    throw new Error("Isolated PostgreSQL did not become ready.");
  const binary = join(workspace, "vellum-server");
  command("go", ["build", "-o", binary, "./cmd/server"], {
    cwd: backend,
    stdio: "pipe",
    env: {
      ...process.env,
      CGO_ENABLED: "0",
      GOOS: "linux",
      GOARCH: command("docker", ["version", "--format", "{{.Server.Arch}}"]),
    },
  });
  const apiName = `${container}-api`;
  webContainers.push(apiName);
  // These settings never inherit credentials, dotenv files or SES delivery.
  const apiEnv = {
    PORT: "8080",
    BIND_HOST: "0.0.0.0",
    DB_HOST: container,
    DB_PORT: "5432",
    DB_USER: "postgres",
    DB_PASSWORD: "unused-isolated-test",
    DB_NAME: "vellum_e2e",
    DB_SSLMODE: "disable",
    JWT_SECRET: randomUUID() + randomUUID(),
    JWT_EXPIRY: "15m",
    JWT_REFRESH_EXPIRY: "1h",
    CORS_ALLOWED_ORIGINS: "http://localhost:15173",
    GIN_MODE: "release",
    PASSWORD_RESET_EMAIL_MODE: "disabled",
  };
  command("docker", [
    "run",
    "--detach",
    "--rm",
    "--name",
    apiName,
    "--network",
    network,
    "--publish",
    "127.0.0.1:18080:8080",
    "--user",
    "65532:65532",
    "--read-only",
    "--cap-drop",
    "ALL",
    ...Object.entries(apiEnv).flatMap(([key, value]) => [
      "--env",
      `${key}=${value}`,
    ]),
    "--mount",
    `type=bind,src=${binary},dst=/vellum-server,readonly`,
    "--entrypoint",
    "/vellum-server",
    "caddy:2-alpine",
  ]);
  await ready("http://localhost:18080/ping");
  // Match the production build, Nginx runtime and Caddy routing. No Vite HMR
  // exceptions or cross-origin API allowance may conceal CSP regressions.
  command(process.execPath, ["node_modules/typescript/bin/tsc", "-b"], {
    cwd: frontend,
  });
  command(process.execPath, ["node_modules/vite/bin/vite.js", "build"], {
    cwd: frontend,
    env: { ...process.env, VELLUM_E2E: "1", VITE_API_BASE_URL: "/" },
  });
  const dockerfile = await readFile(join(frontend, "Dockerfile.prod"), "utf8");
  const runtime = dockerfile.match(/^FROM (\S+) AS runtime$/m)?.[1];
  if (!runtime) throw new Error("Cannot find pinned production Nginx runtime.");
  let nginx = await readFile(join(frontend, "nginx.prod.conf"), "utf8");
  const headers = [
    ...nginx.matchAll(
      /add_header (Content-Security-Policy(?:-Report-Only)?) "/g,
    ),
  ];
  if (headers.length !== 1)
    throw new Error("Expected exactly one production CSP header.");
  if (!requestedMode) enforce = headers[0][1] === "Content-Security-Policy";
  nginx = nginx.replace(
    headers[0][1],
    enforce ? "Content-Security-Policy" : "Content-Security-Policy-Report-Only",
  );
  const nginxPath = join(workspace, "nginx.conf");
  await writeFile(nginxPath, nginx);
  const nginxName = `${container}-nginx`;
  webContainers.push(nginxName);
  command("docker", [
    "run",
    "--detach",
    "--rm",
    "--name",
    nginxName,
    "--network",
    network,
    "--publish",
    "127.0.0.1::8080",
    "--mount",
    `type=bind,src=${nginxPath},dst=/etc/nginx/conf.d/default.conf,readonly`,
    "--mount",
    `type=bind,src=${join(frontend, "dist")},dst=/usr/share/nginx/html,readonly`,
    "--entrypoint",
    "nginx",
    runtime,
    "-g",
    "daemon off;",
  ]);
  const nginxPort = command("docker", ["port", nginxName, "8080/tcp"])
    .split(":")
    .at(-1);
  let caddy = await readFile(
    resolve(frontend, "../deploy/Caddyfile.example"),
    "utf8",
  );
  for (const [from, to] of [
    ["staging.vellum.kacpergajdarski.com {", "http://staging.test:8080 {"],
    ["\nvellum.kacpergajdarski.com {", "\nhttp://localhost:8080 {"],
    ["vellum-production-backend:8080", `${apiName}:8080`],
    ["vellum-staging-backend:8080", `${apiName}:8080`],
    ["vellum-production-frontend:8080", `${nginxName}:8080`],
    ["vellum-staging-frontend:8080", `${nginxName}:8080`],
  ]) {
    if (!caddy.includes(from))
      throw new Error(`Missing Caddy test substitution: ${from}`);
    caddy = caddy.replaceAll(from, to);
  }
  const caddyPath = join(workspace, "Caddyfile");
  await writeFile(caddyPath, "{\n admin off\n}\n" + caddy);
  const caddyName = `${container}-caddy`;
  webContainers.push(caddyName);
  command("docker", [
    "run",
    "--detach",
    "--rm",
    "--name",
    caddyName,
    "--network",
    network,
    "--publish",
    "127.0.0.1:15173:8080",
    "--mount",
    `type=bind,src=${caddyPath},dst=/etc/caddy/Caddyfile,readonly`,
    "caddy:2-alpine",
    "caddy",
    "run",
    "--config",
    "/etc/caddy/Caddyfile",
  ]);
  await ready("http://localhost:15173/login");
  // Header checks also exercise Nginx directly (not only proxy output).
  process.env.E2E_NGINX_URL = `http://127.0.0.1:${nginxPort}`;
  const capture = args.includes("--capture-screenshots");
  const testArgs = args.filter(
    (arg) =>
      !["--capture-screenshots", "--csp-enforce", "--csp-report-only"].includes(
        arg,
      ),
  );
  if (capture) testArgs.push("--project=screenshots");
  const outputIndex = testArgs.findLastIndex(
    (arg) => arg === "--output" || arg.startsWith("--output="),
  );
  const diagnostics = resolve(
    frontend,
    outputIndex < 0
      ? "test-results"
      : testArgs[outputIndex] === "--output"
        ? testArgs[outputIndex + 1]
        : testArgs[outputIndex].slice("--output=".length),
  );
  const tests = spawn(
    process.execPath,
    ["node_modules/@playwright/test/cli.js", "test", ...testArgs],
    {
      cwd: frontend,
      detached: true,
      stdio: "inherit",
      env: {
        ...process.env,
        VELLUM_E2E: "1",
        E2E_API_URL: "http://localhost:18080",
        E2E_APP_URL: "http://localhost:15173",
        E2E_CSP_MODE: enforce ? "enforce" : "report",
        VELLUM_CAPTURE_SCREENSHOTS: capture ? "1" : "0",
      },
    },
  );
  processes.push(tests);
  const result = await new Promise((accept, reject) => {
    tests.once("error", reject);
    tests.once("exit", (code) => accept(code ?? 1));
  });
  if (result !== 0) {
    for (const name of webContainers) {
      try {
        logs += command("docker", ["logs", name], {
          stdio: ["ignore", "pipe", "pipe"],
        });
      } catch {
        /* startup failure */
      }
    }
    await mkdir(diagnostics, { recursive: true });
    await writeFile(join(diagnostics, "services.log"), logs);
  }
  process.exitCode = result;
} catch (error) {
  console.error(error.message);
  for (const name of webContainers) {
    try {
      console.error(
        command("docker", ["logs", name], {
          stdio: ["ignore", "pipe", "pipe"],
        }),
      );
    } catch {
      /* startup failure */
    }
  }
  if (logs) console.error(logs);
  process.exitCode = 1;
} finally {
  await cleanup();
}
