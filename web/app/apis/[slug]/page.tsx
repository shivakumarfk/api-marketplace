import type { Metadata } from "next";
import { cacheLife, cacheTag } from "next/cache";
import Link from "next/link";
import { notFound } from "next/navigation";
import { connection } from "next/server";
import { Suspense } from "react";
import { TryIt } from "@/components/try-it";
import { Card, Skeleton, Table, Td } from "@/components/ui";
import { getCatalogAPI } from "@/lib/controlplane";
import { ControlPlaneError } from "@/lib/errors";
import { planSummary, rateLimit } from "@/lib/format";
import { pretty } from "@/lib/request";
import type { CatalogDetail, Endpoint } from "@/lib/types";

const GATEWAY = process.env.GATEWAY_PUBLIC_URL ?? "http://localhost:8080";

export const metadata: Metadata = { title: "API" };

// Public contract of one API: how to call it (request details) and on what
// terms (plans, limits, billing). Nothing internal: no upstream URL, provider
// cost or credential. Same cache tag as the catalog, so admin edits show at once.
async function detail(slug: string): Promise<CatalogDetail> {
  "use cache";
  cacheTag("catalog");
  cacheLife({ stale: 10, revalidate: 10, expire: 60 });
  return getCatalogAPI(slug);
}

export default function APIDocPage({ params }: PageProps<"/apis/[slug]">) {
  return (
    <Suspense fallback={<Skeleton label="Loading API…" />}>
      <Doc params={params} />
    </Suspense>
  );
}

async function Doc({ params }: Pick<PageProps<"/apis/[slug]">, "params">) {
  await connection();
  const { slug } = await params;
  let api: CatalogDetail;
  try {
    api = await detail(slug);
  } catch (e) {
    if (e instanceof ControlPlaneError && e.status === 404) notFound();
    throw e;
  }
  const base = `${GATEWAY}/${api.slug}`;
  return (
    <div className="flex flex-col gap-6">
      <div>
        <Link href="/" className="text-sm text-indigo-700 hover:underline">← Catalog</Link>
        <div className="mt-2 flex items-baseline gap-3">
          <h1 className="text-2xl font-semibold text-gray-900">{api.name}</h1>
          <span className="rounded-full bg-gray-100 px-2 py-0.5 text-xs text-gray-600">{api.category}</span>
        </div>
        {api.description && <p className="mt-1 text-gray-600">{api.description}</p>}
      </div>

      <Card title="How to call it">
        <dl className="grid gap-3 text-sm sm:grid-cols-[10rem_1fr]">
          <dt className="text-gray-500">Base URL</dt>
          <dd><code data-testid="base-url" className="font-mono">{base}</code></dd>
          <dt className="text-gray-500">Authentication</dt>
          <dd>Send your platform key on every request: <code className="font-mono">X-API-Key: &lt;your key&gt;</code>. One key works for every API you subscribe to.</dd>
          <dt className="text-gray-500">Errors</dt>
          <dd>
            Platform errors are JSON: <code className="font-mono">{`{"error":{"code","message","request_id"}}`}</code>. Provider responses are passed through unchanged.
          </dd>
        </dl>
      </Card>

      <Card title="Plans and terms">
        <Table head={["Plan", "Price", "Rate limit"]} empty="No plans available.">
          {api.plans?.map((p) => (
            <tr key={p.id}>
              <Td>{p.name}</Td>
              <Td>{planSummary(p)}</Td>
              <Td>{rateLimit(p.rate_limit_rps)} per key</Td>
            </tr>
          ))}
        </Table>
        <ul className="mt-4 list-disc space-y-1 pl-5 text-sm text-gray-600">
          <li>Responses with status 2xx–4xx are billed. Provider 5xx errors and timeouts are not billed.</li>
          <li>Over the rate limit: <code className="font-mono">429 RATE_LIMITED</code> with <code className="font-mono">Retry-After</code>.</li>
          <li>Monthly quota used up: <code className="font-mono">429 QUOTA_EXCEEDED</code> until the next period or a plan upgrade.</li>
          <li>Prepaid balance too low: <code className="font-mono">402 INSUFFICIENT_BALANCE</code>.</li>
          <li>GET, PUT and DELETE calls are retried on provider errors; POST is never retried unless you send an <code className="font-mono">Idempotency-Key</code>.</li>
        </ul>
      </Card>

      <section className="flex flex-col gap-4">
        <h2 className="text-lg font-semibold text-gray-900">Endpoints</h2>
        {api.endpoints.length === 0 && (
          <p className="text-sm text-gray-500">The provider hasn&apos;t documented endpoints yet. Calls go to <code className="font-mono">{base}/…</code>.</p>
        )}
        {api.endpoints.map((e) => <EndpointDoc key={e.id} e={e} slug={api.slug} />)}
      </section>
    </div>
  );
}

