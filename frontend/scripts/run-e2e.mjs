import { spawn, execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { mkdtemp, writeFile, rm, mkdir } from "node:fs/promises";
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

function start(executable, args, options, label) {
  const child = spawn(executable, args, {
    ...options,
    detached: true,
    stdio: ["ignore", "pipe", "pipe"],
  });
  processes.push(child);
  for (const stream of [child.stdout, child.stderr])
    stream.on("data", (chunk) => {
      logs += `[${label}] ${chunk}`;
    });
  child.on("error", (error) => {
    logs += `[${label}] ${error.message}\n`;
  });
  return child;
}

async function ready(url, child) {
  const deadline = Date.now() + 60_000;
  while (Date.now() < deadline) {
    if (child.exitCode !== null)
      throw new Error(`Test server exited before ${url} was ready.`);
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
  if (containerStarted) {
    try {
      command("docker", ["rm", "--force", container], { stdio: "pipe" });
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
  command("docker", [
    "run",
    "--detach",
    "--rm",
    "--name",
    container,
    "--publish",
    "127.0.0.1::5432",
    "--env",
    "POSTGRES_HOST_AUTH_METHOD=trust",
    "--env",
    "POSTGRES_DB=vellum_e2e",
    "postgres:17",
  ]);
  containerStarted = true;
  const binding = command("docker", ["port", container, "5432/tcp"]);
  const dbPort = binding.split(":").at(-1);
  if (!dbPort || !/^\d+$/.test(dbPort))
    throw new Error("Cannot determine isolated PostgreSQL port.");
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
  });
  const api = start(
    binary,
    [],
    {
      // Run away from backend/.env; every connection/security setting is synthetic.
      cwd: workspace,
      env: {
        ...process.env,
        PORT: "18080",
        BIND_HOST: "127.0.0.1",
        DB_HOST: "127.0.0.1",
        DB_PORT: dbPort,
        DB_USER: "postgres",
        DB_PASSWORD: "unused-isolated-test",
        DB_NAME: "vellum_e2e",
        DB_SSLMODE: "disable",
        JWT_SECRET: randomUUID() + randomUUID(),
        JWT_EXPIRY: "15m",
        JWT_REFRESH_EXPIRY: "1h",
        CORS_ALLOWED_ORIGINS: "http://localhost:15173",
        GIN_MODE: "release",
      },
    },
    "api",
  );
  await ready("http://localhost:18080/ping", api);
  const web = start(
    process.execPath,
    [
      "node_modules/vite/bin/vite.js",
      "--host",
      "localhost",
      "--port",
      "15173",
      "--strictPort",
    ],
    {
      cwd: frontend,
      env: {
        ...process.env,
        VELLUM_E2E: "1",
        VITE_API_BASE_URL: "http://localhost:18080",
      },
    },
    "web",
  );
  await ready("http://localhost:15173", web);
  const args = process.argv.slice(2);
  const capture = args.includes("--capture-screenshots");
  const testArgs = args.filter((arg) => arg !== "--capture-screenshots");
  if (capture) testArgs.push("--project=screenshots");
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
    await mkdir(join(frontend, "test-results"), { recursive: true });
    await writeFile(join(frontend, "test-results/services.log"), logs);
  }
  process.exitCode = result;
} catch (error) {
  console.error(error.message);
  if (logs) console.error(logs);
  process.exitCode = 1;
} finally {
  await cleanup();
}
