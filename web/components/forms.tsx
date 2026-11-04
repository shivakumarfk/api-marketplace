"use client";

import { Fragment, useActionState, useId, useState } from "react";
import * as actions from "@/app/admin/actions";
import type { AdminAPI, Endpoint, FormState, Provider } from "@/lib/types";
import { Button, ErrorNote, Field, Input, OkNote, Select, Textarea } from "./ui";

const initial: FormState = {};

// After an error the action returns the submitted values; keying the form on
// them re-mounts it with those values, so React's post-action reset does not
// wipe what the user typed. Secrets are never returned, so they stay blank.
const formKey = (s: FormState) => (s.values ? JSON.stringify(s.values) : "initial");
const pick = (s: FormState, k: string, fallback = "") => s.values?.[k] ?? fallback;

type BoundAction = (state: FormState, fd: FormData) => Promise<FormState>;

// ActionButton is a one-button form (publish, suspend, retire, sign) that shows
// the control plane's answer next to it.
export function ActionButton({ action, label, variant = "secondary", confirm, testId }: {
  action: BoundAction; label: string; variant?: "primary" | "secondary" | "danger"; confirm?: string; testId?: string;
}) {
  const [state, formAction, pending] = useActionState(action, initial);
  return (
    <form
      action={formAction}
      onSubmit={(e) => { if (confirm && !window.confirm(confirm)) e.preventDefault(); }}
      className="flex flex-col items-start gap-2"
    >
      <Button type="submit" variant={variant} disabled={pending} data-testid={testId}>{pending ? "Working…" : label}</Button>
      <ErrorNote error={state.error} />
      <OkNote ok={state.ok} />
    </form>
  );
}

export function LoginForm({ next, expired }: { next?: string; expired?: boolean }) {
  const [state, formAction, pending] = useActionState(actions.login, initial);
  return (
    <form action={formAction} className="flex flex-col gap-4">
      {expired && !state.error && <p className="text-sm text-amber-700">Your session ended. Log in again.</p>}
      <input type="hidden" name="next" value={next ?? ""} />
      <Field label="Admin token" htmlFor="token" hint="Stage 0 only: the platform admin token. Google sign-in replaces this.">
        <Input id="token" name="token" type="password" autoComplete="current-password" required autoFocus />
      </Field>
      <ErrorNote error={state.error} />
      <Button type="submit" disabled={pending}>{pending ? "Checking…" : "Log in"}</Button>
    </form>
  );
}

export function ProviderForm() {
  const [state, formAction, pending] = useActionState(actions.createProvider, initial);
  return (
    <form key={formKey(state)} action={formAction} className="grid gap-4 sm:grid-cols-2">
      <Field label="Name" htmlFor="p-name">
        <Input id="p-name" name="name" required defaultValue={pick(state, "name")} />
      </Field>
      <Field label="Contact email" htmlFor="p-email">
        <Input id="p-email" name="contact_email" type="email" defaultValue={pick(state, "contact_email")} />
      </Field>
      <label className="flex items-center gap-2 text-sm text-gray-700 sm:col-span-2">
        <input type="checkbox" name="agreement_signed" defaultChecked={pick(state, "agreement_signed") === "on"} />
        Resale agreement already signed
      </label>
      <div className="flex flex-col gap-2 sm:col-span-2">
        <ErrorNote error={state.error} />
        <OkNote ok={state.ok} />
        <div><Button type="submit" disabled={pending}>Add provider</Button></div>
      </div>
    </form>
  );
}

export function ApiForm({ mode, providers, api, action, gateway }: {
  mode: "create" | "update"; providers: Provider[]; api?: AdminAPI; action: BoundAction; gateway: string;
}) {
  const [state, formAction, pending] = useActionState(action, initial);
  return <ApiFormBody key={formKey(state)} mode={mode} providers={providers} api={api} state={state} formAction={formAction} pending={pending} gateway={gateway} />;
}

