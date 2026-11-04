"use client";

import { useActionState, useState } from "react";
import * as actions from "@/app/admin/actions";
import type { AdminAPI, FormState, Provider } from "@/lib/types";
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

export function ApiForm({ mode, providers, api, action }: {
  mode: "create" | "update"; providers: Provider[]; api?: AdminAPI; action: BoundAction;
}) {
  const [state, formAction, pending] = useActionState(action, initial);
  return <ApiFormBody key={formKey(state)} mode={mode} providers={providers} api={api} state={state} formAction={formAction} pending={pending} />;
}

function ApiFormBody({ mode, providers, api, state, formAction, pending }: {
  mode: "create" | "update"; providers: Provider[]; api?: AdminAPI; state: FormState; formAction: (fd: FormData) => void; pending: boolean;
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
          <Field label="Slug" htmlFor="slug" hint="First path segment on the gateway; cannot change later.">
            <Input id="slug" name="slug" required pattern="[a-z0-9][a-z0-9\-]{1,62}" placeholder="ifsc" defaultValue={v("slug")} />
          </Field>
        </>
      ) : (
        <p className="text-sm text-gray-500 sm:col-span-2">
          Slug <code className="font-mono">{api?.slug}</code> and provider are fixed once created.
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
      <Field label="Upstream auth" htmlFor="auth_type">
        <Select id="auth_type" name="auth_type" value={authType} onChange={(e) => setAuthType(e.target.value)}>
          <option value="none">None</option>
          <option value="api_key">API key in a header</option>
        </Select>
      </Field>
      <Field label="Provider rate limit (req/s)" htmlFor="upstream_rate_limit_rps" hint="Shared across all customers. 0 = unlimited.">
        <Input id="upstream_rate_limit_rps" name="upstream_rate_limit_rps" inputMode="decimal" defaultValue={v("upstream_rate_limit_rps", api?.upstream_rate_limit_rps ?? 0)} />
      </Field>
      {authType === "api_key" && (
        <>
          <Field label="Header name" htmlFor="auth_header">
            <Input id="auth_header" name="auth_header" required placeholder="X-Api-Key" defaultValue={v("auth_header", api?.auth.header)} />
          </Field>
          <Field
            label="Provider secret"
            htmlFor="secret"
            hint={api?.auth.has_secret ? "A secret is stored. Leave blank to keep it; type a new one to rotate." : "Encrypted before storage; never shown again."}
          >
            <Input id="secret" name="secret" type="password" autoComplete="new-password"
              required={!(api?.auth.type === "api_key" && api.auth.has_secret)} />
          </Field>
        </>
      )}
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
