import { redirect } from "@tanstack/react-router";
import { hasAccessToken } from "../api/tokenStorage";

export function requireAuth() {
  if (!hasAccessToken()) {
    throw redirect({ to: "/login" });
  }
}