const methodTone: Record<string, string> = {
  GET: "bg-sky-100 text-sky-800", POST: "bg-green-100 text-green-800", PUT: "bg-amber-100 text-amber-800",
  PATCH: "bg-amber-100 text-amber-800", DELETE: "bg-red-100 text-red-800",
};

function EndpointDoc({ e, slug }: { e: Endpoint; slug: string }) {
  return (
    <article id={`${e.method}-${e.path}`} data-testid={`endpoint-${e.method}-${e.path}`}>
      <Card>
        <div className="flex flex-wrap items-center gap-3">
          <span className={`rounded px-2 py-0.5 font-mono text-xs font-semibold ${methodTone[e.method] ?? "bg-gray-100"}`}>{e.method}</span>
          <code className="font-mono text-sm">/{slug}{e.path === "/" ? "/" : e.path}</code>
          {e.summary && <span className="text-sm text-gray-700">{e.summary}</span>}
        </div>
        {e.description && <p className="mt-2 text-sm text-gray-600">{e.description}</p>}

        <div className="mt-4">
          <h3 className="mb-1 text-sm font-semibold text-gray-900">Request</h3>
          <Table head={["Name", "In", "Required", "Description", "Example"]}>
            <tr>
              <Td className="whitespace-nowrap"><code className="font-mono">X-API-Key</code></Td><Td>header</Td><Td>yes</Td><Td>Your platform key</Td><Td />
            </tr>
            {e.params.map((p) => (
              <tr key={`${p.in}-${p.name}`}>
                <Td className="whitespace-nowrap"><code className="font-mono">{p.name}</code></Td><Td>{p.in}</Td><Td>{p.required ? "yes" : "no"}</Td>
                <Td>{p.description}</Td><Td><code className="break-all font-mono text-xs">{p.example}</code></Td>
              </tr>
            ))}
            {e.headers.map((h) => (
              <tr key={`h-${h.name}`}>
                <Td className="whitespace-nowrap"><code className="font-mono">{h.name}</code></Td><Td>header</Td><Td>{h.required ? "yes" : "no"}</Td>
                <Td>{h.description}</Td><Td><code className="break-all font-mono text-xs">{h.example}</code></Td>
              </tr>
            ))}
          </Table>
        </div>
        <div className="mt-6 grid gap-6 lg:grid-cols-2">
          <div className="flex min-w-0 flex-col gap-4">
            {e.request_example && (
              <div>
                <h3 className="mb-1 text-sm font-semibold text-gray-900">Request body example</h3>
                <pre className="overflow-x-auto rounded bg-gray-50 p-3 text-xs ring-1 ring-gray-200">{pretty(e.request_example, "application/json")}</pre>
              </div>
            )}
            <div>
              <h3 className="mb-1 text-sm font-semibold text-gray-900">
                Response <span className="font-mono text-xs text-gray-500">{e.response_status} · {e.response_content_type}</span>
              </h3>
              <pre className="max-h-96 overflow-auto rounded bg-gray-50 p-3 text-xs ring-1 ring-gray-200">
                {pretty(e.response_example, e.response_content_type) || "(empty body)"}
              </pre>
            </div>
          </div>
          <div className="min-w-0"><TryIt endpoint={e} gateway={GATEWAY} slug={slug} /></div>
        </div>
      </Card>
    </article>
  );
}
