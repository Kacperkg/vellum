import axios from "axios";
import { api, type SessionRequestConfig } from "./client";
import { refresh } from "./endpoints/auth";
import { getSession, SessionChangedError } from "./tokenStorage";

function isCurrentSession(request: SessionRequestConfig): boolean {
  const session = getSession();
  return (
    !!session?.credentials && request._authGeneration === session.generation
  );
}

api.interceptors.response.use(
  (response) => {
    if (
      response.config.url?.startsWith("/api/") &&
      !isCurrentSession(response.config as SessionRequestConfig)
    )
      throw new SessionChangedError();
    return response;
  },
  async (error: unknown) => {
    if (!axios.isAxiosError(error)) return Promise.reject(error);
    const request = error.config as SessionRequestConfig | undefined;
    if (
      error.response?.status !== 401 ||
      !request?.url?.startsWith("/api/") ||
      request._retry
    )
      return Promise.reject(error);

    if (!isCurrentSession(request)) throw new SessionChangedError();
    request._retry = true;
    const session = getSession()!;
    // Only a rotation within the same session can authorise this request again.
    // Never replay a mutation belonging to a different login/account.
    if (request._authRevision === session.revision) await refresh(session);
    if (!isCurrentSession(request)) throw new SessionChangedError();
    return api(request);
  },
);
