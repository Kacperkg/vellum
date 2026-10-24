import axios from "axios";
import type { InternalAxiosRequestConfig } from "axios";
import { api } from "./client";
import { refresh } from "./endpoints/auth";
import { getAccessToken } from "./tokenStorage";

type RetryableRequestConfig = InternalAxiosRequestConfig & {
  _retry?: boolean;
};

let refreshPromise: Promise<void> | null = null;

api.interceptors.response.use(
  (response) => response,

  async (error: unknown) => {
    if (!axios.isAxiosError(error)) {
      return Promise.reject(error);
    }

    const status = error.response?.status;
    const originalRequest = error.config as RetryableRequestConfig | undefined;

    const isProtectedRequest = originalRequest?.url?.startsWith("/api/");

    if (status !== 401 || !isProtectedRequest || !originalRequest) {
      return Promise.reject(error);
    }

    if (originalRequest._retry) {
      return Promise.reject(error);
    }

    originalRequest._retry = true;

    // A slow response can report 401 after another request already rotated the
    // token. Retry it with the current token instead of rotating a second time.
    const currentToken = getAccessToken();
    if (
      currentToken &&
      originalRequest.headers.get("Authorization") !== `Bearer ${currentToken}`
    ) {
      return api(originalRequest);
    }

    if (!refreshPromise) {
      refreshPromise = refresh().finally(() => {
        refreshPromise = null;
      });
    }

    try {
      await refreshPromise;
    } catch (refreshError) {
      window.location.assign("/login");
      return Promise.reject(refreshError);
    }

    return api(originalRequest);
  },
);
