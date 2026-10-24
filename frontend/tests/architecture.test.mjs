import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile, readdir, stat } from "node:fs/promises";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

const sourceRoot = fileURLToPath(new URL("../src/", import.meta.url));

async function sourceFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const files = await Promise.all(
    entries.map((entry) => {
      const path = join(directory, entry.name);
      return entry.isDirectory()
        ? sourceFiles(path)
        : /\.tsx?$/.test(path)
          ? [path]
          : [];
    }),
  );
  return files.flat();
}

async function resolveLocal(file, specifier) {
  if (!specifier.startsWith(".") && !specifier.startsWith("@/")) return null;
  const target = specifier.startsWith("@/")
    ? resolve(sourceRoot, specifier.slice(2))
    : resolve(dirname(file), specifier);
  for (const candidate of [
    target,
    `${target}.ts`,
    `${target}.tsx`,
    join(target, "index.ts"),
    join(target, "index.tsx"),
  ]) {
    if (
      await stat(candidate)
        .then((entry) => entry.isFile())
        .catch(() => false)
    )
      return candidate;
  }
  throw new Error(
    `Unresolved import ${specifier} in ${relative(sourceRoot, file)}`,
  );
}

const modules = new Map();
const imports = [];
for (const file of await sourceFiles(sourceRoot)) {
  const source = ts.createSourceFile(
    file,
    await readFile(file, "utf8"),
    ts.ScriptTarget.Latest,
    true,
  );
  const edges = [];
  for (const statement of source.statements) {
    if (
      (!ts.isImportDeclaration(statement) &&
        !ts.isExportDeclaration(statement)) ||
      !statement.moduleSpecifier ||
      !ts.isStringLiteral(statement.moduleSpecifier)
    )
      continue;
    const target = await resolveLocal(file, statement.moduleSpecifier.text);
    if (!target) continue;
    imports.push({
      file: relative(sourceRoot, file),
      target: relative(sourceRoot, target),
    });
    const typeOnly = ts.isImportDeclaration(statement)
      ? statement.importClause?.isTypeOnly ||
        (statement.importClause?.namedBindings &&
          ts.isNamedImports(statement.importClause.namedBindings) &&
          !statement.importClause.name &&
          statement.importClause.namedBindings.elements.every(
            (entry) => entry.isTypeOnly,
          ))
      : statement.isTypeOnly ||
        (statement.exportClause &&
          ts.isNamedExports(statement.exportClause) &&
          statement.exportClause.elements.every((entry) => entry.isTypeOnly));
    if (!typeOnly && /\.tsx?$/.test(target)) edges.push(target);
  }
  modules.set(file, edges);
}

test("every local import resolves after the feature migration", () => {
  assert.ok(modules.size > 0);
  assert.ok(
    imports.some((entry) => entry.target === "assets/styles/globals.css"),
  );
});

test("shared foundations and API infrastructure do not depend on product features", () => {
  for (const { file, target } of imports) {
    if (
      /^(api|contexts|utils)\//.test(file) ||
      file.startsWith("components/")
    ) {
      assert.ok(
        !target.startsWith("features/"),
        `${file} depends on ${target}`,
      );
    }
    if (file.startsWith("components/ui/")) {
      assert.ok(
        !/^(api|contexts|layout|app|routes)\//.test(target),
        `${file} depends on ${target}`,
      );
    }
  }
});

test("features access other features through their public entry points", () => {
  for (const { file, target } of imports) {
    const sourceFeature = /^features\/([^/]+)\//.exec(file)?.[1];
    const targetFeature = /^features\/([^/]+)\//.exec(target)?.[1];
    if (sourceFeature && targetFeature && sourceFeature !== targetFeature) {
      assert.equal(
        target,
        `features/${targetFeature}/index.ts`,
        `${file} reaches into ${target}`,
      );
    }
  }
});

test("runtime imports have no circular dependencies", () => {
  const visited = new Set();
  function visit(file, chain) {
    assert.ok(
      !chain.includes(file),
      `Import cycle: ${[...chain, file].map((path) => relative(sourceRoot, path)).join(" → ")}`,
    );
    if (visited.has(file)) return;
    for (const next of modules.get(file) ?? []) visit(next, [...chain, file]);
    visited.add(file);
  }
  for (const file of modules.keys()) visit(file, []);
});
