// API 封装: 同源请求, 401 时跳转登录。
export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(path, {
    credentials: "same-origin",
    headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
    ...init,
  });
  if (resp.status === 401 && !path.startsWith("/api/v1/auth/")) {
    window.location.hash = "#/login";
    throw new ApiError(401, "unauthorized");
  }
  const body = await resp.json().catch(() => ({}));
  if (!resp.ok) {
    throw new ApiError(resp.status, (body as { error?: string }).error ?? `HTTP ${resp.status}`);
  }
  return body as T;
}

export const api = {
  get: <T>(p: string) => request<T>(p),
  post: <T>(p: string, body?: unknown) =>
    request<T>(p, { method: "POST", body: body === undefined ? undefined : JSON.stringify(body) }),
  put: <T>(p: string, body: unknown) =>
    request<T>(p, { method: "PUT", body: JSON.stringify(body) }),
  del: <T>(p: string) => request<T>(p, { method: "DELETE" }),
};

export interface ServersResp { servers: import("./types").ServerInfo[] }
export interface TagsResp { tags: import("./types").Tag[] }
export interface SessionsResp { sessions: import("./types").SessionInfo[] }
export interface CodesResp { codes: import("./types").RegisterCodeInfo[] }
export interface TargetsResp { ping_targets: import("./types").PingTarget[] }
export interface PingRowsResp { rows: import("./types").PingResult[][]; interval_sec: number }
export interface DetailResp {
  server: import("./types").ServerInfo;
  traffic_monthly: { month: string; rx_bytes: number; tx_bytes: number }[];
}
