import { describe, expect, it } from "vitest";
import { ControlPlaneError, parseError } from "./errors";
import { apiBody, formValues, planBody, safeNext } from "./forms";
import { planSummary, rateLimit, rupees, statusTone } from "./format";

const fd = (o: Record<string, string>) => {
  const f = new FormData();
  for (const [k, v] of Object.entries(o)) f.append(k, v);
  return f;
};

describe("parseError", () => {
  it("reads the control-plane envelope", () => {
    const e = parseError(409, JSON.stringify({ error: { code: "AGREEMENT_REQUIRED", message: "sign first", request_id: "req_1" } }));
    expect(e).toBeInstanceOf(ControlPlaneError);
    expect(e.info()).toEqual({ code: "AGREEMENT_REQUIRED", message: "sign first", requestId: "req_1" });
    expect(e.status).toBe(409);
  });
  it("handles non-JSON and odd shapes", () => {
    expect(parseError(502, "<html>Bad Gateway</html>").code).toBe("HTTP_502");
    expect(parseError(500, "").code).toBe("HTTP_500");
    expect(parseError(400, JSON.stringify({ error: "nope" })).code).toBe("HTTP_400");
    expect(parseError(400, JSON.stringify({ error: { code: 1, message: "x" } })).code).toBe("HTTP_400");
  });
});

describe("format", () => {
  it("keeps money exact", () => {
    expect(rupees("0.000150")).toBe("₹0.000150");
    expect(rupees("₹0.02")).toBe("₹0.02");
  });
  it("summarises plans per kind", () => {
    expect(planSummary({ kind: "payg", monthly_fee: "0.00", included_calls: 0, price_per_call: "0.02" })).toBe("₹0.02 per call");
    expect(planSummary({ kind: "free", monthly_fee: "0.00", included_calls: 10000, price_per_call: "0.00" })).toBe("Free · 10,000 calls/month");
    expect(planSummary({ kind: "tier", monthly_fee: "499.00", included_calls: 10000, price_per_call: "0.00" })).toBe("₹499.00/month · 10,000 calls");
    expect(planSummary({ kind: "tier", monthly_fee: "₹499.00", included_calls: 100, price_per_call: "₹0.0100" })).toBe("₹499.00/month · 100 calls + ₹0.0100/call");
  });
  it("maps status and rate limits", () => {
    expect(statusTone("published")).toBe("green");
    expect(statusTone("draft")).toBe("amber");
    expect(statusTone("suspended")).toBe("red");
    expect(statusTone("retired")).toBe("gray");
    expect(rateLimit(0)).toBe("unlimited");
    expect(rateLimit(2.5)).toBe("2.5 req/s");
  });
});

describe("apiBody", () => {
  const base = { name: " IFSC ", base_url: "https://ifsc.razorpay.com", provider_id: "p1", slug: "ifsc" };
  it("builds a create body with auth none by default", () => {
    const r = apiBody(fd(base), "create");
    expect(r.body).toMatchObject({ name: "IFSC", slug: "ifsc", provider_id: "p1", auth: { type: "none" }, upstream_rate_limit_rps: 0 });
  });
  it("includes header and exact secret for api_key", () => {
    const r = apiBody(fd({ ...base, auth_type: "api_key", auth_header: "X-Key", secret: " s3cret " }), "create");
    expect(r.body?.auth).toEqual({ type: "api_key", header: "X-Key", secret: " s3cret " });
  });
  it("omits a blank secret on update and never sends slug/provider", () => {
    const r = apiBody(fd({ ...base, auth_type: "api_key", auth_header: "X-Key", secret: "" }), "update");
    expect(r.body?.auth).toEqual({ type: "api_key", header: "X-Key" });
    expect(r.body).not.toHaveProperty("slug");
    expect(r.body).not.toHaveProperty("provider_id");
  });
  it("rejects a bad number locally", () => {
    expect(apiBody(fd({ ...base, upstream_rate_limit_rps: "fast" }), "create").error?.code).toBe("INVALID_NUMBER");
    expect(apiBody(fd({ ...base, upstream_rate_limit_rps: "-1" }), "create").error?.code).toBe("INVALID_NUMBER");
  });
});

describe("planBody", () => {
  it("passes money through as strings", () => {
    const r = planBody(fd({ name: "PAYG", kind: "payg", price_per_call: "0.02", provider_cost_per_call: "", included_calls: "", rate_limit_rps: "2" }));
    expect(r.body).toEqual({ name: "PAYG", kind: "payg", price_per_call: "0.02", included_calls: 0, rate_limit_rps: 2 });
  });
  it("needs whole included_calls", () => {
    expect(planBody(fd({ name: "F", kind: "free", included_calls: "1.5" })).error?.code).toBe("INVALID_NUMBER");
  });
});

describe("formValues and safeNext", () => {
  it("never echoes secrets or tokens", () => {
    expect(formValues(fd({ name: "x", secret: "s", token: "t", $ACTION_ID: "1" }))).toEqual({ name: "x" });
  });
  it("keeps redirects inside the admin console", () => {
    expect(safeNext("/admin/apis/ifsc")).toBe("/admin/apis/ifsc");
    for (const bad of [null, "", "https://evil.example", "//evil.example", "/admin/logout", "/catalog", "/admin/x?y=1"]) {
      expect(safeNext(bad)).toBe("/admin/apis");
    }
  });
});
