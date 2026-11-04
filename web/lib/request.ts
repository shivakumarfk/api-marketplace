import type { Endpoint } from "./types";

// Builds the concrete request a customer would send for an endpoint, from
// the values typed into "Try it" (falling back to each parameter's example).

export interface Built {
  url: string;
  missing: string[]; // required params or headers with no value
  headers: [string, string][];
  curl: string;
}

const shellQuote = (s: string) => `'${s.replace(/'/g, `'\\''`)}'`;

export function buildRequest(gateway: string, slug: string, ep: Endpoint, values: Record<string, string> = {},
  body?: string, apiKey = "$API_KEY"): Built {
  const v = (k: string, example?: string) => (values[k] !== undefined ? values[k] : example ?? "");
  const missing: string[] = [];

  let path = ep.path;
  for (const p of ep.params.filter((p) => p.in === "path")) {
    const val = v(`path:${p.name}`, p.example);
    if (val === "") missing.push(p.name);
    path = path.replace(`{${p.name}}`, encodeURIComponent(val || `{${p.name}}`));
  }
  const q = new URLSearchParams();
  for (const p of ep.params.filter((p) => p.in === "query")) {
    const val = v(`query:${p.name}`, p.example);
    if (val !== "") q.append(p.name, val);
    else if (p.required) missing.push(p.name);
  }
  const qs = q.toString();
  const url = `${gateway.replace(/\/$/, "")}/${slug}${path === "/" ? "/" : path}${qs ? `?${qs}` : ""}`;

  const headers: [string, string][] = [["X-API-Key", apiKey]];
  for (const h of ep.headers) {
    const val = v(`header:${h.name}`, h.example);
    if (val !== "") headers.push([h.name, val]);
    else if (h.required) missing.push(h.name);
  }

  const reqBody = body ?? ep.request_example;
  const parts = ["curl"];
  if (ep.method !== "GET") parts.push(`-X ${ep.method}`);
  for (const [k, val] of headers) parts.push(`-H ${shellQuote(`${k}: ${val}`)}`);
  if (reqBody && ep.method !== "GET" && ep.method !== "DELETE") parts.push(`-d ${shellQuote(reqBody)}`);
  parts.push(shellQuote(url));
  return { url, missing, headers, curl: parts.join(" \\\n  ") };
}

// pretty formats JSON examples for display; anything else is shown as is.
export function pretty(body: string, contentType: string): string {
  if (!body || !contentType.includes("json")) return body;
  try {
    return JSON.stringify(JSON.parse(body), null, 2);
  } catch {
    return body;
  }
}
