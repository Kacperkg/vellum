import type { User } from "./types";

const ACCESS_KEY = "vellum:access_token";
const REFRESH_KEY = "vellum:refresh_token";
const USER_KEY = "vellum:user";

export function getAccessToken(): string | null {
  return localStorage.getItem(ACCESS_KEY);
}

export function setAccessToken(v: string | null): void {
  if (v) localStorage.setItem(ACCESS_KEY, v);
  else localStorage.removeItem(ACCESS_KEY);
}

export function getRefreshToken(): string | null {
  return localStorage.getItem(REFRESH_KEY);
}

export function setRefreshToken(v: string | null): void {
  if (v) localStorage.setItem(REFRESH_KEY, v);
  else localStorage.removeItem(REFRESH_KEY);
}

export function hasAccessToken(): boolean {
  return !!getAccessToken();
}

export function getStoredUser(): User | null {
  const value = localStorage.getItem(USER_KEY);
  if (!value) return null;
  try {
    return JSON.parse(value) as User;
  } catch {
    localStorage.removeItem(USER_KEY);
    return null;
  }
}

export function setStoredUser(user: User | null): void {
  if (user) localStorage.setItem(USER_KEY, JSON.stringify(user));
  else localStorage.removeItem(USER_KEY);
}

export function clearAuth(): void {
  setAccessToken(null);
  setRefreshToken(null);
  setStoredUser(null);
}
