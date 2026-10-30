import { api } from "../client";
import {
  beginSession,
  clearAuth,
  clearAuthGeneration,
  getSession,
  sameSessionRevision,
  saveSession,
  SessionChangedError,
  withAuthLock,
  type AuthSession,
} from "../tokenStorage";
import type {
  AuthResponse,
  LoginRequest,
  RegisterRequest,
} from "../types/index";

async function revoke(refreshToken: string): Promise<void> {
  // Revocation is session-family scoped, including already rotated descendants.
  await api.post(
    "/logout",
    { refresh_token: refreshToken },
    { timeout: 10000 },
  );
}

async function authenticate(
  path: "/login" | "/register",
  request: LoginRequest | RegisterRequest,
): Promise<AuthResponse> {
  const attempt = await beginSession();
  const response = await api.post<AuthResponse>(path, request);
  if (!(await saveSession(attempt, response.data))) {
    // A discarded successful sign-in must not leave its new family usable.
    await revoke(response.data.refresh_token).catch(() => {});
    throw new SessionChangedError();
  }
  return response.data;
}

export function register(request: RegisterRequest): Promise<AuthResponse> {
  return authenticate("/register", request);
}

export function login(request: LoginRequest): Promise<AuthResponse> {
  return authenticate("/login", request);
}

export async function refresh(expected = getSession()): Promise<void> {
  if (!expected?.credentials) throw new SessionChangedError();
  // One rotation per generation across all tabs. Logout only takes the short
  // state lock, so even a response delayed indefinitely cannot block sign-out.
  return withAuthLock(`vellum:refresh:${expected.generation}`, async () => {
    const current = getSession();
    if (current?.generation !== expected.generation || !current.credentials)
      throw new SessionChangedError();
    if (!sameSessionRevision(current, expected)) return;

    let credentials: AuthResponse;
    try {
      const response = await api.post<AuthResponse>(
        "/refresh",
        {
          refresh_token: current.credentials.refresh_token,
        },
        { timeout: 15000 },
      );
      credentials = response.data;
    } catch {
      await clearAuth(current);
      // Even a lost response may have rotated on the server. The predecessor
      // remains a revocation handle for its family.
      await revoke(current.credentials.refresh_token).catch(() => {});
      throw new Error("Session expired, please log in again");
    }
    if (!(await saveSession(current, credentials))) {
      await revoke(credentials.refresh_token).catch(() => {});
      throw new SessionChangedError();
    }
  });
}

export async function logout(): Promise<void> {
  const previous = await clearAuth();
  if (previous?.credentials)
    await revoke(previous.credentials.refresh_token).catch(() => {});
}

export async function forgotPassword(email: string): Promise<void> {
  await api.post("/forgot-password", { email: email.trim() });
}

export async function resetPassword(
  token: string,
  newPassword: string,
): Promise<void> {
  const previous: AuthSession | null = getSession();
  await api.post("/reset-password", { token, new_password: newPassword });
  // Reset revokes server sessions. A response from an earlier reset must not
  // erase a sign-in that completed after that reset was committed.
  if (previous) await clearAuthGeneration(previous.generation);
}
