import axios from "axios";
import type { InternalAxiosRequestConfig } from "axios";
import { getSession, SessionChangedError } from "./tokenStorage";

export type SessionRequestConfig = InternalAxiosRequestConfig & {
  _authGeneration?: string | null;
  _authRevision?: string | null;
  _retry?: boolean;
};

export const api = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080",
});

api.interceptors.request.use(
  (config: SessionRequestConfig) => {
    const session = getSession();
    if (config.url?.startsWith("/api/")) {
      if (
        config._authGeneration !== undefined &&
        config._authGeneration !== session?.generation
      )
        throw new SessionChangedError();
      config._authGeneration = session?.generation ?? null;
      config._authRevision = session?.revision ?? null;
    }
    const token = session?.credentials?.access_token;
    if (token) config.headers.set("Authorization", `Bearer ${token}`);
    else config.headers.delete("Authorization");
    return config;
  },
  (error: unknown) => {
    throw error;
  },
  { synchronous: true },
);
