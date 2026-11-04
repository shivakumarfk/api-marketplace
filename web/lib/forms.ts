import type { ErrorInfo } from "./types";

// Pure helpers that turn submitted forms into control-plane request bodies.
// Money fields pass through as strings; the control plane parses them exactly.

type Result = { body: Record<string, unknown>; error?: undefined } | { body?: undefined; error: ErrorInfo };

const str = (fd: FormData, k: string) => {
  const v = fd.get(k);
  return typeof v === "string" ? v.trim() : "";
};

// formValues snapshots text fields so a form can be re-filled after an error.
// Secrets are never echoed back.
export function formValues(fd: FormData): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of fd.entries()) {
    if (typeof v === "string" && !k.startsWith("$") && k !== "secret" && k !== "token") out[k] = v;
  }
  return out;
}

function num(fd: FormData, k: string, opts: { integer?: boolean } = {}): number | ErrorInfo {
  const raw = str(fd, k);
  if (raw === "") return 0;
  const n = Number(raw);
  if (!Number.isFinite(n) || n < 0 || (opts.integer && !Number.isInteger(n))) {
    return { code: "INVALID_NUMBER", message: `${k} must be ${opts.integer ? "a whole number" : "a number"} of 0 or more` };
  }
  return n;
}

const isErr = (v: unknown): v is ErrorInfo => typeof v === "object" && v !== null && "code" in v;

function auth(fd: FormData): Record<string, string> {
  const type = str(fd, "auth_type") || "none";
  if (type === "none") return { type };
  const a: Record<string, string> = { type, header: str(fd, "auth_header") };
  const secret = fd.get("secret");
  if (typeof secret === "string" && secret !== "") a.secret = secret; // not trimmed: secrets are exact
  return a;
}

// apiBody builds the create (POST) or update (PATCH) body. On update, slug and
// provider are immutable and a blank secret keeps the stored one.
export function apiBody(fd: FormData, mode: "create" | "update"): Result {
  const rps = num(fd, "upstream_rate_limit_rps");
  if (isErr(rps)) return { error: rps };
  const body: Record<string, unknown> = {
    name: str(fd, "name"),
    description: str(fd, "description"),
    category: str(fd, "category"),
    base_url: str(fd, "base_url"),
    mock_url: str(fd, "mock_url"),
    auth: auth(fd),
    upstream_rate_limit_rps: rps,
  };
  if (mode === "create") {
    body.provider_id = str(fd, "provider_id");
    body.slug = str(fd, "slug");
  }
  return { body };
}

export function planBody(fd: FormData): Result {
  const included = num(fd, "included_calls", { integer: true });
  if (isErr(included)) return { error: included };
  const rps = num(fd, "rate_limit_rps");
  if (isErr(rps)) return { error: rps };
  const body: Record<string, unknown> = { name: str(fd, "name"), kind: str(fd, "kind"), included_calls: included, rate_limit_rps: rps };
  for (const k of ["monthly_fee", "price_per_call", "provider_cost_per_call"]) {
    const v = str(fd, k);
    if (v !== "") body[k] = v;
  }
  return { body };
}

// safeNext keeps post-login redirects inside the admin console.
export function safeNext(next: string | null | undefined): string {
  return next && /^\/admin\/[A-Za-z0-9\-_/]*$/.test(next) && !next.startsWith("/admin/log") ? next : "/admin/apis";
}
