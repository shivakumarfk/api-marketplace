import type { AdminPlan, APIStatus, CatalogPlan } from "./types";

// rupees prefixes an exact decimal string with ₹. It never converts to a
// number, so "0.000150" stays exact.
export function rupees(decimal: string): string {
  return decimal.startsWith("₹") ? decimal : `₹${decimal}`;
}

const nf = new Intl.NumberFormat("en-IN");

// planSummary describes what a customer pays, per plan kind.
export function planSummary(p: Pick<AdminPlan | CatalogPlan, "kind" | "monthly_fee" | "included_calls" | "price_per_call">): string {
  const isZero = (s: string) => /^₹?0+(\.0+)?$/.test(s);
  switch (p.kind) {
    case "free":
      return `Free · ${nf.format(p.included_calls)} calls/month`;
    case "tier": {
      const base = `${rupees(p.monthly_fee)}/month · ${nf.format(p.included_calls)} calls`;
      return isZero(p.price_per_call) ? base : `${base} + ${rupees(p.price_per_call)}/call`;
    }
    case "payg":
      return `${rupees(p.price_per_call)} per call`;
    default:
      return p.kind;
  }
}

export type Tone = "green" | "amber" | "gray" | "red";

export function statusTone(status: APIStatus | string): Tone {
  switch (status) {
    case "published":
    case "active":
      return "green";
    case "draft":
      return "amber";
    case "suspended":
      return "red";
    default:
      return "gray";
  }
}

export function rateLimit(rps: number): string {
  return rps > 0 ? `${rps} req/s` : "unlimited";
}

export function formatDate(iso: string | null | undefined): string {
  if (!iso) return "";
  return new Date(iso).toLocaleDateString("en-IN", { day: "numeric", month: "short", year: "numeric", timeZone: "Asia/Kolkata" });
}
