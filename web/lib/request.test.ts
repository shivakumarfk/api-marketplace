import { describe, expect, it } from "vitest";
import { endpointBody } from "./forms";
import { buildRequest, pretty } from "./request";
import type { Endpoint } from "./types";

const ep = (o: Partial<Endpoint>): Endpoint => ({
  id: "e1", method: "GET", path: "/", summary: "", description: "", params: [], headers: [], request_example: "",
  response_status: 200, response_content_type: "application/json", response_example: "", idempotent: true, ...o,
});

describe("buildRequest", () => {
  const ifsc = ep({ path: "/{ifsc}", params: [{ name: "ifsc", in: "path", required: true, example: "HDFC0CAGSBK" }] });

  it("fills path params from examples and builds curl", () => {
    const b = buildRequest("http://localhost:8080", "ifsc", ifsc);
    expect(b.url).toBe("http://localhost:8080/ifsc/HDFC0CAGSBK");
    expect(b.missing).toEqual([]);
    expect(b.curl).toBe("curl \\\n  -H 'X-API-Key: $API_KEY' \\\n  'http://localhost:8080/ifsc/HDFC0CAGSBK'");
  });

  it("uses typed values, encodes them, and reports missing required ones", () => {
    const b = buildRequest("http://gw/", "ifsc", ifsc, { "path:ifsc": "a b/c" });
    expect(b.url).toBe("http://gw/ifsc/a%20b%2Fc");
    const m = buildRequest("http://gw", "ifsc", ifsc, { "path:ifsc": "" });
    expect(m.missing).toEqual(["ifsc"]);
  });

  it("adds query params and skips empty optional ones", () => {
    const meteo = ep({ path: "/forecast", params: [
      { name: "latitude", in: "query", required: true, example: "12.97" },
      { name: "timezone", in: "query", required: false },
    ] });
    expect(buildRequest("http://gw", "open-meteo", meteo).url).toBe("http://gw/open-meteo/forecast?latitude=12.97");
    expect(buildRequest("http://gw", "open-meteo", meteo, { "query:latitude": "" }).missing).toEqual(["latitude"]);
  });

  it("includes method, headers and a shell-safe body", () => {
    const post = ep({ method: "POST", path: "/posts", request_example: `{"t":"it's"}`,
      headers: [{ name: "Content-Type", required: true, example: "application/json" }] });
    const b = buildRequest("http://gw", "jp", post);
    expect(b.headers).toEqual([["X-API-Key", "$API_KEY"], ["Content-Type", "application/json"]]);
    expect(b.curl).toContain("-X POST");
    expect(b.curl).toContain(`-d '{"t":"it'\\''s"}'`);
  });
});

describe("pretty", () => {
  it("indents JSON and leaves other bodies alone", () => {
    expect(pretty(`{"a":1}`, "application/json")).toBe(`{\n  "a": 1\n}`);
    expect(pretty("not json", "application/json")).toBe("not json");
    expect(pretty("<b>x</b>", "text/html")).toBe("<b>x</b>");
  });
});

describe("endpointBody", () => {
  const fd = (o: Record<string, string>) => {
    const f = new FormData();
    for (const [k, v] of Object.entries(o)) f.append(k, v);
    return f;
  };
  it("parses row editors and drops blank rows", () => {
    const r = endpointBody(fd({
      method: "get", path: "/{id}", response_status: "200",
      params_json: JSON.stringify([{ name: "id", in: "path", required: true }, { name: " ", in: "query" }]),
      headers_json: "[]", request_example: "{}",
    }));
    expect(r.body).toMatchObject({ method: "GET", path: "/{id}", params: [{ name: "id" }], headers: [], request_example: "" });
  });
  it("rejects broken row JSON", () => {
    expect(endpointBody(fd({ method: "GET", path: "/", params_json: "{" })).error?.code).toBe("INVALID_ROWS");
  });
});
