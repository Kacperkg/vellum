import { api } from "../client";
import {
  clearAuth,
  getRefreshToken,
  setAccessToken,
  setRefreshToken,
  setStoredUser,
} from "../tokenStorage";
import type {
  AuthResponse,
  LoginRequest,
  RegisterRequest,
} from "../types/index";

const loginPath = "/login";
const logoutPath = "/logout";
const refreshPath = "/refresh";
const registerPath = "/register";

export async function register(
  request: RegisterRequest,
): Promise<AuthResponse> {
  const response = await api.post<AuthResponse>(registerPath, request);
  setAccessToken(response.data.access_token);
  setRefreshToken(response.data.refresh_token);
  setStoredUser(response.data.user);
  return response.data;
}

export async function login(request: LoginRequest): Promise<AuthResponse> {
  const response = await api.post<AuthResponse>(loginPath, request);
  setAccessToken(response.data.access_token);
  setRefreshToken(response.data.refresh_token);
  setStoredUser(response.data.user);
  return response.data;
}

export async function refresh(): Promise<void> {
  const token = getRefreshToken();
  if (!token) {
    clearAuth();
    throw new Error("No refresh token available");
  }
  try {
    const response = await api.post<AuthResponse>(refreshPath, {
      refresh_token: token,
    });
    setAccessToken(response.data.access_token);
    setRefreshToken(response.data.refresh_token);
    setStoredUser(response.data.user);
  } catch {
    clearAuth();
    throw new Error("Session expired, please log in again");
  }
}

export async function logout(): Promise<void> {
  const token = getRefreshToken();
  clearAuth();
  if (token) {
    await api.post(logoutPath, { refresh_token: token }).catch(() => {});
  }
}

export async function forgotPassword(email: string): Promise<void> {
  await api.post("/forgot-password", { email: email.trim() });
}

export async function resetPassword(
  token: string,
  newPassword: string,
): Promise<void> {
  await api.post("/reset-password", { token, new_password: newPassword });
  // The reset revokes all sessions, including a session in this browser.
  clearAuth();
}
