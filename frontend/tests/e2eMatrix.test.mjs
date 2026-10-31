import { test } from "node:test";
import assert from "node:assert/strict";
import { relative } from "node:path";
import { fileURLToPath } from "node:url";
import { execFileSync } from "node:child_process";
import {
  matrixOptions,
  listingArgs,
  runMatrix,
  selectedProjects,
} from "../scripts/run-e2e-matrix.mjs";

const frontend = fileURLToPath(new URL("../", import.meta.url));

function listedSelection(args) {
  // Exercise Playwright's actual CLI parser without launching a browser or API.
  const report = execFileSync(process.execPath, listingArgs(args), {
    cwd: frontend,
    encoding: "utf8",
    env: {
      ...process.env,
      VELLUM_E2E: "1",
      E2E_APP_URL: "http://localhost:15173",
      E2E_API_URL: "http://localhost:18080",
    },
  });
  const parsed = JSON.parse(report);
  const tests = [];
  function visit(suite) {
    for (const spec of suite.specs ?? [])
      for (const item of spec.tests ?? [])
        tests.push({
          project: item.projectName,
          file: spec.file,
          id: `${item.projectName}:${spec.file}:${spec.line}:${spec.title}`,
        });
    for (const nested of suite.suites ?? []) visit(nested);
  }
  for (const suite of parsed.suites) visit(suite);
  return { projects: selectedProjects(parsed), tests };
}

test("CSP matrix runs every selected browser in both modes after a child failure and retains each child's diagnostics", async () => {
  const invocations = [];
  const diagnostics = new Map();
  const failures = await runMatrix(
    ["firefox", "mobile"],
    [
      "auth-session.spec.ts",
      "--project=firefox",
      "--project",
      "mobile",
      "--output=../../overwritten",
    ],
    async (task) => {
      const child = invocations.length + 1;
      invocations.push(task);
      // A process failure must not stop later projects or erase its evidence.
      diagnostics.set(task.output, `trace-${child}`);
      diagnostics.set(task.report, `report-${child}`);
      assert.deepEqual(task.args.slice(1, -2), ["auth-session.spec.ts"]);
      assert.deepEqual(task.args.slice(-2), [
        `--project=${task.project}`,
        `--output=${task.output}`,
      ]);
      return child === 1 || child === 4 ? 1 : 0;
    },
    ["report", "enforce"],
  );
  assert.deepEqual(
    invocations.map(({ mode, project }) => `${mode}/${project}`),
    ["report/firefox", "report/mobile", "enforce/firefox", "enforce/mobile"],
  );
  assert.deepEqual(failures, ["report/firefox", "enforce/mobile"]);
  assert.equal(diagnostics.size, 8);
  assert.equal(diagnostics.get(invocations[0].output), "trace-1");
  assert.equal(diagnostics.get(invocations[0].report), "report-1");
});

test("normal browser runs retain the production policy while explicit CSP selections run only the requested modes", async () => {
  assert.deepEqual(matrixOptions(["workflows.spec.ts"]), {
    modes: ["default"],
    args: ["workflows.spec.ts"],
  });
  assert.deepEqual(matrixOptions(["--csp-matrix", "--project=webkit"]), {
    modes: ["report", "enforce"],
    args: ["--project=webkit"],
  });
  for (const [flag, mode] of [
    ["--csp-report-only", "report"],
    ["--csp-enforce", "enforce"],
  ])
    assert.deepEqual(matrixOptions([flag]), { modes: [mode], args: [] });
  assert.throws(() => matrixOptions(["--csp-enforce", "--csp-report-only"]));
  assert.throws(() => matrixOptions(["--csp-matrix", "--csp-enforce"]));
  for (const args of [["--shard", "2/2"], ["--shard=2/2"], ["--last-failed"]]) {
    assert.throws(
      () => matrixOptions(args),
      /not supported by the per-project isolated runner/,
    );
    assert.throws(
      () =>
        execFileSync(
          process.execPath,
          ["scripts/run-e2e-matrix.mjs", ...args],
          { cwd: frontend, stdio: "pipe" },
        ),
      (error) =>
        error.status === 1 &&
        error.stdout.length === 0 &&
        /not supported by the per-project isolated runner/.test(
          String(error.stderr),
        ),
    );
  }
  const children = [];
  await runMatrix(["chromium"], [], async (task) => {
    children.push(task);
    return 0;
  });
  assert.equal(children.length, 1);
  assert.equal(
    children[0].args.some((arg) => arg.startsWith("--csp-")),
    false,
  );
});

test("project discovery honours nested selected tests and fails closed on discovery errors", () => {
  const report = {
    config: {
      projects: [{ name: "firefox" }, { name: "webkit" }, { name: "mobile" }],
    },
    suites: [
      {
        suites: [
          {
            specs: [
              {
                tests: [{ projectName: "mobile" }, { projectName: "firefox" }],
              },
            ],
          },
        ],
      },
    ],
    errors: [],
  };
  assert.deepEqual(selectedProjects(report), ["firefox", "mobile"]);
  assert.throws(() =>
    selectedProjects({ ...report, errors: [{ message: "Invalid filter" }] }),
  );
  assert.throws(() => selectedProjects({ ...report, suites: [] }));
});

test("configured project names cannot escape owned diagnostics directories", async () => {
  await runMatrix(
    ["../../elsewhere", ".", "a/b", "a%2Fb"],
    [],
    async (task) => {
      assert.equal(
        relative(frontend, task.output).startsWith("test-results/"),
        true,
      );
      assert.equal(
        relative(frontend, task.report).startsWith("playwright-report/"),
        true,
      );
      assert.equal(relative(frontend, task.output).split("/").length, 3);
      assert.equal(relative(frontend, task.report).split("/").length, 3);
      return 0;
    },
  );
});

test("variadic project filters and the option terminator select exactly one real Playwright project in each child", async () => {
  for (const [args, expected] of [
    [
      ["auth-session.spec.ts", "--project", "firefox", "webkit"],
      ["firefox", "webkit"],
    ],
    [["--project=firefox", "auth-session.spec.ts"], ["firefox"]],
    [
      ["auth-session.spec.ts", "--project=firefox", "--project=webkit"],
      ["firefox", "webkit"],
    ],
    [
      ["--project", "firefox", "webkit", "--", "auth-session.spec.ts"],
      ["firefox", "webkit"],
    ],
    [
      ["--project=firefox", "--reporter=list", "--", "auth-session.spec.ts"],
      ["firefox"],
    ],
  ]) {
    const selection = listedSelection(args);
    const { projects } = selection;
    assert.deepEqual(projects, expected);
    assert.equal(selection.tests.length > 0, true);
    // Playwright 1.64 does not apply positional file filters after its option
    // terminator. Preserve its actual selected cases in either form below.
    if (!args.includes("--"))
      assert.equal(
        selection.tests.every((item) => item.file === "auth-session.spec.ts"),
        true,
      );
    const failures = await runMatrix(projects, args, async (task) => {
      const child = listedSelection(task.args);
      assert.deepEqual(child.projects, [task.project]);
      assert.deepEqual(
        child.tests.map((item) => item.id).sort(),
        selection.tests
          .filter((item) => item.project === task.project)
          .map((item) => item.id)
          .sort(),
      );
      return 0;
    });
    assert.deepEqual(failures, []);
  }
});
