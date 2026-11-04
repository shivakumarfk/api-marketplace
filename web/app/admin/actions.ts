"use server";

import { refresh, updateTag } from "next/cache";
import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import * as cp from "@/lib/controlplane";
import { ControlPlaneError } from "@/lib/errors";
import { apiBody, endpointBody, formValues, planBody, safeNext } from "@/lib/forms";
import { cookieOptions, SESSION_COOKIE } from "@/lib/session";
import type { FormState } from "@/lib/types";

// Every action returns FormState for useActionState. Control-plane errors come
// back as {code, message} so the form can show them inline; redirects (from
// redirect() or an expired session) propagate untouched.

async function run(fd: FormData, fn: () => Promise<string | void>): Promise<FormState> {
  try {
    const ok = await fn();
    return { ok: ok ?? "Saved" };
  } catch (e) {
    if (e instanceof ControlPlaneError) return { error: e.info(), values: formValues(fd) };
    throw e;
  }
}

// ---------- session ----------

export async function login(_: FormState, fd: FormData): Promise<FormState> {
  const token = fd.get("token");
  if (typeof token !== "string" || token.trim() === "") {
    return { error: { code: "TOKEN_REQUIRED", message: "enter the admin token" } };
  }
  let valid: boolean;
  try {
    valid = await cp.verifyToken(token.trim());
  } catch (e) {
    if (e instanceof ControlPlaneError) return { error: e.info() };
    throw e;
  }
  if (!valid) return { error: { code: "UNAUTHORIZED", message: "that token was not accepted" } };
  (await cookies()).set(SESSION_COOKIE, token.trim(), cookieOptions());
  redirect(safeNext(fd.get("next") as string | null));
}

// ---------- providers ----------

export async function createProvider(_: FormState, fd: FormData): Promise<FormState> {
  return run(fd, async () => {
    const p = await cp.createProvider({
      name: String(fd.get("name") ?? ""),
      contact_email: String(fd.get("contact_email") ?? "").trim() || undefined,
      agreement_signed: fd.get("agreement_signed") === "on",
    });
    refresh();
    return `Created ${p.name}`;
  });
}

export async function signAgreement(providerId: string, _: FormState, fd: FormData): Promise<FormState> {
  return run(fd, async () => {
    await cp.signAgreement(providerId);
    refresh();
    return "Agreement signed";
  });
}

// ---------- APIs ----------

export async function createAPI(_: FormState, fd: FormData): Promise<FormState> {
  const r = apiBody(fd, "create");
  if (r.error) return { error: r.error, values: formValues(fd) };
  let slug = "";
  const state = await run(fd, async () => {
    slug = (await cp.createAPI(r.body)).slug;
  });
  if (state.error) return state;
  redirect(`/admin/apis/${encodeURIComponent(slug)}`);
}

export async function updateAPI(slug: string, _: FormState, fd: FormData): Promise<FormState> {
  const r = apiBody(fd, "update");
  if (r.error) return { error: r.error, values: formValues(fd) };
  return run(fd, async () => {
    const a = await cp.updateAPI(slug, r.body);
    if (a.status === "published") updateTag("catalog");
    refresh();
    return "Saved";
  });
}

export async function publishAPI(slug: string, _: FormState, fd: FormData): Promise<FormState> {
  return run(fd, async () => {
    await cp.publishAPI(slug);
    updateTag("catalog");
    refresh();
    return "Published. The gateway picks it up within a few seconds.";
  });
}

export async function suspendAPI(slug: string, _: FormState, fd: FormData): Promise<FormState> {
  return run(fd, async () => {
    await cp.suspendAPI(slug);
    updateTag("catalog");
    refresh();
    return "Suspended. Calls stop within a few seconds; subscriptions are kept.";
  });
}

// ---------- plans ----------

export async function createPlan(slug: string, _: FormState, fd: FormData): Promise<FormState> {
  const r = planBody(fd);
  if (r.error) return { error: r.error, values: formValues(fd) };
  return run(fd, async () => {
    const p = await cp.createPlan(slug, r.body);
    updateTag("catalog");
    refresh();
    return `Added plan ${p.name}`;
  });
}

export async function retirePlan(planId: string, _: FormState, fd: FormData): Promise<FormState> {
  return run(fd, async () => {
    await cp.retirePlan(planId);
    updateTag("catalog");
    refresh();
    return "Retired";
  });
}

// ---------- endpoints (documentation) ----------

export async function createEndpoint(slug: string, _: FormState, fd: FormData): Promise<FormState> {
  const r = endpointBody(fd);
  if (r.error) return { error: r.error, values: formValues(fd) };
  return run(fd, async () => {
    const e = await cp.createEndpoint(slug, r.body);
    updateTag("catalog");
    refresh();
    return `Added ${e.method} ${e.path}`;
  });
}

export async function updateEndpoint(id: string, _: FormState, fd: FormData): Promise<FormState> {
  const r = endpointBody(fd);
  if (r.error) return { error: r.error, values: formValues(fd) };
  return run(fd, async () => {
    await cp.updateEndpoint(id, r.body);
    updateTag("catalog");
    refresh();
    return "Saved";
  });
}

export async function deleteEndpoint(id: string, _: FormState, fd: FormData): Promise<FormState> {
  return run(fd, async () => {
    await cp.deleteEndpoint(id);
    updateTag("catalog");
    refresh();
    return "Deleted";
  });
}