function ApiFormBody({ mode, providers, api, state, formAction, pending, gateway }: {
  mode: "create" | "update"; providers: Provider[]; api?: AdminAPI; state: FormState; formAction: (fd: FormData) => void; pending: boolean; gateway: string;
}) {
  const [authType, setAuthType] = useState(pick(state, "auth_type", api?.auth.type ?? "none"));
  const v = (k: string, fallback?: string | number) => pick(state, k, fallback === undefined ? "" : String(fallback));
  return (
    <form action={formAction} className="grid gap-4 sm:grid-cols-2">
      {mode === "create" ? (
        <>
          <Field label="Provider" htmlFor="provider_id" hint={providers.length === 0 ? "Add a provider first." : undefined}>
            <Select id="provider_id" name="provider_id" required defaultValue={v("provider_id")}>
              <option value="">Choose…</option>
              {providers.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
            </Select>
          </Field>
          <Field label="API path" htmlFor="slug" hint={<>Customers call <code className="font-mono">{gateway}/&lt;API path&gt;/…</code>. Lowercase letters, digits and hyphens; cannot change later.</>}>
            <Input id="slug" name="slug" required pattern="[a-z0-9][a-z0-9\-]{1,62}" placeholder="ifsc" defaultValue={v("slug")} />
          </Field>
        </>
      ) : (
        <p className="text-sm text-gray-500 sm:col-span-2">
          API path <code className="font-mono">/{api?.slug}</code> and provider are fixed once created.
        </p>
      )}
      <Field label="Name" htmlFor="name">
        <Input id="name" name="name" required defaultValue={v("name", api?.name)} />
      </Field>
      <Field label="Category" htmlFor="category">
        <Input id="category" name="category" placeholder="other" defaultValue={v("category", api?.category)} />
      </Field>
      <div className="sm:col-span-2">
        <Field label="Description" htmlFor="description">
          <Textarea id="description" name="description" rows={2} defaultValue={v("description", api?.description)} />
        </Field>
      </div>
      <Field label="Base URL" htmlFor="base_url" hint="The gateway appends the customer's path and query.">
        <Input id="base_url" name="base_url" required placeholder="https://api.example.com/v1" defaultValue={v("base_url", api?.base_url)} />
      </Field>
      <Field label="Mock URL (sandbox keys)" htmlFor="mock_url">
        <Input id="mock_url" name="mock_url" defaultValue={v("mock_url", api?.mock_url)} />
      </Field>
      <Field label="Upstream auth" htmlFor="auth_type" hint="How the gateway authenticates to the provider. Customers never see this.">
        <Select id="auth_type" name="auth_type" value={authType} onChange={(e) => setAuthType(e.target.value)}>
          <option value="none">None</option>
          <option value="api_key">API key in a header</option>
          <option value="bearer">Bearer token (Authorization: Bearer …)</option>
          <option value="basic">Basic auth (user:password)</option>
          <option value="query">API key in a query parameter</option>
        </Select>
      </Field>
      <Field label="Provider rate limit (req/s)" htmlFor="upstream_rate_limit_rps" hint="Shared across all customers. 0 = unlimited.">
        <Input id="upstream_rate_limit_rps" name="upstream_rate_limit_rps" inputMode="decimal" defaultValue={v("upstream_rate_limit_rps", api?.upstream_rate_limit_rps ?? 0)} />
      </Field>
      {(authType === "api_key" || authType === "query") && (
        <Field label={authType === "api_key" ? "Header name" : "Query parameter name"} htmlFor="auth_header">
          <Input id="auth_header" name="auth_header" required placeholder={authType === "api_key" ? "X-Api-Key" : "api_key"}
            defaultValue={v("auth_header", api?.auth.type === authType ? api?.auth.header : "")} />
        </Field>
      )}
      {authType !== "none" && (
        <Field
          label={{ api_key: "Provider API key", query: "Provider API key", bearer: "Bearer token", basic: "user:password" }[authType] ?? "Provider secret"}
          htmlFor="secret"
          hint={secretKept(api, authType) ? "A secret is stored. Leave blank to keep it; type a new one to rotate." : "Encrypted before storage; never shown again."}
        >
          <Input id="secret" name="secret" type="password" autoComplete="new-password" required={!secretKept(api, authType)} />
        </Field>
      )}
      <div className="sm:col-span-2">
        <Field label="Fixed headers sent to the provider" htmlFor="upstream_headers"
          hint="Non-secret values added to every upstream call, e.g. a client id or API version. They override what customers send.">
          <RowEditor id="upstream_headers" name="upstream_headers_json"
            initial={rowsFrom(state, "upstream_headers_json", api?.upstream_headers ?? [])}
            columns={[{ key: "name", label: "Header", placeholder: "X-Client-Id" }, { key: "value", label: "Value", placeholder: "acme" }]}
            blank={{ name: "", value: "" }} addLabel="Add header" />
        </Field>
      </div>
      <div className="flex flex-col gap-2 sm:col-span-2">
        <ErrorNote error={state.error} />
        <OkNote ok={state.ok} />
        <div><Button type="submit" disabled={pending}>{mode === "create" ? "Create draft" : "Save changes"}</Button></div>
      </div>
    </form>
  );
}

export function PlanForm({ action }: { action: BoundAction }) {
  const [state, formAction, pending] = useActionState(action, initial);
  return <PlanFormBody key={formKey(state)} state={state} formAction={formAction} pending={pending} />;
}

function PlanFormBody({ state, formAction, pending }: { state: FormState; formAction: (fd: FormData) => void; pending: boolean }) {
  const [kind, setKind] = useState(pick(state, "kind", "payg"));
  const v = (k: string) => pick(state, k);
  const money = "Rupees, up to 6 decimals, e.g. 0.02";
  return (
    <form action={formAction} className="grid gap-4 sm:grid-cols-3">
      <Field label="Plan name" htmlFor="plan-name">
        <Input id="plan-name" name="name" required defaultValue={v("name")} />
      </Field>
      <Field label="Kind" htmlFor="kind">
        <Select id="kind" name="kind" value={kind} onChange={(e) => setKind(e.target.value)}>
          <option value="payg">Pay per call</option>
          <option value="tier">Monthly tier</option>
          <option value="free">Free</option>
        </Select>
      </Field>
      <Field label="Rate limit (req/s)" htmlFor="rate_limit_rps" hint="Per customer key. 0 = unlimited.">
        <Input id="rate_limit_rps" name="rate_limit_rps" inputMode="decimal" defaultValue={v("rate_limit_rps")} />
      </Field>
      {kind === "tier" && (
        <Field label="Monthly fee (₹)" htmlFor="monthly_fee" hint={money}>
          <Input id="monthly_fee" name="monthly_fee" inputMode="decimal" required defaultValue={v("monthly_fee")} />
        </Field>
      )}
      {kind !== "payg" && (
        <Field label="Included calls / month" htmlFor="included_calls">
          <Input id="included_calls" name="included_calls" inputMode="numeric" required defaultValue={v("included_calls")} />
        </Field>
      )}
      {kind !== "free" && (
        <Field label={kind === "tier" ? "Extra price per call (₹, optional)" : "Price per call (₹)"} htmlFor="price_per_call" hint={money}>
          <Input id="price_per_call" name="price_per_call" inputMode="decimal" required={kind === "payg"} defaultValue={v("price_per_call")} />
        </Field>
      )}
      <Field label="Provider cost per call (₹)" htmlFor="provider_cost_per_call" hint="What we pay the provider; price cannot be lower.">
        <Input id="provider_cost_per_call" name="provider_cost_per_call" inputMode="decimal" defaultValue={v("provider_cost_per_call")} />
      </Field>
      <div className="flex flex-col gap-2 sm:col-span-3">
        <ErrorNote error={state.error} />
        <OkNote ok={state.ok} />
        <div><Button type="submit" disabled={pending}>Add plan</Button></div>
      </div>
    </form>
  );
}

// An empty secret keeps the stored one when the type stays key-like (or basic stays basic).
function secretKept(api: AdminAPI | undefined, to: string): boolean {
  if (!api?.auth.has_secret) return false;
  const keyLike = ["api_key", "bearer", "query"];
  return (keyLike.includes(api.auth.type) && keyLike.includes(to)) || (api.auth.type === "basic" && to === "basic");
}

// rowsFrom restores a row editor after an error (from the submitted JSON),
// otherwise starts from the stored rows.
function rowsFrom<T>(state: FormState, field: string, stored: T[]): T[] {
  const raw = state.values?.[field];
  if (raw) {
    try {
      return JSON.parse(raw) as T[];
    } catch {
      // fall through to stored rows
    }
  }
  return stored;
}

type Column = { key: string; label: string; placeholder?: string; type?: "text" | "checkbox" | "select"; options?: string[]; width?: string };
type Row = Record<string, string | boolean>;

// RowEditor edits a list of small records (headers, parameters) and submits
// them as one JSON hidden field, so the server action gets a plain list.
export function RowEditor({ id, name, columns, initial, blank, addLabel }: {
  id: string; name: string; columns: Column[]; initial: object[]; blank: Row; addLabel: string;
}) {
  const [rows, setRows] = useState<Row[]>(initial as Row[]);
  const set = (i: number, key: string, val: string | boolean) =>
    setRows((rs) => rs.map((r, j) => (j === i ? { ...r, [key]: val } : r)));
  return (
    <div id={id} className="flex flex-col gap-2" data-testid={`rows-${id}`}>
      <input type="hidden" name={name} value={JSON.stringify(rows)} />
      {rows.length > 0 && (
        <div className="grid gap-2" style={{ gridTemplateColumns: `${columns.map((c) => c.width ?? "1fr").join(" ")} auto` }}>
          {columns.map((c) => <span key={c.key} className="text-xs font-medium text-gray-500">{c.label}</span>)}
          <span />
          {rows.map((r, i) => (
            <Fragment key={i}>
              {columns.map((c) => {
                const label = `${c.label} ${i + 1}`;
                if (c.type === "checkbox") {
                  return <input key={c.key} aria-label={label} type="checkbox" className="h-4 w-4 self-center"
                    checked={Boolean(r[c.key])} onChange={(e) => set(i, c.key, e.target.checked)} />;
                }
                if (c.type === "select") {
                  return <Select key={c.key} aria-label={label} value={String(r[c.key] ?? "")} onChange={(e) => set(i, c.key, e.target.value)}>
                    {c.options?.map((o) => <option key={o} value={o}>{o}</option>)}
                  </Select>;
                }
                return <Input key={c.key} aria-label={label} placeholder={c.placeholder} value={String(r[c.key] ?? "")}
                  onChange={(e) => set(i, c.key, e.target.value)} />;
              })}
              <Button type="button" variant="secondary" aria-label={`Remove row ${i + 1}`} onClick={() => setRows((rs) => rs.filter((_, j) => j !== i))}>✕</Button>
            </Fragment>
          ))}
        </div>
      )}
      <div><Button type="button" variant="secondary" onClick={() => setRows((rs) => [...rs, { ...blank }])}>{addLabel}</Button></div>
    </div>
  );
}

// EndpointForm documents one operation: what customers send and get back.
export function EndpointForm({ action, endpoint, submitLabel }: { action: BoundAction; endpoint?: Endpoint; submitLabel: string }) {
  const [state, formAction, pending] = useActionState(action, initial);
  return <EndpointFormBody key={formKey(state)} state={state} formAction={formAction} pending={pending} endpoint={endpoint} submitLabel={submitLabel} />;
}

function EndpointFormBody({ state, formAction, pending, endpoint, submitLabel }: {
  state: FormState; formAction: (fd: FormData) => void; pending: boolean; endpoint?: Endpoint; submitLabel: string;
}) {
  const uid = useId();
  const [method, setMethod] = useState(pick(state, "method", endpoint?.method ?? "GET"));
  const v = (k: string, fallback?: string | number) => pick(state, k, fallback === undefined ? "" : String(fallback));
  const hasBody = method !== "GET" && method !== "DELETE";
  return (
    <form action={formAction} className="grid gap-4 sm:grid-cols-6">
      <div className="sm:col-span-1">
        <Field label="Method" htmlFor={`${uid}-method`}>
          <Select id={`${uid}-method`} name="method" value={method} onChange={(e) => setMethod(e.target.value)}>
            {["GET", "POST", "PUT", "PATCH", "DELETE"].map((m) => <option key={m}>{m}</option>)}
          </Select>
        </Field>
      </div>
      <div className="sm:col-span-2">
        <Field label="Path" htmlFor={`${uid}-path`} hint={<>Relative to the API, e.g. <code className="font-mono">/branches/{"{ifsc}"}</code></>}>
          <Input id={`${uid}-path`} name="path" required placeholder="/forecast" defaultValue={v("path", endpoint?.path)} />
        </Field>
      </div>
      <div className="sm:col-span-3">
        <Field label="Summary" htmlFor={`${uid}-summary`}>
          <Input id={`${uid}-summary`} name="summary" placeholder="Weather forecast by coordinates" defaultValue={v("summary", endpoint?.summary)} />
        </Field>
      </div>
      <div className="sm:col-span-6">
        <Field label="Description" htmlFor={`${uid}-description`}>
          <Textarea id={`${uid}-description`} name="description" rows={2} defaultValue={v("description", endpoint?.description)} />
        </Field>
      </div>
      <div className="sm:col-span-6">
        <Field label="Parameters" htmlFor={`${uid}-params`} hint="Path parameters must appear in the path as {name}; they are always required.">
          <RowEditor id={`${uid}-params`} name="params_json" initial={rowsFrom(state, "params_json", endpoint?.params ?? [])}
            columns={[
              { key: "name", label: "Name", placeholder: "latitude" },
              { key: "in", label: "In", type: "select", options: ["query", "path"], width: "7rem" },
              { key: "required", label: "Required", type: "checkbox", width: "4.5rem" },
              { key: "description", label: "Description", placeholder: "Decimal degrees", width: "2fr" },
              { key: "example", label: "Example", placeholder: "12.97" },
            ]}
            blank={{ name: "", in: "query", required: false, description: "", example: "" }} addLabel="Add parameter" />
        </Field>
      </div>
      <div className="sm:col-span-6">
        <Field label="Headers the customer sends" htmlFor={`${uid}-headers`} hint="Besides X-API-Key, which every call needs.">
          <RowEditor id={`${uid}-headers`} name="headers_json" initial={rowsFrom(state, "headers_json", endpoint?.headers ?? [])}
            columns={[
              { key: "name", label: "Header", placeholder: "Content-Type" },
              { key: "required", label: "Required", type: "checkbox", width: "4.5rem" },
              { key: "description", label: "Description", width: "2fr" },
              { key: "example", label: "Example", placeholder: "application/json" },
            ]}
            blank={{ name: "", required: false, description: "", example: "" }} addLabel="Add header" />
        </Field>
      </div>
      {hasBody && (
        <div className="sm:col-span-6">
          <Field label="Request body example" htmlFor={`${uid}-request`}>
            <Textarea id={`${uid}-request`} name="request_example" rows={4} className="font-mono" defaultValue={v("request_example", endpoint?.request_example)} />
          </Field>
        </div>
      )}
      <div className="sm:col-span-1">
        <Field label="Response status" htmlFor={`${uid}-status`}>
          <Input id={`${uid}-status`} name="response_status" inputMode="numeric" defaultValue={v("response_status", endpoint?.response_status ?? 200)} />
        </Field>
      </div>
      <div className="sm:col-span-2">
        <Field label="Response content type" htmlFor={`${uid}-ctype`}>
          <Input id={`${uid}-ctype`} name="response_content_type" defaultValue={v("response_content_type", endpoint?.response_content_type ?? "application/json")} />
        </Field>
      </div>
      <div className="sm:col-span-6">
        <Field label="Response example" htmlFor={`${uid}-response`} hint="Shown in the docs and returned by Try it (the mock).">
          <Textarea id={`${uid}-response`} name="response_example" rows={5} className="font-mono" defaultValue={v("response_example", endpoint?.response_example)} />
        </Field>
      </div>
      <div className="flex flex-col gap-2 sm:col-span-6">
        <ErrorNote error={state.error} />
        <OkNote ok={state.ok} />
        <div><Button type="submit" disabled={pending}>{submitLabel}</Button></div>
      </div>
    </form>
  );
}
