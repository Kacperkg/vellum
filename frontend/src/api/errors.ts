import axios from "axios";
import type { ApiErrorResponse } from "./types";

export function apiErrorMessage(error: unknown, fallback: string): string {
  if (axios.isAxiosError<ApiErrorResponse>(error)) {
    const message = error.response?.data?.error || fallback;
    if (message.includes("money must remain within")) {
      return "The amount or resulting account summary exceeds the supported monetary range (±90071992547409.91). Reduce the amount or linked goal allocation.";
    }
    return message;
  }
  return fallback;
}
