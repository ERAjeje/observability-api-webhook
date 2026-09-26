// Painel Endpoints — CRUD admin (RF-001..006, T4.3).
import { useState } from "react";
import {
  useDeleteEndpoint,
  useEndpoints,
  useGroups,
  useToggleEndpoint,
} from "../../hooks/queries";
import type { Endpoint } from "../../lib/types";
import { fmtLatency } from "../../lib/api";
import { StatusBadge } from "../../components/StatusBadge";
import { Spinner } from "../../components/Spinner";
import { EndpointForm } from "../../components/EndpointForm";

export default function EndpointsPage() {
  const { data: eps, isLoading } = useEndpoints();
  const { data: groups } = useGroups();
  const del = useDeleteEndpoint();
  const toggle = useToggleEndpoint();
  const [editing, setEditing] = useState<Endpoint | null>(null);
  const [creating, setCreating] = useState(false);
  const [confirmId, setConfirmId] = useState<number | null>(null);

  if (isLoading) return <Spinner label="Carregando endpoints…" />;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold text-slate-100">Endpoints monitorados</h1>
        <button
          onClick={() => {
            setCreating((v) => !v);
            setEditing(null);
          }}
          className="rounded-lg bg-sky-500 px-4 py-2 text-sm font-semibold text-white hover:bg-sky-400"
        >
          {creating ? "Cancelar" : "+ Novo endpoint"}
        </button>
      </div>

      {(creating || editing) && (
        <EndpointForm
          initial={editing}
          groups={groups?.items ?? []}
          onSaved={() => {
            setCreating(false);
            setEditing(null);
          }}
          onCancel={() => {
            setCreating(false);
            setEditing(null);
          }}
        />
      )}

      <div className="overflow-x-auto rounded-xl border border-white/10">
        <table className="w-full text-left text-sm">
          <thead className="bg-slate-900 text-xs uppercase tracking-wide text-slate-500">
            <tr>
              <th className="px-4 py-3">Status</th>
              <th className="px-4 py-3">Nome</th>
              <th className="px-4 py-3">URL</th>
              <th className="px-4 py-3">Intervalo</th>
              <th className="px-4 py-3">Grupo</th>
              <th className="px-4 py-3 text-right">Ações</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/5 bg-slate-950/60">
            {(eps?.items ?? []).map((ep) => (
              <tr key={ep.id} className="hover:bg-white/[0.02]">
                <td className="px-4 py-3">
                  <StatusBadge status={ep.status} />
                </td>
                <td className="px-4 py-3 font-medium text-slate-200">
                  {ep.name}
                  <span className="block text-xs font-normal text-slate-500">
                    {ep.active ? `${ep.interval_seconds}s` : "inativo"}
                  </span>
                </td>
                <td className="max-w-[260px] truncate px-4 py-3 text-xs text-slate-400">{ep.url}</td>
                <td className="px-4 py-3 text-xs text-slate-400">{fmtLatency(ep.timeout_ms)}</td>
                <td className="px-4 py-3 text-xs text-slate-400">
                  {groups?.items.find((g) => g.id === ep.group_id)?.name ?? "—"}
                </td>
                <td className="px-4 py-3">
                  <div className="flex justify-end gap-2 text-xs">
                    <button
                      onClick={() => {
                        setEditing(ep);
                        setCreating(false);
                      }}
                      className="rounded border border-white/10 px-2 py-1 text-slate-300 hover:bg-white/5"
                    >
                      Editar
                    </button>
                    <button
                      onClick={() =>
                        toggle.mutate({
                          id: ep.id,
                          body: {
                            name: ep.name,
                            url: ep.url,
                            method: ep.method,
                            headers: ep.headers,
                            interval_seconds: ep.interval_seconds,
                            timeout_ms: ep.timeout_ms,
                            active: !ep.active,
                          },
                        })
                      }
                      className="rounded border border-white/10 px-2 py-1 text-slate-300 hover:bg-white/5"
                    >
                      {ep.active ? "Desativar" : "Ativar"}
                    </button>
                    {confirmId === ep.id ? (
                      <button
                        onClick={() => {
                          del.mutate(ep.id);
                          setConfirmId(null);
                        }}
                        className="rounded bg-rose-500/20 px-2 py-1 text-rose-300 hover:bg-rose-500/30"
                      >
                        Confirmar?
                      </button>
                    ) : (
                      <button
                        onClick={() => setConfirmId(ep.id)}
                        className="rounded border border-white/10 px-2 py-1 text-slate-400 hover:bg-white/5"
                      >
                        Excluir
                      </button>
                    )}
                  </div>
                </td>
              </tr>
            ))}
            {(eps?.items ?? []).length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-slate-500">
                  Nenhum endpoint — crie o primeiro.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}