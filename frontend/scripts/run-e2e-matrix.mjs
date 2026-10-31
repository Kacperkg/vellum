import { spawn, execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { join, resolve } from "node:path";

const frontend = fileURLToPath(new URL("../", import.meta.url));

export function listingArgs(args) {
  const separator = args.indexOf("--");
  const options = separator < 0 ? args : args.slice(0, separator);
  const files = separator < 0 ? [] : args.slice(separator);
  return [
    "node_modules/@playwright/test/cli.js",
    "test",
    ...options,
    "--list",
    "--reporter=json",
    ...files,
  ];
}

export function matrixOptions(args) {
  if (
    args.some(
      (arg) =>
        arg === "--shard" ||
        arg.startsWith("--shard=") ||
        arg === "--last-failed" ||
        arg.startsWith("--last-failed="),
    )
  )
    throw new Error(
      "--shard and --last-failed are not supported by the per-project isolated runner.",
    );
  const matrix = args.includes("--csp-matrix");
  const report = args.includes("--csp-report-only");
  const enforce = args.includes("--csp-enforce");
  if ((report && enforce) || (matrix && (report || enforce)))
    throw new Error("Choose the CSP matrix or one policy mode.");
  return {
    modes: matrix
      ? ["report", "enforce"]
      : [report ? "report" : enforce ? "enforce" : "default"],
    args: args.filter(
      (arg) =>
        !["--csp-matrix", "--csp-report-only", "--csp-enforce"].includes(arg),
    ),
  };
}

export function selectedProjects(report) {
  const selected = new Set();
  function visit(suite) {
    for (const spec of suite.specs ?? [])
      for (const test of spec.tests ?? []) selected.add(test.projectName);
    for (const child of suite.suites ?? []) visit(child);
  }
  for (const suite of report.suites ?? []) visit(suite);
  const projects = report.config.projects
    .map((project) => project.name)
    .filter((name) => selected.has(name));
  if (report.errors?.length || !projects.length)
    throw new Error(
      "Cannot run the browser matrix without selected browser tests.",
    );
  return projects;
}

function testFilters(args) {
  const filters = [];
  for (let index = 0; index < args.length; index++) {
    const arg = args[index];
    if (arg === "--") {
      filters.push(...args.slice(index));
      break;
    }
    // Each child owns exactly one project and a distinct diagnostics directory.
    // The space-separated project option is variadic. Its equals form has
    // one attached value; following positional arguments remain file filters.
    if (arg === "--project") {
      while (index + 1 < args.length && !args[index + 1].startsWith("-"))
        index++;
    } else if (arg === "--output") index++;
    else if (!arg.startsWith("--output=") && !arg.startsWith("--project="))
      filters.push(arg);
  }
  return filters;
}

// A new disposable stack for every project keeps production's per-network
// registration quota from being shared by unrelated browser populations.
// Continue after failures so both policy modes run and every result is retained.
export async function runMatrix(projects, args, run, modes = ["default"]) {
  const failures = [];
  const filters = testFilters(args);
  const separator = filters.indexOf("--");
  const options = separator < 0 ? filters : filters.slice(0, separator);
  const files = separator < 0 ? [] : filters.slice(separator);
  for (const mode of modes) {
    for (const [index, project] of projects.entries()) {
      const name = `${index + 1}-${encodeURIComponent(project)}`;
      const output = join(frontend, "test-results", mode, name);
      const report = join(frontend, "playwright-report", mode, name);
      const task = {
        mode,
        project,
        args: [
          ...(mode === "report"
            ? ["--csp-report-only"]
            : mode === "enforce"
              ? ["--csp-enforce"]
              : []),
          ...options,
          `--project=${project}`,
          `--output=${output}`,
          ...files,
        ],
        output,
        report,
      };
      if ((await run(task)) !== 0) failures.push(`${mode}/${project}`);
    }
  }
  return failures;
}

async function main() {
  const { args, modes } = matrixOptions(process.argv.slice(2));
  const listing = execFileSync(process.execPath, listingArgs(args), {
    cwd: frontend,
    encoding: "utf8",
    env: {
      ...process.env,
      VELLUM_E2E: "1",
      E2E_APP_URL: "http://localhost:15173",
      E2E_API_URL: "http://localhost:18080",
    },
  });
  let active;
  let interrupted = false;
  for (const signal of ["SIGINT", "SIGTERM"])
    process.once(signal, () => {
      interrupted = true;
      active?.kill(signal);
    });
  const failures = await runMatrix(
    selectedProjects(JSON.parse(listing)),
    args,
    async (task) => {
      if (interrupted) throw new Error("Browser matrix interrupted.");
      console.log(`${task.mode} policy: isolated ${task.project} stack`);
      active = spawn(process.execPath, ["scripts/run-e2e.mjs", ...task.args], {
        cwd: frontend,
        stdio: "inherit",
        env: { ...process.env, PLAYWRIGHT_HTML_OUTPUT_DIR: task.report },
      });
      return new Promise((accept, reject) => {
        active.once("error", reject);
        active.once("exit", (code) => accept(code ?? 1));
      });
    },
    modes,
  );
  if (failures.length)
    console.error(`Failed browser projects: ${failures.join(", ")}`);
  process.exitCode = interrupted ? 130 : failures.length ? 1 : 0;
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
)
  main().catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
  });
