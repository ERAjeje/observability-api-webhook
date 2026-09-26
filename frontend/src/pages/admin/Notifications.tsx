// Notificações — auditoria de alertas (RF-029, T4.5).
import { useState } from "react";
import { useEndpoints, useNotifications } from "../../hooks/queries";
import { fmtTime } from "../../lib/api";
import { Spinner } from "../../components/Spinner";
import clsx from "clsx";

export default function NotificationsPage() {
  const { data: eps } = useEndpoints();
  const [endpointId, setEndpointId] = useState("");
  const { data, isLoading } = useNotifications(endpointId ? Number(endpointId) : undefined);
  const [openId, setOpenId] = useState<number | null>(null);

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <h1 className="text-lg font-semibold text-slate-100">Auditoria de notificações</h1>
        <select
          value={endpointId}
          onChange={(e) => setEndpointId(e.target.value)}
          className="rounded-lg border border-white/10 bg-slate-950 px-3 py-2 text-sm text-slate-100 outline-none focus:border-sky-400"
        >
          <option value="">Todos os endpoints</option>
          {(eps?.items ?? []).map((ep) => (
            <option key={ep.id} value={ep.id}>{ep.name}</option>
          ))}
        </select>
      </div>

      <div className="rounded-xl border border-white/10">
        <table className="w-full text-left text-sm">
          <thead className="bg-slate-900 text-xs uppercase tracking-wide text-slate-500">
            <tr>
              <th className="px-4 py-3">#</th>
              <th className="px-4 py-3">Endpoint</th>
              <th className="px-4 py-3">Canal</th>
              <th className="px-4 py-3">Status</th>
              <th className="px-4 py-3">Entregue em</th>
              <th className="px-4 py-3">Payload</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/5 bg-slate-950/60">
            {(data?.items ?? []).map((n) => (
              <tr key={n.id} className="hover:bg-white/[0.02]">
                <td className="px-4 py-2.5 text-xs text-slate-500">{n.id}</td>
                <td className="px-4 py-2.5 text-xs text-slate-300">
                  #{n.endpoint_id} — {eps?.items.find((e) => e.id === n.endpoint_id)?.name ?? ""}
                  {n.incident_id ? <span className="text-slate-500"> (inc #{n.incident_id})</span> : null}
                </td>
                <td className="px-4 py-2.5 text-xs text-slate-300">{n.channel}</td>
                <td className="px-4 py-2.5 text-xs">
                  <span
                    className={clsx(
                      "rounded-full px-2 py-0.5 font-medium",
                      n.status === "sent" ? "bg-emerald-500/15 text-emerald-300" : "bg-rose-500/15 text-rose-300",
                    )}
                  >
                    {n.status}
                  </span>
                </td>
                <td className="px-4 py-2.5 text-xs text-slate-400">{n.delivered_at ? fmtTime(n.delivered_at) : "—"}</td>
                <td className="px-4 py-2.5">
                  <button
                    onClick={() => setOpenId(openId === n.id ? null : n.id)}
                    className="rounded border border-white/10 px-2 py-1 text-xs text-slate-400 hover:bg-white/5"
                  >
                    {openId === n.id ? "ocultar" : "ver"}
                  </button>
                </td>
              </tr>
            ))}
            {isLoading && (
              <tr><td colSpan={6} className="px-4 py-8 text-center"><Spinner /></td></tr>
            )}
            {(data?.items ?? []).length === 0 && !isLoading && (
              <tr><td colSpan={6} className="px-4 py-8 text-center text-slate-500">Nenhuma notificação registrada.</td></tr>
            )}
          </tbody>
        </table>
      </div>

      {openId && (
        <pre className="thin-scroll max-h-64 overflow-auto rounded-xl border border-white/10 bg-slate-900 p-4 text-xs text-slate-300">
          {data?.items.find((n) => n.id === openId)?.payload}
        </pre>
      )}
    </div>
  );
}