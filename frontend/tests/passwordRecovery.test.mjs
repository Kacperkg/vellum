import { after, test } from "node:test";
import assert from "node:assert/strict";
import { rm } from "node:fs/promises";
import { join } from "node:path";
import { createRequire } from "node:module";
import { compileModules } from "./helpers/compileModules.mjs";

const directory = await compileModules({
  recovery: new URL(
    "../src/features/auth/utils/resetToken.ts",
    import.meta.url,
  ),
});
after(() => rm(directory, { recursive: true, force: true }));
const require = createRequire(import.meta.url);
const { extractResetToken, passwordError } = require(
  join(directory, "recovery.js"),
);
const token = "A".repeat(43);

test("only one correctly shaped fragment token is accepted", () => {
  assert.equal(
    extractResetToken(
      new URL(`https://example.test/reset-password#token=${token}`),
    ),
    token,
  );
  for (const suffix of [
    "",
    `?token=${token}`,
    "#token=short",
    "#token=" + "!".repeat(43),
    `#token=${token}&token=${token}`,
  ]) {
    assert.equal(
      extractResetToken(
        new URL("https://example.test/reset-password" + suffix),
      ),
      null,
    );
  }
});

test("bootstrap removes query and fragment using replaceState without persisting the token", () => {
  delete require.cache[require.resolve(join(directory, "recovery.js"))];
  const previous = globalThis.window;
  const calls = [];
  globalThis.window = {
    location: {
      pathname: "/reset-password",
      href: `https://example.test/reset-password?tracking=unsafe#token=${token}`,
    },
    history: {
      state: { key: "current" },
      replaceState: (...args) => calls.push(args),
    },
  };
  try {
    const recovery = require(join(directory, "recovery.js"));
    assert.deepEqual(calls, [[{ key: "current" }, "", "/reset-password"]]);
    assert.equal(recovery.getResetToken(), token);
    recovery.clearResetToken();
    assert.equal(recovery.getResetToken(), null);
  } finally {
    if (previous) globalThis.window = previous;
    else delete globalThis.window;
  }
});

test("password validation counts Unicode characters, bounds bcrypt bytes and matches confirmation", () => {
  assert.equal(passwordError("password", "password"), null);
  assert.equal(passwordError("😀".repeat(8), "😀".repeat(8)), null);
  assert.match(passwordError("😀".repeat(4), "😀".repeat(4)), /at least 8/);
  assert.match(passwordError("a".repeat(73), "a".repeat(73)), /72 UTF-8/);
  assert.match(passwordError("😀".repeat(19), "😀".repeat(19)), /72 UTF-8/);
  assert.match(passwordError("password", "different"), /do not match/);
});
