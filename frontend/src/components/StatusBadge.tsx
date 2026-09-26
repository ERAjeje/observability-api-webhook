import type { StatusClass } from "../lib/types";
import clsx from "clsx";

const META: Record<StatusClass, { dot: string; text: string; label: string }> = {
  up: { dot: "bg-emerald-400", text: "text-emerald-300", label: "Operacional" },
  down: { dot: "bg-rose-500", text: "text-rose-300", label: "Indisponível" },
  degraded: { dot: "bg-amber-400", text: "text-amber-300", label: "Degradado" },
  unknown: { dot: "bg-slate-500", text: "text-slate-300", label: "Desconhecido" },
};

/** Badge de status acessível: o rótulo em texto acompanha a cor (WCAG AA). */
export function StatusBadge({ status, className }: { status: StatusClass; className?: string }) {
  const m = META[status] ?? META.unknown;
  return (
    <span
      className={clsx(
        "inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset ring-white/10",
        m.text,
        className,
      )}
    >
      <span aria-hidden="true" className={clsx("h-2 w-2 rounded-full", m.dot)} />
      {m.label}
    </span>
  );
}

export function StatusDot({ status, className }: { status: StatusClass; className?: string }) {
  const m = META[status] ?? META.unknown;
  return <span aria-hidden="true" className={clsx("inline-block h-2.5 w-2.5 rounded-full", m.dot, className)} />;
}

export const statusLabel = (s: StatusClass): string => (META[s] ?? META.unknown).label;