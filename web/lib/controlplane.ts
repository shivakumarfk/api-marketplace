import "server-only";
import { redirect } from "next/navigation";
import { ControlPlaneError, parseError, UNREACHABLE } from "./errors";
import { getAdminToken } from "./session";
import type { AdminAPI, AdminPlan, CatalogAPI, Provider } from "./types";

// Data access layer for the control plane. Server-only: the admin token and
// the control plane's address never reach the browser.

const BASE = (process.env.CONTROL_PLANE_URL ?? "http://localhost:8081").replace(/\/$/, "");
const TIMEOUT_MS = 10_000;

type Method = "GET" | "POST" | "PATCH";

// request performs one call. It throws ControlPlaneError for any failure.
export async function request<T>(method: Method, path: string, opts: { token?: string; body?: unknown } = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" };
  if (opts.token) headers.Authorization = `Bearer ${opts.token}`;
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";
  let res: Response;
  try {
    res = await fetch(`${BASE}${path}`, {
      method,
      headers,
      body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
      cache: "no-store",
      signal: AbortSignal.timeout(TIMEOUT_MS),
    });
  } catch (e) {
    throw UNREACHABLE(e);
  }
  const text = await res.text();
  if (!res.ok) throw parseError(res.status, text);
  return (text ? JSON.parse(text) : {}) as T;
}

// admin calls the admin API with the session token. A missing or rejected
// token sends the user to log in again (the logout route clears the cookie,
// which a Server Component render cannot do itself).
async function admin<T>(method: Method, path: string, body?: unknown): Promise<T> {
  const token = await getAdminToken();
  if (!token) redirect("/admin/login");
  try {
    return await request<T>(method, path, { token, body });
  } catch (e) {
    if (e instanceof ControlPlaneError && e.status === 401) redirect("/admin/logout?expired=1");
    throw e;
  }
}

const seg = encodeURIComponent;

// ---------- public ----------

export async function listCatalog(): Promise<CatalogAPI[]> {
  return (await request<{ apis: CatalogAPI[] }>("GET", "/v1/catalog/apis")).apis ?? [];
}

// verifyToken checks an admin token without a session (used by login).
export async function verifyToken(token: string): Promise<boolean> {
  try {
    await request("GET", "/v1/admin/providers", { token });
    return true;
  } catch (e) {
    if (e instanceof ControlPlaneError && e.status === 401) return false;
    throw e;
  }
}

// ---------- admin ----------

export const listProviders = async () => (await admin<{ providers: Provider[] }>("GET", "/v1/admin/providers")).providers;
export const createProvider = (body: { name: string; contact_email?: string; agreement_signed: boolean }) =>
  admin<Provider>("POST", "/v1/admin/providers", body);
export const signAgreement = (id: string) => admin<Provider>("POST", `/v1/admin/providers/${seg(id)}/agreement`);

export const listAPIs = async () => (await admin<{ apis: AdminAPI[] }>("GET", "/v1/admin/apis")).apis;
export const getAPI = (slug: string) => admin<AdminAPI>("GET", `/v1/admin/apis/${seg(slug)}`);
export const createAPI = (body: Record<string, unknown>) => admin<AdminAPI>("POST", "/v1/admin/apis", body);
export const updateAPI = (slug: string, body: Record<string, unknown>) =>
  admin<AdminAPI>("PATCH", `/v1/admin/apis/${seg(slug)}`, body);
export const publishAPI = (slug: string) => admin<AdminAPI>("POST", `/v1/admin/apis/${seg(slug)}/publish`);
export const suspendAPI = (slug: string) => admin<AdminAPI>("POST", `/v1/admin/apis/${seg(slug)}/suspend`);

export const createPlan = (slug: string, body: Record<string, unknown>) =>
  admin<AdminPlan>("POST", `/v1/admin/apis/${seg(slug)}/plans`, body);
export const retirePlan = (id: string) => admin<AdminPlan>("POST", `/v1/admin/plans/${seg(id)}/retire`);
