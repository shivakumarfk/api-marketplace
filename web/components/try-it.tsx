"use client";

import { useState } from "react";
import { buildRequest, pretty } from "@/lib/request";
import type { Endpoint } from "@/lib/types";
import { Button, Input, Textarea } from "./ui";

// TryIt is the mock test for one endpoint. It builds the exact request a
// customer would send (URL, headers, curl) from the values typed in, and
// answers with the provider's documented example. It never calls the
// provider and needs no key, so it is free and safe for anyone.
export function TryIt({ endpoint, gateway, slug }: { endpoint: Endpoint; gateway: string; slug: string }) {
  const [values, setValues] = useState<Record<string, string>>({});
  const [body, setBody] = useState(endpoint.request_example);
  const [result, setResult] = useState<null | { missing: string[] }>(null);
  const hasBody = endpoint.method !== "GET" && endpoint.method !== "DELETE";
  const built = buildRequest(gateway, slug, endpoint, values, hasBody ? body : undefined);
  const field = (key: string, example?: string) => (values[key] !== undefined ? values[key] : example ?? "");
  const set = (key: string, v: string) => { setValues((s) => ({ ...s, [key]: v })); setResult(null); };
  const inputs = [
    ...endpoint.params.map((p) => ({ key: `${p.in}:${p.name}`, label: p.name, tag: p.in, required: p.required, example: p.example })),
    ...endpoint.headers.map((h) => ({ key: `header:${h.name}`, label: h.name, tag: "header", required: h.required, example: h.example })),
  ];

  return (
    <div className="rounded-md border border-indigo-100 bg-indigo-50/40 p-4" data-testid={`try-${endpoint.method}-${endpoint.path}`}>
      <div className="mb-3 flex items-center justify-between">
        <h4 className="text-sm font-semibold text-gray-900">Try it (mock)</h4>
        <span className="text-xs text-gray-500">Free · no key needed · returns the documented example</span>
      </div>
      {inputs.length > 0 && (
        <div className="mb-3 grid gap-3 sm:grid-cols-2">
          {inputs.map((i) => (
            <label key={i.key} className="flex flex-col gap-1 text-sm">
              <span className="text-gray-700">
                <code className="font-mono">{i.label}</code> <span className="text-xs text-gray-500">{i.tag}{i.required ? " · required" : ""}</span>
              </span>
              <Input aria-label={`${i.label} (${i.tag})`} value={field(i.key, i.example)} onChange={(e) => set(i.key, e.target.value)} />
            </label>
          ))}
        </div>
      )}
      {hasBody && (
        <label className="mb-3 flex flex-col gap-1 text-sm">
          <span className="text-gray-700">Request body</span>
          <Textarea aria-label="Request body" rows={3} className="font-mono text-xs" value={body} onChange={(e) => { setBody(e.target.value); setResult(null); }} />
        </label>
      )}
      <p className="mb-1 text-xs font-medium uppercase tracking-wide text-gray-500">Request</p>
      <pre data-testid="try-curl" className="mb-3 overflow-x-auto rounded bg-gray-900 p-3 text-xs text-gray-100">{built.curl}</pre>
      <Button type="button" onClick={() => setResult({ missing: built.missing })}>Send to mock</Button>
      {result && (
        <div className="mt-3" data-testid="try-result">
          {result.missing.length > 0 ? (
            <p role="alert" className="text-sm text-red-700">Fill the required value{result.missing.length > 1 ? "s" : ""}: {result.missing.join(", ")}</p>
          ) : (
            <>
              <p className="mb-1 text-xs text-gray-600">
                <span data-testid="try-status" className="font-mono font-semibold">{endpoint.response_status}</span> · {endpoint.response_content_type}
                <span className="ml-2 text-gray-400">mock response; real calls go to the provider and are billed per your plan</span>
              </p>
              <pre data-testid="try-body" className="max-h-80 overflow-auto rounded bg-white p-3 text-xs ring-1 ring-gray-200">
                {pretty(endpoint.response_example, endpoint.response_content_type) || "(empty body)"}
              </pre>
            </>
          )}
        </div>
      )}
    </div>
  );
}
