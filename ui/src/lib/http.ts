import axios, { type AxiosInstance, type AxiosRequestConfig, type AxiosResponse } from "axios";
import config from "./config";
import { clearSession, getToken } from "./session";

const API_BASE_URL = `${config.baseUrl}v1`;

export const axiosInstance: AxiosInstance = axios.create({
  baseURL: API_BASE_URL,
  headers: { "Content-Type": "application/json" },
});

// Attach the bearer token on every request.
axiosInstance.interceptors.request.use((cfg) => {
  const token = getToken();
  if (token) cfg.headers.Authorization = `Bearer ${token}`;
  return cfg;
});

// A missing/expired/revoked token answers 401 (API.md). There is no refresh
// flow — the user signs in again — so clear the session and bounce to /login.
axiosInstance.interceptors.response.use(
  (r) => r,
  (error) => {
    const status = error?.response?.status;
    const url = String(error?.config?.url ?? "");
    const isAuthCall = url.includes("/auth/");
    if (status === 401 && !isAuthCall) {
      clearSession();
      if (!window.location.pathname.startsWith("/login")) {
        window.location.href = "/login";
      }
    }
    return Promise.reject(error);
  },
);

/** Error thrown for a failed API call, carrying the server's code if any. */
export class ApiError extends Error {
  constructor(
    message: string,
    readonly status?: number,
    readonly code?: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export function isNotFound(err: unknown): boolean {
  return err instanceof ApiError && err.status === 404;
}

// The bee-saas JSON envelope: { success, data, message } on success,
// { success:false, error, code } on failure. Raw responses (SSE, files,
// transcript) don't go through here.
function unwrap<T>(res: AxiosResponse): T {
  const body = res.data as unknown;
  if (body && typeof body === "object" && "success" in body) {
    const env = body as { success: boolean; data?: unknown; error?: string; code?: string };
    if (env.success) return env.data as T;
    throw new ApiError(env.error ?? "Request failed", res.status, env.code);
  }
  return body as T;
}

function toApiError(err: unknown): ApiError {
  if (axios.isAxiosError(err)) {
    const data = err.response?.data as { error?: string; code?: string } | undefined;
    if (data?.error) return new ApiError(data.error, err.response?.status, data.code);
    if (!err.response) return new ApiError("Couldn't reach the Keen server.");
    return new ApiError(`The server returned ${err.response.status}.`, err.response.status);
  }
  return new ApiError(err instanceof Error ? err.message : "Something went wrong.");
}

async function run<T>(cfg: AxiosRequestConfig): Promise<T> {
  try {
    return unwrap<T>(await axiosInstance(cfg));
  } catch (err) {
    if (err instanceof ApiError) throw err;
    throw toApiError(err);
  }
}

export const http = {
  get: <T>(url: string, cfg?: AxiosRequestConfig) => run<T>({ ...cfg, method: "get", url }),
  post: <T>(url: string, data?: unknown, cfg?: AxiosRequestConfig) =>
    run<T>({ ...cfg, method: "post", url, data }),
  patch: <T>(url: string, data?: unknown, cfg?: AxiosRequestConfig) =>
    run<T>({ ...cfg, method: "patch", url, data }),
  del: <T>(url: string, cfg?: AxiosRequestConfig) => run<T>({ ...cfg, method: "delete", url }),
};

/** Absolute URL for raw (non-JSON) endpoints: SSE, file downloads, transcript. */
export const apiUrl = (path: string): string =>
  `${API_BASE_URL}${path.startsWith("/") ? path : `/${path}`}`;
