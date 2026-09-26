// Cliente HTTP da API — base same-origin (Nginx serve o SPA e faz proxy /api).
import type { TestResult } from "./types";

const API = "/api/v1";

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

const TOKEN_KEY = "monitor_token";

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(token: string | null) {
  if (token) localStorage.setItem(TOKEN_KEY, token);
  else localStorage.removeItem(TOKEN_KEY);
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body && !(init.body instanceof FormData)) {
    headers.set("Content-Type", "application/json");
  }
  const token = getToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);

  const res = await fetch(`${API}${path}`, { ...init, headers, cache: "no-store" });
  if (res.status === 204) return undefined as T;

  let body: unknown = null;
  const ct = res.headers.get("Content-Type") ?? "";
  if (ct.includes("application/json")) {
    try {
      body = await res.json();
    } catch {
      body = null;
    }
  }

  if (!res.ok) {
    const msg =
      body && typeof body === "object" && "error" in body
        ? String((body as { error: string }).error)
        : `Erro ${res.status}`;
    // 401 (token inválido/expirado) → limpa a sessão; a UI redireciona para login.
    if (res.status === 401 && getToken()) {
      setToken(null);
    }
    throw new ApiError(res.status, msg);
  }
  return body as T;
}

export const api = {
  // Auth
  signup: (body: { email: string; password: string }) =>
    request<{ id: number; email: string }>(`/auth/signup`, { method: "POST", body: JSON.stringify(body) }),
  login: (body: { email: string; password: string }) =>
    request<{ token: string; expires_at: string }>(`/auth/login`, { method: "POST", body: JSON.stringify(body) }),

  // Público
  status: () => request<import("./types").StatusResponse>(`/status`),
  incidents: () => request<{ items: import("./types").Incident[] }>(`/incidents`),
  config: () => request<{ branding: import("./types").Branding }>(`/config`),
  series: (id: number, from: string, to: string, bucket: "minute" | "hour" | "day" = "minute", limit = 1440) =>
    request<{ items: import("./types").SeriesPoint[] }>(
      `/status/${id}/stats/series?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}&bucket=${bucket}&limit=${limit}`,
    ),
  summary: (id: number) => request<import("./types").StatsSummary>(`/status/${id}/stats/summary`),

  // Admin — endpoints
  listEndpoints: () => request<{ items: import("./types").Endpoint[] }>(`/admin/endpoints/`),
  createEndpoint: (body: Record<string, unknown>) =>
    request<import("./types").Endpoint>(`/admin/endpoints/`, { method: "POST", body: JSON.stringify(body) }),
  updateEndpoint: (id: number, body: Record<string, unknown>) =>
    request<import("./types").Endpoint>(`/admin/endpoints/${id}`, { method: "PUT", body: JSON.stringify(body) }),
  deleteEndpoint: (id: number) => request<void>(`/admin/endpoints/${id}`, { method: "DELETE" }),
  testEndpoint: (body: Record<string, unknown>) =>
    request<TestResult>(`/admin/endpoints/test`, { method: "POST", body: JSON.stringify(body) }),

  // Admin — grupos
  listGroups: () => request<{ items: import("./types").Group[] }>(`/admin/groups/`),
  createGroup: (body: Record<string, unknown>) =>
    request<import("./types").Group>(`/admin/groups/`, { method: "POST", body: JSON.stringify(body) }),
  updateGroup: (id: number, body: Record<string, unknown>) =>
    request<import("./types").Group>(`/admin/groups/${id}`, { method: "PUT", body: JSON.stringify(body) }),
  deleteGroup: (id: number) => request<void>(`/admin/groups/${id}`, { method: "DELETE" }),

  // Admin — logs/stats
  listChecks: (params: string) => request<{ items: import("./types").Check[] }>(`/admin/checks?${params}`),
  statsSeries: (endpointId: number, bucket: "minute" | "hour" | "day" = "minute") =>
    request<{ items: import("./types").SeriesPoint[] }>(`/admin/stats/series?endpoint_id=${endpointId}&bucket=${bucket}`),
  statsSummary: (endpointId: number) =>
    request<import("./types").StatsSummary>(`/admin/stats/summary?endpoint_id=${endpointId}`),

  // Admin — notifications/settings
  notifications: (endpointId?: number) =>
    request<{ items: import("./types").Notification[] }>(
      `/admin/notifications${endpointId ? `?endpoint_id=${endpointId}` : ""}`,
    ),
  getSettings: () => request<import("./types").SettingsPayload>(`/admin/settings`),
  putSettings: (body: Partial<import("./types").SettingsPayload>) =>
    request<import("./types").SettingsPayload>(`/admin/settings`, { method: "PUT", body: JSON.stringify(body) }),
};

export const fmtLatency = (ms: number): string => (ms >= 1000 ? `${(ms / 1000).toFixed(2)}s` : `${Math.round(ms)}ms`);

export const fmtUptime = (pct: number): string => `${pct.toFixed(2)}%`;

export const fmtTime = (iso: string): string =>
  new Date(iso).toLocaleString("pt-BR", { dateStyle: "short", timeStyle: "short" });

export const fmtDuration = (ms: number | null): string => {
  if (ms == null) return "—";
  const s = Math.floor(ms / 1000);
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d > 0) return `${d}d ${h}h ${m}m`;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${s % 60}s`;
  return `${s}s`;
};