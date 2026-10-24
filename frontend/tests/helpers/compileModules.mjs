import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

const sourceRoot = fileURLToPath(new URL("../../src/", import.meta.url));

// Use the installed compiler for native Node tests, rewriting only the declared
// production modules into an isolated CommonJS directory. No app aliases or
// global module-loader hooks are installed in the test process.
export async function compileModules(sources) {
  const directory = await mkdtemp(join(tmpdir(), "vellum-finance-test-"));
  const outputs = new Map(
    Object.entries(sources).map(([name, url]) => [
      fileURLToPath(url),
      `./${name}.js`,
    ]),
  );
  try {
    for (const [name, url] of Object.entries(sources)) {
      const source = await readFile(url, "utf8");
      const result = ts.transpileModule(source, {
        fileName: fileURLToPath(url),
        compilerOptions: {
          module: ts.ModuleKind.CommonJS,
          target: ts.ScriptTarget.ES2023,
        },
        transformers: {
          before: [
            () => (file) => {
              const statements = file.statements.map((statement) => {
                if (
                  !ts.isImportDeclaration(statement) ||
                  statement.importClause?.isTypeOnly ||
                  !ts.isStringLiteral(statement.moduleSpecifier)
                )
                  return statement;
                const specifier = statement.moduleSpecifier.text;
                if (!specifier.startsWith("@/") && !specifier.startsWith("."))
                  return statement;
                const target = specifier.startsWith("@/")
                  ? resolve(sourceRoot, specifier.slice(2))
                  : resolve(dirname(fileURLToPath(url)), specifier);
                const output =
                  outputs.get(target) ?? outputs.get(`${target}.ts`);
                if (!output)
                  throw new Error(`Declare the test dependency ${specifier}`);
                return ts.factory.updateImportDeclaration(
                  statement,
                  statement.modifiers,
                  statement.importClause,
                  ts.factory.createStringLiteral(output),
                  statement.attributes,
                );
              });
              return ts.factory.updateSourceFile(file, statements);
            },
          ],
        },
      });
      await writeFile(join(directory, `${name}.js`), result.outputText);
    }
    return directory;
  } catch (error) {
    await rm(directory, { recursive: true, force: true });
    throw error;
  }
}
