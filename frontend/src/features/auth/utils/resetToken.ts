// This module runs before router creation. Fragments never reach the HTTP server;
// discard all URL parameters before rendering or navigating within the app.
export function extractResetToken(url: URL): string | null {
  const values = new URLSearchParams(url.hash.slice(1)).getAll("token");
  return values.length === 1 && /^[A-Za-z0-9_-]{43}$/.test(values[0])
    ? values[0]
    : null;
}

let resetToken: string | null = null;
if (
  typeof window !== "undefined" &&
  window.location.pathname === "/reset-password"
) {
  resetToken = extractResetToken(new URL(window.location.href));
  window.history.replaceState(
    window.history.state,
    "",
    window.location.pathname,
  );
}

export function getResetToken(): string | null {
  return resetToken;
}

export function clearResetToken(): void {
  resetToken = null;
}

export function passwordError(
  password: string,
  confirmation: string,
): string | null {
  if (Array.from(password).length < 8) return "Use at least 8 characters.";
  if (new TextEncoder().encode(password).length > 72)
    return "Use a password of at most 72 UTF-8 bytes.";
  if (password !== confirmation) return "Passwords do not match.";
  return null;
}
