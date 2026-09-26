// Formulário de endpoint — cria/edita e oferece teste de conectividade
// antes de salvar (RF-001..005, T3.2).
import { useState, type FormEvent } from "react";
import { useCreateEndpoint, useTestEndpoint, useUpdateEndpoint } from "../hooks/queries";
import type { Endpoint, Group } from "../lib/types";
import { fmtLatency } from "../lib/api";

const METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"];

interface Props {
  initial: Endpoint | null;
  groups: Group[];
  onSaved: () => void;
  onCancel: () => void;
}

function parseHeaders(text: string): Record<string, string> | null {
  const t = text.trim();
  if (!t) return {};
  try {
    const v = JSON.parse(t);
    if (typeof v === "object" && v !== null && !Array.isArray(v)) return v as Record<string, string>;
    return null;
  } catch {
    return null;
  }
}

export function EndpointForm({ initial, groups, onSaved, onCancel }: Props) {
  const [name, setName] = useState(initial?.name ?? "");
  const [url, setUrl] = useState(initial?.url ?? "");
  const [method, setMethod] = useState(initial?.method ?? "GET");
  const [groupId, setGroupId] = useState(initial?.group_id ? String(initial.group_id) : "");
  const [intervalS, setIntervalS] = useState(String(initial?.interval_seconds ?? 300));
  const [timeoutMs, setTimeoutMs] = useState(String(initial?.timeout_ms ?? 10000));
  const [latThr, setLatThr] = useState(String(initial?.latency_threshold_ms ?? 0));
  const [expectStatus, setExpectStatus] = useState(String(initial?.expect_status ?? 0));
  const [expectBody, setExpectBody] = useState(initial?.expect_body ?? "");
  const [headersText, setHeadersText] = useState(initial ? JSON.stringify(initial.headers ?? {}, null, 2) : "");
  const [body, setBody] = useState(initial?.body ?? "");
  const [active, setActive] = useState(initial?.active ?? true);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const create = useCreateEndpoint();
  const update = useUpdateEndpoint();
  const testMut = useTestEndpoint();

  const payload = (): Record<string, unknown> => ({
    name,
    url,
    method,
    group_id: groupId ? Number(groupId) : null,
    interval_seconds: Number(intervalS),
    timeout_ms: Number(timeoutMs),
    latency_threshold_ms: Number(latThr),
    expect_status: Number(expectStatus),
    expect_body: expectBody,
    headers: parseHeaders(headersText) ?? undefined,
    body,
    active,
  });

  async function handleTest() {
    setError(null);
    setNotice(null);
    const h = parseHeaders(headersText);
    if (h === null) {
      setError("headers deve ser JSON de objeto (ex.: {\"Authorization\": \"x\"})");
      return;
    }
    try {
      const r = await testMut.mutateAsync({
        url,
        method,
        headers: h,
        body,
        timeout_ms: Number(timeoutMs) || 10000,
      });
      setNotice(
        r.result === "ok"
          ? `Conectado em ${fmtLatency(r.latency_ms)} (HTTP ${r.http_status})`
          : `Resposta: ${r.result} — ${r.error || `HTTP ${r.http_status}`}`,
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : "Falha no teste");
    }
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setNotice(null);
    if (parseHeaders(headersText) === null) {
      setError("headers deve ser JSON de objeto");
      return;
    }
    try {
      if (initial) await update.mutateAsync({ id: initial.id, body: payload() as never });
      else await create.mutateAsync(payload() as never);
      onSaved();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Falha ao salvar");
    }
  }

  const field = "mt-1 w-full rounded-lg border border-white/10 bg-slate-950 px-3 py-2 text-sm text-slate-100 outline-none focus:border-sky-400";
  const label = "text-xs font-medium text-slate-400";

  return (
    <form onSubmit={handleSubmit} className="space-y-4 rounded-xl border border-white/10 bg-slate-900/60 p-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <label className="block">
          <span className={label}>Nome *</span>
          <input required value={name} onChange={(e) => setName(e.target.value)} className={field} placeholder="api-gateway" />
        </label>
        <label className="block">
          <span className={label}>URL * (http/https)</span>
          <input required type="url" value={url} onChange={(e) => setUrl(e.target.value)} className={field} placeholder="https://api.example.com/health" />
        </label>
        <label className="block">
          <span className={label}>Método</span>
          <select value={method} onChange={(e) => setMethod(e.target.value)} className={field}>
            {METHODS.map((m) => (
              <option key={m} value={m}>{m}</option>
            ))}
          </select>
        </label>
        <label className="block">
          <span className={label}>Grupo</span>
          <select value={groupId} onChange={(e) => setGroupId(e.target.value)} className={field}>
            <option value="">Sem grupo</option>
            {groups.map((g) => (
              <option key={g.id} value={g.id}>{g.name}</option>
            ))}
          </select>
        </label>
        <label className="block">
          <span className={label}>Intervalo (s)</span>
          <input type="number" min={5} required value={intervalS} onChange={(e) => setIntervalS(e.target.value)} className={field} />
        </label>
        <label className="block">
          <span className={label}>Timeout (ms)</span>
          <input type="number" min={100} required value={timeoutMs} onChange={(e) => setTimeoutMs(e.target.value)} className={field} />
        </label>
        <label className="block">
          <span className={label}>Limiar de latência (ms, 0 = off)</span>
          <input type="number" min={0} value={latThr} onChange={(e) => setLatThr(e.target.value)} className={field} />
        </label>
        <label className="block">
          <span className={label}>Status HTTP esperado (0 = 2xx/3xx)</span>
          <input type="number" min={0} value={expectStatus} onChange={(e) => setExpectStatus(e.target.value)} className={field} />
        </label>
      </div>

      <label className="block">
        <span className={label}>Headers (JSON opcional)</span>
        <textarea rows={2} value={headersText} onChange={(e) => setHeadersText(e.target.value)} className={field} placeholder='{"Authorization": "Bearer …"}' />
      </label>
      <label className="block">
        <span className={label}>Body (opcional)</span>
        <textarea rows={2} value={body} onChange={(e) => setBody(e.target.value)} className={field} placeholder="{}" />
      </label>
      <label className="block">
        <span className={label}>Conteúdo esperado na resposta (opcional)</span>
        <input value={expectBody} onChange={(e) => setExpectBody(e.target.value)} className={field} />
      </label>

      <label className="flex items-center gap-2 text-sm text-slate-300">
        <input type="checkbox" checked={active} onChange={(e) => setActive(e.target.checked)} className="accent-sky-400" />
        Ativo (gerar checagens)
      </label>

      {error && <p role="alert" className="text-sm text-rose-300">{error}</p>}
      {notice && <p className="text-sm text-emerald-300">{notice}</p>}

      <div className="flex flex-wrap items-center gap-2">
        <button
          type="submit"
          disabled={create.isPending || update.isPending}
          className="rounded-lg bg-sky-500 px-4 py-2 text-sm font-semibold text-white hover:bg-sky-400 disabled:opacity-50"
        >
          {initial ? "Salvar alterações" : "Criar endpoint"}
        </button>
        <button
          type="button"
          onClick={handleTest}
          disabled={testMut.isPending}
          className="rounded-lg border border-white/10 px-4 py-2 text-sm text-slate-300 hover:bg-white/5 disabled:opacity-50"
        >
          {testMut.isPending ? "Testando…" : "Testar conexão"}
        </button>
        <button type="button" onClick={onCancel} className="rounded-lg px-3 py-2 text-sm text-slate-500 hover:text-slate-300">
          Cancelar
        </button>
      </div>
    </form>
  );
}