const ACCESS_KEY = "vellum:access_token";
const REFRESH_KEY = "vellum:refresh_token";

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

export function clearAuth(): void {
  setAccessToken(null);
  setRefreshToken(null);
}
