// Hooks de dados (React Query) — separam UI de fetching.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { queryClient } from "../lib/query";
import type { EndpointInput, Group } from "../lib/types";

export function useStatus(enabled = true) {
  return useQuery({ queryKey: ["status"], queryFn: api.status, enabled, refetchInterval: 30_000 });
}

export function useIncidents() {
  return useQuery({ queryKey: ["incidents"], queryFn: api.incidents });
}

export function useConfig() {
  return useQuery({ queryKey: ["config"], queryFn: api.config, staleTime: 60_000 });
}

export function useSummary(endpointId: number | null) {
  return useQuery({
    queryKey: ["summary", endpointId],
    queryFn: () => api.summary(endpointId!),
    enabled: endpointId != null,
    staleTime: 30_000,
  });
}

// isoHoursAgo devolve o ISO de "h" horas atrás — janela das séries (RF-020).
export function isoHoursAgo(h: number): string {
  return new Date(Date.now() - h * 3600_000).toISOString();
}

export function useSeries(endpointId: number | null, bucket: "minute" | "hour" | "day" = "minute", hours = 24) {
  return useQuery({
    // Chave ESTÁVEL (endpoint + bucket). A janela from/to é calculada NA hora
    // do fetch: se ela entrasse na chave, cada render gerava uma chave nova →
    // fetch infinito (gráfico preso em "Carregando…") e rajada de requests.
    queryKey: ["series", endpointId, bucket],
    queryFn: () => api.series(endpointId!, isoHoursAgo(hours), new Date().toISOString(), bucket),
    enabled: endpointId != null,
    staleTime: 30_000,
  });
}

// ─── Admin ────────────────────────────────────────────────────────────────

export function useEndpoints() {
  return useQuery({ queryKey: ["endpoints"], queryFn: api.listEndpoints });
}

export function useGroups() {
  return useQuery({ queryKey: ["groups"], queryFn: api.listGroups });
}

export function useChecks(params: string, enabled = true) {
  return useQuery({ queryKey: ["checks", params], queryFn: () => api.listChecks(params), enabled });
}

export function useStatsSeries(endpointId: number | null, bucket: "minute" | "hour" | "day" = "minute") {
  return useQuery({
    queryKey: ["stats-series", endpointId, bucket],
    queryFn: () => api.statsSeries(endpointId!, bucket),
    enabled: endpointId != null,
  });
}

export function useStatsSummary(endpointId: number | null) {
  return useQuery({
    queryKey: ["stats-summary", endpointId],
    queryFn: () => api.statsSummary(endpointId!),
    enabled: endpointId != null,
  });
}

export function useNotifications(endpointId?: number) {
  return useQuery({ queryKey: ["notifications", endpointId ?? ""], queryFn: () => api.notifications(endpointId) });
}

export function useSettings() {
  return useQuery({ queryKey: ["settings"], queryFn: api.getSettings });
}

// ─── Mutações ─────────────────────────────────────────────────────────────

export function invalidateAll() {
  return queryClient.invalidateQueries();
}

export function useCreateEndpoint() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: EndpointInput) => api.createEndpoint(toEndpointBody(body)),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["endpoints"] }),
  });
}

export function useUpdateEndpoint() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body }: { id: number; body: EndpointInput }) =>
      api.updateEndpoint(id, toEndpointBody(body)),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["endpoints"] }),
  });
}

export function useDeleteEndpoint() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.deleteEndpoint(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["endpoints"] }),
  });
}

export function useToggleEndpoint() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body }: { id: number; body: Record<string, unknown> }) => api.updateEndpoint(id, body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["endpoints"] }),
  });
}

export function useTestEndpoint() {
  return useMutation({
    mutationFn: (body: Record<string, unknown>) => api.testEndpoint(body),
  });
}

export function useCreateGroup() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (g: Group) => api.createGroup({ name: g.name, display_order: g.display_order }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["groups"] }),
  });
}

export function useUpdateGroup() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (g: Group) => api.updateGroup(g.id, { name: g.name, display_order: g.display_order }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["groups"] }),
  });
}

export function useDeleteGroup() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.deleteGroup(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["groups"] }),
  });
}

export function useSaveSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Parameters<typeof api.putSettings>[0]) => api.putSettings(body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["settings"] });
      qc.invalidateQueries({ queryKey: ["config"] });
    },
  });
}

// Converte o input do formulário (snake_case opcional) em body da API.
function toEndpointBody(input: EndpointInput): Record<string, unknown> {
  const body: Record<string, unknown> = { ...input };
  if ("id" in body) delete (body as Record<string, unknown>).id;
  if (!body.interval_seconds) body.interval_seconds = 300;
  if (!body.timeout_ms) body.timeout_ms = 10000;
  if (!body.method) body.method = "GET";
  if (body.headers === undefined) body.headers = {};
  for (const k of ["status", "next_check_at", "created_at", "updated_at"] as const) delete body[k as string];
  return body;
}