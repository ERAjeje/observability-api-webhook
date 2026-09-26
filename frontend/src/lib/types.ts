// Tipos alinhados ao contrato JSON da API (snake_case — Fase 3/4).

export type StatusClass = "up" | "down" | "degraded" | "unknown";
export type ResultClass = "ok" | "degraded" | "fail";

// ─── Público (status page) ────────────────────────────────────────────────

export interface PublicEndpoint {
  id: number;
  name: string;
  group_id: number | null;
  group_name: string;
  status: StatusClass;
}

export interface Incident {
  id: number;
  endpoint_id: number;
  started_at: string;
  ended_at: string | null;
  duration_ms: number | null;
  resolution: string | null;
}

export interface StatusResponse {
  generated_at: string;
  endpoints: PublicEndpoint[];
  incidents_open: Incident[];
}

export interface SeriesPoint {
  bucket: string;
  count: number;
  ok_count: number;
  avg_ms: number;
  p50_ms: number;
  p95_ms: number;
  uptime_pct: number;
}

export interface StatsSummary {
  endpoint_id: number;
  uptime: { day: number; week: number; month: number };
  latency_ms: { p50: number; p95: number; avg: number };
}

// ─── Config / settings (RF-023, T4.5) ─────────────────────────────────────

export interface Branding {
  title: string;
  description: string;
  primary_color: string;
}

export interface AlertsSettings {
  enabled: boolean;
  webhook_url: string;
  to_email: string;
  suppression_seconds: number;
}

export interface SettingsPayload {
  branding: Branding | null;
  alerts: AlertsSettings | null;
}

// ─── Admin ────────────────────────────────────────────────────────────────

export interface Endpoint {
  id: number;
  group_id: number | null;
  name: string;
  url: string;
  method: string;
  headers: Record<string, string>;
  body: string;
  interval_seconds: number;
  timeout_ms: number;
  latency_threshold_ms: number;
  expect_status: number;
  expect_body: string;
  active: boolean;
  status: StatusClass;
  next_check_at: string;
  created_at: string;
  updated_at: string;
}

export type EndpointInput = Partial<Endpoint> & {
  name: string;
  url: string;
  method?: string;
};

export interface Group {
  id: number;
  name: string;
  display_order: number;
}

export interface Check {
  id: number;
  endpoint_id: number;
  checked_at: string;
  result: ResultClass;
  http_status: number | null;
  latency_ms: number;
  error_detail: string | null;
}

export interface TestResult {
  result: ResultClass;
  http_status: number;
  latency_ms: number;
  error: string;
}

export interface Notification {
  id: number;
  endpoint_id: number;
  incident_id: number | null;
  channel: string;
  payload: string;
  delivered_at: string | null;
  status: string;
}

// ─── SSE ──────────────────────────────────────────────────────────────────

export interface SSEEvent {
  type: string;
  [key: string]: unknown;
}