// Grupos — CRUD (RF-005, T4.3).
import { useState, type FormEvent } from "react";
import { useCreateGroup, useDeleteGroup, useGroups, useUpdateGroup } from "../../hooks/queries";
import type { Group } from "../../lib/types";
import { Spinner } from "../../components/Spinner";

export default function GroupsPage() {
  const { data, isLoading } = useGroups();
  const create = useCreateGroup();
  const update = useUpdateGroup();
  const del = useDeleteGroup();
  const [name, setName] = useState("");
  const [order, setOrder] = useState("0");
  const [editing, setEditing] = useState<Group | null>(null);
  const [error, setError] = useState<string | null>(null);

  if (isLoading) return <Spinner label="Carregando grupos…" />;

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      const g = { id: editing?.id ?? 0, name, display_order: Number(order) || 0 };
      if (editing) await update.mutateAsync(g);
      else await create.mutateAsync(g);
      setName("");
      setOrder("0");
      setEditing(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Falha ao salvar");
    }
  }

  return (
    <div className="space-y-4">
      <h1 className="text-lg font-semibold text-slate-100">Grupos de endpoints</h1>

      <form onSubmit={submit} className="flex flex-wrap items-end gap-2 rounded-xl border border-white/10 bg-slate-900/60 p-4">
        <label className="flex-1 min-w-40">
          <span className="text-xs font-medium text-slate-400">Nome</span>
          <input
            required
            maxLength={64}
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="mt-1 w-full rounded-lg border border-white/10 bg-slate-950 px-3 py-2 text-sm text-slate-100 outline-none focus:border-sky-400"
          />
        </label>
        <label>
          <span className="text-xs font-medium text-slate-400">Ordem</span>
          <input
            type="number"
            value={order}
            onChange={(e) => setOrder(e.target.value)}
            className="mt-1 w-24 rounded-lg border border-white/10 bg-slate-950 px-3 py-2 text-sm text-slate-100 outline-none focus:border-sky-400"
          />
        </label>
        <button type="submit" className="rounded-lg bg-sky-500 px-4 py-2 text-sm font-semibold text-white hover:bg-sky-400">
          {editing ? "Salvar" : "Criar"}
        </button>
        {editing && (
          <button
            type="button"
            onClick={() => {
              setEditing(null);
              setName("");
            }}
            className="rounded-lg px-3 py-2 text-sm text-slate-400 hover:text-slate-200"
          >
            Cancelar
          </button>
        )}
        {error && <p className="w-full text-sm text-rose-300">{error}</p>}
      </form>

      <ul className="divide-y divide-white/5 rounded-xl border border-white/10 bg-slate-900/40 text-sm">
        {(data?.items ?? []).map((g) => (
          <li key={g.id} className="flex items-center justify-between gap-3 px-4 py-3">
            <span className="font-medium text-slate-200">{g.name}</span>
            <div className="flex items-center gap-2 text-xs">
              <span className="text-slate-500">ordem {g.display_order}</span>
              <button
                onClick={() => {
                  setEditing(g);
                  setName(g.name);
                  setOrder(String(g.display_order));
                }}
                className="rounded border border-white/10 px-2 py-1 text-slate-300 hover:bg-white/5"
              >
                Editar
              </button>
              <button
                onClick={() => del.mutate(g.id)}
                className="rounded border border-white/10 px-2 py-1 text-slate-400 hover:bg-white/5"
              >
                Excluir
              </button>
            </div>
          </li>
        ))}
        {(data?.items ?? []).length === 0 && <li className="px-4 py-8 text-center text-slate-500">Sem grupos.</li>}
      </ul>
    </div>
  );
}