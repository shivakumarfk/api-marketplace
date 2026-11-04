// Mirrors the control-plane JSON contract (internal/transport/http/controlplane).
// Money is always an exact decimal rupee string, e.g. "0.02"; never a JS number.

export type APIStatus = "draft" | "published" | "suspended" | "retired" | (string & {});
export type PlanKind = "free" | "tier" | "payg";

export interface Provider {
  id: string;
  name: string;
  contact_email?: string;
  agreement_signed_at: string | null;
  status: string;
  created_at: string;
}

export interface AdminPlan {
  id: string;
  name: string;
  kind: PlanKind;
  status: "active" | "retired" | (string & {});
  monthly_fee: string;
  included_calls: number;
  price_per_call: string;
  provider_cost_per_call: string;
  rate_limit_rps: number;
}

export type AuthType = "none" | "api_key" | "bearer" | "basic" | "query";

export interface UpstreamHeader {
  name: string;
  value: string;
}

export interface Param {
  name: string;
  in: "path" | "query";
  required: boolean;
  description?: string;
  example?: string;
}

export interface DocHeader {
  name: string;
  required: boolean;
  description?: string;
  example?: string;
}

// The documented contract of one operation. Public: holds no internal fields.
export interface Endpoint {
  id: string;
  method: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  path: string;
  summary: string;
  description: string;
  params: Param[];
  headers: DocHeader[];
  request_example: string;
  response_status: number;
  response_content_type: string;
  response_example: string;
  idempotent: boolean;
}

export interface AdminAPI {
  id: string;
  provider_id: string;
  slug: string;
  name: string;
  description: string;
  category: string;
  status: APIStatus;
  base_url: string;
  mock_url?: string;
  // header: header name (api_key) or query parameter name (query)
  auth: { type: AuthType | (string & {}); header?: string; has_secret: boolean };
  upstream_rate_limit_rps: number;
  upstream_headers: UpstreamHeader[];
  plans: AdminPlan[];
  endpoints: Endpoint[];
}

// Public catalog (GET /v1/catalog/apis). Prices here are pre-formatted with ₹.
export interface CatalogPlan {
  id: string;
  name: string;
  kind: PlanKind;
  monthly_fee: string;
  included_calls: number;
  price_per_call: string;
  rate_limit_rps: number;
}

export interface CatalogAPI {
  slug: string;
  name: string;
  description: string;
  category: string;
  plans: CatalogPlan[] | null;
}

export interface CatalogDetail extends CatalogAPI {
  endpoints: Endpoint[];
}

export interface ErrorInfo {
  code: string;
  message: string;
  requestId?: string;
}

// What every form action returns to useActionState.
export interface FormState {
  error?: ErrorInfo;
  ok?: string;
  values?: Record<string, string>;
}
