import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { randomUUID } from "node:crypto";
import vm from "node:vm";
import ts from "typescript";

// Execute the actual modules in independent tab realms. Only browser APIs and
// transport are substituted; the session/locking/response code stays intact.
const modules = Object.fromEntries(
  await Promise.all(
    ["tokenStorage", "endpoints/auth", "interceptors"].map(async (name) => [
      name,
      ts.transpileModule(
        await readFile(
          new URL(`../src/api/${name}.ts`, import.meta.url),
          "utf8",
        ),
        {
          compilerOptions: {
            module: ts.ModuleKind.CommonJS,
            target: ts.ScriptTarget.ES2023,
          },
        },
      ).outputText,
    ]),
  ),
);

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

function browser() {
  const values = new Map();
  const queues = new Map();
  const windows = [];
  const locks = {
    async request(name, action) {
      const previous = queues.get(name) ?? Promise.resolve();
      const done = deferred();
      queues.set(name, done.promise);
      await previous;
      try {
        return await action();
      } finally {
        done.resolve();
      }
    },
  };
  function tab(post) {
    const window = new EventTarget();
    windows.push(window);
    const emitStorage = (key) => {
      for (const other of windows) {
        if (other === window) continue;
        const event = new Event("storage");
        event.key = key;
        other.dispatchEvent(event);
      }
    };
    const localStorage = {
      getItem: (key) => values.get(key) ?? null,
      setItem: (key, value) => {
        values.set(key, value);
        emitStorage(key);
      },
      removeItem: (key) => {
        values.delete(key);
        emitStorage(key);
      },
    };
    const responseHandlers = [];
    const api = Object.assign(async (request) => ({ retried: request }), {
      post,
      interceptors: {
        response: { use: (...handlers) => responseHandlers.push(handlers) },
      },
    });
    const context = vm.createContext({
      localStorage,
      window,
      navigator: { locks },
      crypto: { randomUUID },
      Event,
    });
    const exports = {};
    const load = (name) => {
      if (exports[name]) return exports[name];
      const result = {};
      const require = (specifier) => {
        if (specifier === "../client" || specifier === "./client")
          return { api };
        if (specifier === "axios")
          return { isAxiosError: (error) => !!error.isAxiosError };
        if (specifier === "./endpoints/auth") return load("endpoints/auth");
        if (specifier.endsWith("tokenStorage")) return load("tokenStorage");
        throw new Error(`Unexpected dependency: ${specifier}`);
      };
      const run = vm.runInContext(
        `(function(require, exports) { ${modules[name]}\n})`,
        context,
      );
      run(require, result);
      exports[name] = result;
      return result;
    };
    return {
      storage: load("tokenStorage"),
      auth: load("endpoints/auth"),
      loadInterceptors: () => {
        load("interceptors");
        return responseHandlers[0];
      },
      localStorage,
      context,
    };
  }
  return { tab };
}

const credentials = (name) => ({
  access_token: `${name}-access`,
  refresh_token: `${name}-refresh`,
  user: { id: name, email: `${name}@example.test` },
});

async function seed(tab, name = "original") {
  const attempt = await tab.storage.beginSession();
  await tab.storage.saveSession(attempt, credentials(name));
  return tab.storage.getSession();
}

const loginRequest = { email: "synthetic@example.test", password: "test-only" };

test("logout does not wait for refresh delivery and rejects its late credentials across reload", async () => {
  const state = browser();
  const started = deferred();
  const delivery = deferred();
  const revoked = [];
  const tab = state.tab(async (path, body) => {
    if (path === "/refresh") {
      started.resolve();
      return delivery.promise;
    }
    if (path === "/logout") revoked.push(body.refresh_token);
    return {};
  });
  await seed(tab);
  const refresh = tab.auth.refresh();
  const rejection = assert.rejects(refresh, /session changed/);
  await started.promise;
  await tab.auth.logout();
  assert.equal(tab.storage.getAccessToken(), null);
  assert.equal(state.tab(() => {}).storage.getAccessToken(), null);
  delivery.resolve({ data: credentials("rotated") });
  await rejection;
  assert.equal(tab.storage.getAccessToken(), null);
  assert.deepEqual(revoked, ["original-refresh", "rotated-refresh"]);
});

