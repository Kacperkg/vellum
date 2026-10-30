import type { AuthResponse, User } from "./types";

const SESSION_KEY = "vellum:session";
const STATE_LOCK = "vellum:auth-state";
const CHANGE_EVENT = "vellum:session-change";

export type AuthSession = {
  generation: string;
  revision: string;
  credentials: AuthResponse | null;
};

export class SessionChangedError extends Error {
  constructor() {
    super("The authentication session changed. Please try again.");
  }
}

export function getSessionSnapshot(): string | null {
  return localStorage.getItem(SESSION_KEY);
}

export function getSession(): AuthSession | null {
  if (!navigator.locks) return null;
  const value = getSessionSnapshot();
  if (!value) return null;
  try {
    const session = JSON.parse(value) as AuthSession;
    if (
      typeof session.generation !== "string" ||
      typeof session.revision !== "string" ||
      (session.credentials !== null &&
        (typeof session.credentials?.access_token !== "string" ||
          typeof session.credentials?.refresh_token !== "string" ||
          !session.credentials?.user))
    )
      return null;
    return session;
  } catch {
    return null;
  }
}

export function sameSessionRevision(
  current: AuthSession | null,
  expected: AuthSession | null,
): boolean {
  return (
    current?.generation === expected?.generation &&
    current?.revision === expected?.revision
  );
}

export async function withAuthLock<T>(
  name: string,
  action: () => Promise<T> | T,
): Promise<T> {
  if (!navigator.locks) {
    throw new Error(
      "Secure session coordination is unavailable. Please use a current browser over HTTPS.",
    );
  }
  return navigator.locks.request(name, action);
}

function writeSession(session: AuthSession): void {
  // Never read legacy credentials: an older tab can still write those keys.
  for (const key of [
    "vellum:access_token",
    "vellum:refresh_token",
    "vellum:user",
  ])
    localStorage.removeItem(key);
  localStorage.setItem(SESSION_KEY, JSON.stringify(session));
  window.dispatchEvent(new Event(CHANGE_EVENT));
}

function emptySession(): AuthSession {
  return {
    generation: crypto.randomUUID(),
    revision: crypto.randomUUID(),
    credentials: null,
  };
}

// Starting an explicit sign-in invalidates older sign-in attempts and refreshes.
export async function beginSession(): Promise<AuthSession> {
  return withAuthLock(STATE_LOCK, () => {
    const session = emptySession();
    writeSession(session);
    return session;
  });
}

export async function saveSession(
  expected: AuthSession,
  credentials: AuthResponse,
): Promise<boolean> {
  return withAuthLock(STATE_LOCK, () => {
    if (!sameSessionRevision(getSession(), expected)) return false;
    writeSession({
      generation: expected.generation,
      revision: crypto.randomUUID(),
      credentials,
    });
    return true;
  });
}

// A tombstone survives reloads and prevents earlier responses from resurrecting
// credentials. The short state lock never waits for a network request.
export async function clearAuth(
  expected?: AuthSession,
): Promise<AuthSession | null> {
  return withAuthLock(STATE_LOCK, () => {
    const current = getSession();
    if (expected && !sameSessionRevision(current, expected)) return null;
    writeSession(emptySession());
    return current;
  });
}

// Password reset invalidates the entire original generation, including a
// rotation that committed while cleanup was waiting for the state lock.
export async function clearAuthGeneration(generation: string): Promise<void> {
  return withAuthLock(STATE_LOCK, () => {
    if (getSession()?.generation === generation) writeSession(emptySession());
  });
}

export function subscribeAuth(listener: () => void): () => void {
  const onStorage = (event: StorageEvent) => {
    if (event.key === SESSION_KEY || event.key === null) listener();
  };
  window.addEventListener("storage", onStorage);
  window.addEventListener(CHANGE_EVENT, listener);
  return () => {
    window.removeEventListener("storage", onStorage);
    window.removeEventListener(CHANGE_EVENT, listener);
  };
}

export function getAccessToken(): string | null {
  return getSession()?.credentials?.access_token ?? null;
}

export function getRefreshToken(): string | null {
  return getSession()?.credentials?.refresh_token ?? null;
}

export function hasAccessToken(): boolean {
  return !!getAccessToken();
}

export function getStoredUser(): User | null {
  return getSession()?.credentials?.user ?? null;
}
