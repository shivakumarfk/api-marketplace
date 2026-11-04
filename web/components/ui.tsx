import type { ComponentProps, ReactNode } from "react";
import { statusTone, type Tone } from "@/lib/format";
import type { ErrorInfo } from "@/lib/types";

// Small Tailwind primitives, usable from Server and Client Components.

const cx = (...c: (string | false | undefined)[]) => c.filter(Boolean).join(" ");

export function Button({ variant = "primary", className, ...p }: ComponentProps<"button"> & { variant?: "primary" | "secondary" | "danger" }) {
  const v = {
    primary: "bg-indigo-600 text-white hover:bg-indigo-500 disabled:bg-indigo-300",
    secondary: "bg-white text-gray-800 ring-1 ring-gray-300 hover:bg-gray-50 disabled:text-gray-400",
    danger: "bg-white text-red-700 ring-1 ring-red-300 hover:bg-red-50 disabled:text-red-300",
  }[variant];
  return <button className={cx("rounded-md px-3 py-1.5 text-sm font-medium transition disabled:cursor-not-allowed", v, className)} {...p} />;
}

export function Field({ label, hint, children, htmlFor }: { label: string; hint?: ReactNode; children: ReactNode; htmlFor: string }) {
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={htmlFor} className="text-sm font-medium text-gray-700">{label}</label>
      {children}
      {hint && <p className="text-xs text-gray-500">{hint}</p>}
    </div>
  );
}

const inputCls = "rounded-md border border-gray-300 px-2.5 py-1.5 text-sm shadow-sm focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500 disabled:bg-gray-100";

export const Input = ({ className, ...p }: ComponentProps<"input">) => <input className={cx(inputCls, className)} {...p} />;
export const Select = ({ className, ...p }: ComponentProps<"select">) => <select className={cx(inputCls, className)} {...p} />;
export const Textarea = ({ className, ...p }: ComponentProps<"textarea">) => <textarea className={cx(inputCls, className)} {...p} />;

const tones: Record<Tone, string> = {
  green: "bg-green-50 text-green-700 ring-green-600/20",
  amber: "bg-amber-50 text-amber-800 ring-amber-600/20",
  red: "bg-red-50 text-red-700 ring-red-600/20",
  gray: "bg-gray-50 text-gray-600 ring-gray-500/20",
};

export function Badge({ status, children }: { status: string; children?: ReactNode }) {
  return (
    <span data-testid="status" className={cx("inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset", tones[statusTone(status)])}>
      {children ?? status}
    </span>
  );
}

export function ErrorNote({ error }: { error?: ErrorInfo }) {
  if (!error) return null;
  return (
    <div role="alert" className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800">
      <span className="font-mono text-xs font-semibold" data-testid="error-code">{error.code}</span>
      <span className="ml-2">{error.message}</span>
      {error.requestId && <span className="ml-2 font-mono text-xs text-red-500">({error.requestId})</span>}
    </div>
  );
}

export function OkNote({ ok }: { ok?: string }) {
  if (!ok) return null;
  return <p role="status" className="text-sm text-green-700">{ok}</p>;
}

export function Card({ title, children, actions }: { title?: string; children: ReactNode; actions?: ReactNode }) {
  return (
    <section className="rounded-lg border border-gray-200 bg-white p-5 shadow-sm">
      {(title || actions) && (
        <div className="mb-4 flex items-center justify-between gap-4">
          {title && <h2 className="text-base font-semibold text-gray-900">{title}</h2>}
          {actions}
        </div>
      )}
      {children}
    </section>
  );
}

export function Table({ head, children, empty }: { head: string[]; children: ReactNode; empty?: string }) {
  const rows = Array.isArray(children) ? children.length : children ? 1 : 0;
  return (
    <div className="overflow-x-auto">
      <table className="min-w-full divide-y divide-gray-200 text-sm">
        <thead>
          <tr>{head.map((h) => <th key={h} className="px-3 py-2 text-left font-medium text-gray-500">{h}</th>)}</tr>
        </thead>
        <tbody className="divide-y divide-gray-100">{children}</tbody>
      </table>
      {rows === 0 && empty && <p className="px-3 py-6 text-center text-sm text-gray-500">{empty}</p>}
    </div>
  );
}

export const Td = ({ className, ...p }: ComponentProps<"td">) => <td className={cx("px-3 py-2 align-top text-gray-800", className)} {...p} />;

export function PageTitle({ children, sub }: { children: ReactNode; sub?: ReactNode }) {
  return (
    <div className="mb-6">
      <h1 className="text-2xl font-semibold text-gray-900">{children}</h1>
      {sub && <p className="mt-1 text-sm text-gray-500">{sub}</p>}
    </div>
  );
}

export const Skeleton = ({ label = "Loading…" }: { label?: string }) => (
  <p className="animate-pulse text-sm text-gray-400">{label}</p>
);