test("a stale refresh failure cannot clear a newer login in another tab", async () => {
  const state = browser();
  const started = deferred();
  const delivery = deferred();
  const old = state.tab(async () => {
    started.resolve();
    return delivery.promise;
  });
  const newer = state.tab(async () => ({ data: credentials("newer") }));
  await seed(old);
  const refresh = old.auth.refresh();
  const rejection = assert.rejects(refresh, /Session expired/);
  await started.promise;
  await newer.auth.login(loginRequest);
  delivery.reject(new Error("old rotation rejected"));
  await rejection;
  assert.equal(old.storage.getAccessToken(), "newer-access");
});

test("two tabs share one rotation and cannot overwrite its replacement", async () => {
  const state = browser();
  const started = deferred();
  const delivery = deferred();
  let rotations = 0;
  const post = async () => {
    rotations++;
    started.resolve();
    return delivery.promise;
  };
  const first = state.tab(post);
  const second = state.tab(post);
  await seed(first);
  const a = first.auth.refresh();
  await started.promise;
  const b = second.auth.refresh();
  delivery.resolve({ data: credentials("rotated") });
  await Promise.all([a, b]);
  assert.equal(rotations, 1);
  assert.equal(first.storage.getAccessToken(), "rotated-access");
  assert.equal(second.storage.getAccessToken(), "rotated-access");
});

test("logout propagates through storage subscribers and clears a reloaded tab", async () => {
  const state = browser();
  const first = state.tab(async () => ({}));
  const second = state.tab(async () => ({}));
  await seed(first);
  const observed = [];
  const unsubscribe = second.storage.subscribeAuth(() =>
    observed.push(second.storage.getAccessToken()),
  );
  await first.auth.logout();
  assert.ok(observed.includes(null));
  assert.equal(state.tab(() => {}).storage.getStoredUser(), null);
  unsubscribe();
});

test("old login success cannot replace a newer login and its discarded family is revoked", async () => {
  const state = browser();
  const started = deferred();
  const delivery = deferred();
  const revoked = [];
  const first = state.tab(async (path, body) => {
    if (path === "/login") {
      started.resolve();
      return delivery.promise;
    }
    revoked.push(body.refresh_token);
    return {};
  });
  const second = state.tab(async () => ({ data: credentials("newer") }));
  const old = first.auth.login(loginRequest);
  const rejection = assert.rejects(old, /session changed/);
  await started.promise;
  await second.auth.login(loginRequest);
  delivery.resolve({ data: credentials("older") });
  await rejection;
  assert.equal(first.storage.getAccessToken(), "newer-access");
  assert.deepEqual(revoked, ["older-refresh"]);
});

test("logout during a pending login prevents its late success from restoring auth", async () => {
  const state = browser();
  const started = deferred();
  const delivery = deferred();
  const tab = state.tab(async (path) => {
    if (path === "/login") {
      started.resolve();
      return delivery.promise;
    }
    return {};
  });
  const login = tab.auth.login(loginRequest);
  const rejection = assert.rejects(login, /session changed/);
  await started.promise;
  await tab.auth.logout();
  delivery.resolve({ data: credentials("late") });
  await rejection;
  assert.equal(tab.storage.getAccessToken(), null);
});

test("stale login failure leaves a newer login intact", async () => {
  const state = browser();
  const started = deferred();
  const delivery = deferred();
  const first = state.tab(async () => {
    started.resolve();
    return delivery.promise;
  });
  const second = state.tab(async () => ({ data: credentials("newer") }));
  const old = first.auth.login(loginRequest);
  const rejection = assert.rejects(old, /rejected/);
  await started.promise;
  await second.auth.login(loginRequest);
  delivery.reject(new Error("rejected"));
  await rejection;
  assert.equal(first.storage.getAccessToken(), "newer-access");
});

test("protected responses and retries cannot cross login generations", async () => {
  const state = browser();
  const tab = state.tab(async () => ({}));
  const previous = await seed(tab);
  const [success, failure] = tab.loadInterceptors();
  await seed(tab, "newer");
  const config = {
    url: "/api/accounts",
    _authGeneration: previous.generation,
    _authRevision: previous.revision,
  };
  assert.throws(() => success({ config }), /session changed/);
  await assert.rejects(
    failure({ isAxiosError: true, response: { status: 401 }, config }),
    /session changed/,
  );
  assert.equal(tab.storage.getAccessToken(), "newer-access");
});

