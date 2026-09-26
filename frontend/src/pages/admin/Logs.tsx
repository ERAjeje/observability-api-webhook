// Logs — checagens brutas paginadas e filtráveis (RF-018, T4.4).
import { useMemo, useState } from "react";
import { useChecks, useEndpoints } from "../../hooks/queries";
import { fmtTime, fmtLatency } from "../../lib/api";
import { Spinner } from "../../components/Spinner";
import clsx from "clsx";

const RESULT_STYLES: Record<string, string> = {
  ok: "text-emerald-300",
  degraded: "text-amber-300",
  fail: "text-rose-300",
};

const PAGE = 25;

export default function LogsPage() {
  const { data: eps } = useEndpoints();
  const [endpointId, setEndpointId] = useState("");
  const [status, setStatus] = useState("");
  const [hours, setHours] = useState("1");
  const [offset, setOffset] = useState(0);

  const params = useMemo(() => {
    const p = new URLSearchParams({ limit: String(PAGE), offset: String(offset) });
    if (endpointId) p.set("endpoint_id", endpointId);
    if (status) p.set("status", status);
    if (hours) {
      const from = new Date(Date.now() - Number(hours) * 3600_000).toISOString();
      p.set("from", from);
    }
    return p.toString();
  }, [endpointId, status, hours, offset]);

  const { data, isLoading } = useChecks(params);
  const items = data?.items ?? [];
  const hasMore = items.length === PAGE;

  const filters = (
    <div className="flex flex-wrap items-end gap-2">
      <label>
        <span className="text-xs font-medium text-slate-400">Endpoint</span>
        <select
          value={endpointId}
          onChange={(e) => {
            setEndpointId(e.target.value);
            setOffset(0);
          }}
          className="mt-1 rounded-lg border border-white/10 bg-slate-950 px-3 py-2 text-sm text-slate-100 outline-none focus:border-sky-400"
        >
          <option value="">Todos</option>
          {(eps?.items ?? []).map((ep) => (
            <option key={ep.id} value={ep.id}>{ep.name}</option>
          ))}
        </select>
      </label>
      <label>
        <span className="text-xs font-medium text-slate-400">Status</span>
        <select
          value={status}
          onChange={(e) => {
            setStatus(e.target.value);
            setOffset(0);
          }}
          className="mt-1 rounded-lg border border-white/10 bg-slate-950 px-3 py-2 text-sm text-slate-100 outline-none focus:border-sky-400"
        >
          <option value="">Todos</option>
          <option value="ok">ok</option>
          <option value="degraded">degraded</option>
          <option value="fail">fail</option>
        </select>
      </label>
      <label>
        <span className="text-xs font-medium text-slate-400">Período</span>
        <select value={hours} onChange={(e) => setHours(e.target.value)} className="mt-1 rounded-lg border border-white/10 bg-slate-950 px-3 py-2 text-sm text-slate-100 outline-none focus:border-sky-400">
          <option value="1">última 1h</option>
          <option value="6">últimas 6h</option>
          <option value="24">últimas 24h</option>
          <option value="">todo o histórico</option>
        </select>
      </label>
      {offset > 0 && (
        <button
          onClick={() => setOffset((o) => Math.max(0, o - PAGE))}
          className="rounded-lg border border-white/10 px-3 py-2 text-sm text-slate-300 hover:bg-white/5"
        >
          ← anterior
        </button>
      )}
      {hasMore && (
        <button
          onClick={() => setOffset((o) => o + PAGE)}
          className="rounded-lg border border-white/10 px-3 py-2 text-sm text-slate-300 hover:bg-white/5"
        >
          próxima →
        </button>
      )}
    </div>
  );

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold text-slate-100">Logs de checagens</h1>
        <span className="text-xs text-slate-500">somente leitura · logs brutos</span>
      </div>
      {filters}

      <div className="overflow-x-auto rounded-xl border border-white/10">
        <table className="w-full text-left text-sm">
          <thead className="bg-slate-900 text-xs uppercase tracking-wide text-slate-500">
            <tr>
              <th className="px-4 py-3">Quando</th>
              <th className="px-4 py-3">Endpoint</th>
              <th className="px-4 py-3">Resultado</th>
              <th className="px-4 py-3">HTTP</th>
              <th className="px-4 py-3 text-right">Latência</th>
              <th className="px-4 py-3">Detalhe</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/5 bg-slate-950/60">
            {items.map((c) => (
              <tr key={c.id} className="hover:bg-white/[0.02]">
                <td className="px-4 py-2.5 text-xs text-slate-400">{fmtTime(c.checked_at)}</td>
                <td className="px-4 py-2.5 text-xs text-slate-300">
                  #{c.endpoint_id} — {eps?.items.find((e) => e.id === c.endpoint_id)?.name ?? ""}
                </td>
                <td className={clsx("px-4 py-2.5 text-xs font-medium", RESULT_STYLES[c.result] ?? "text-slate-400")}>
                  {c.result}
                </td>
                <td className="px-4 py-2.5 text-xs text-slate-400">{c.http_status ?? "—"}</td>
                <td className="px-4 py-2.5 text-right text-xs text-slate-300">{c.latency_ms > 0 ? fmtLatency(c.latency_ms) : "—"}</td>
                <td className="max-w-[260px] truncate px-4 py-2.5 text-xs text-slate-500">{c.error_detail ?? ""}</td>
              </tr>
            ))}
            {isLoading && (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center"><Spinner /></td>
              </tr>
            )}
            {!isLoading && items.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-slate-500">Sem registros para os filtros.</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}