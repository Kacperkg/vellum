import axios from "axios";
import type { InternalAxiosRequestConfig } from "axios";
import { api } from "./client";
import { refresh } from "./endpoints/auth";

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

    if (!refreshPromise) {
      refreshPromise = refresh().finally(() => {
        refreshPromise = null;
      });
    }

    await refreshPromise;

    return api(originalRequest);
  },
);