test("old protected 401 retries within the same generation after another tab rotates", async () => {
  const state = browser();
  const tab = state.tab(async () => {
    throw new Error("must not refresh twice");
  });
  const previous = await seed(tab);
  const [, failure] = tab.loadInterceptors();
  await tab.storage.saveSession(previous, credentials("rotated"));
  const config = {
    url: "/api/accounts",
    _authGeneration: previous.generation,
    _authRevision: previous.revision,
  };
  const response = await failure({
    isAxiosError: true,
    response: { status: 401 },
    config,
  });
  assert.equal(response.retried, config);
});

test("missing Web Locks fails closed instead of using an unsafe storage fallback", async () => {
  const tab = browser().tab(async () => {
    throw new Error("network must not start");
  });
  tab.context.navigator.locks = undefined;
  await assert.rejects(
    tab.auth.login(loginRequest),
    /current browser over HTTPS/,
  );
  assert.equal(tab.storage.getAccessToken(), null);
});

test("legacy tab writes cannot restore authentication after logout", async () => {
  const state = browser();
  const tab = state.tab(async () => ({}));
  await seed(tab);
  await tab.auth.logout();
  tab.localStorage.setItem("vellum:access_token", "legacy-stale");
  tab.localStorage.setItem("vellum:refresh_token", "legacy-stale");
  assert.equal(state.tab(() => {}).storage.getAccessToken(), null);
});

test("a lost refresh response revokes the predecessor family before losing its handle", async () => {
  const revoked = [];
  const tab = browser().tab(async (path, body) => {
    if (path === "/refresh") throw new Error("response lost after rotation");
    revoked.push(body.refresh_token);
    return {};
  });
  await seed(tab);
  await assert.rejects(tab.auth.refresh(), /Session expired/);
  assert.equal(tab.storage.getAccessToken(), null);
  assert.deepEqual(revoked, ["original-refresh"]);
});

test("a delayed password-reset response does not clear a subsequent login", async () => {
  const state = browser();
  const delivery = deferred();
  const old = state.tab(async () => delivery.promise);
  const newer = state.tab(async () => ({ data: credentials("newer") }));
  await seed(old);
  const reset = old.auth.resetPassword(
    "synthetic-reset-token",
    "test-password",
  );
  await newer.auth.login(loginRequest);
  delivery.resolve({});
  await reset;
  assert.equal(old.storage.getAccessToken(), "newer-access");
});

test("a stale successful refresh cannot overwrite a newer login", async () => {
  const state = browser();
  const started = deferred();
  const delivery = deferred();
  const revoked = [];
  const old = state.tab(async (path, body) => {
    if (path === "/refresh") {
      started.resolve();
      return delivery.promise;
    }
    revoked.push(body.refresh_token);
    return {};
  });
  const newer = state.tab(async () => ({ data: credentials("newer") }));
  await seed(old);
  const rotation = old.auth.refresh();
  const rejection = assert.rejects(rotation, /session changed/);
  await started.promise;
  await newer.auth.login(loginRequest);
  delivery.resolve({ data: credentials("rotated-old") });
  await rejection;
  assert.equal(old.storage.getAccessToken(), "newer-access");
  assert.deepEqual(revoked, ["rotated-old-refresh"]);
});

test("unsupported browser cannot read existing credentials as authenticated", async () => {
  const tab = browser().tab(async () => ({}));
  await seed(tab);
  tab.context.navigator.locks = undefined;
  assert.equal(tab.storage.hasAccessToken(), false);
  assert.equal(tab.storage.getStoredUser(), null);
});

test("password reset clears its generation even when a queued refresh commits first", async () => {
  const delivery = deferred();
  const tab = browser().tab(async () => delivery.promise);
  const previous = await seed(tab);
  const reset = tab.auth.resetPassword(
    "synthetic-reset-token",
    "test-password",
  );
  const lockHeld = deferred();
  const releaseLock = deferred();
  const locks = tab.context.navigator.locks;
  const blocker = locks.request("vellum:auth-state", () => {
    lockHeld.resolve();
    return releaseLock.promise;
  });
  await lockHeld.promise;

  const resetQueued = deferred();
  const request = locks.request;
  let queued = 0;
  locks.request = (name, action) => {
    const result = request(name, action);
    if (name === "vellum:auth-state" && ++queued === 2) resetQueued.resolve();
    return result;
  };
  const refreshWrite = tab.storage.saveSession(
    previous,
    credentials("rotated"),
  );
  delivery.resolve({});
  await resetQueued.promise;
  releaseLock.resolve();
  await blocker;
  assert.equal(await refreshWrite, true);
  await reset;
  assert.equal(tab.storage.getAccessToken(), null);
  assert.notEqual(tab.storage.getSession().generation, previous.generation);
});
