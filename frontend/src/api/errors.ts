import axios from "axios";
import type { ApiErrorResponse } from "./types";

export function apiErrorMessage(error: unknown, fallback: string): string {
  if (axios.isAxiosError<ApiErrorResponse>(error)) {
    return error.response?.data?.error || fallback;
  }
  return fallback;
}
