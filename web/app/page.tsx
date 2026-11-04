import { cacheLife, cacheTag } from "next/cache";
import { connection } from "next/server";
import { Suspense } from "react";
import { Card, PageTitle, Skeleton } from "@/components/ui";
import { listCatalog } from "@/lib/controlplane";
import { ControlPlaneError } from "@/lib/errors";
import { planSummary, rateLimit } from "@/lib/format";
import type { CatalogAPI } from "@/lib/types";

const GATEWAY = process.env.GATEWAY_PUBLIC_URL ?? "http://localhost:8080";

// Cached on the server for a few seconds and tagged so admin publish/suspend
// actions refresh it at once. Only public data goes in this cache.
async function catalog(): Promise<CatalogAPI[]> {
  "use cache";
  cacheTag("catalog");
  cacheLife({ stale: 10, revalidate: 10, expire: 60 });
  return listCatalog();
}

export default function CatalogPage() {
  return (
    <>
      <PageTitle sub={<>Call any API with one key: <code className="font-mono">{GATEWAY}/&#123;api&#125;/&#123;path&#125;</code> with header <code className="font-mono">X-API-Key</code>.</>}>
        API catalog
      </PageTitle>
      <Suspense fallback={<Skeleton label="Loading catalog…" />}>
        <CatalogList />
      </Suspense>
    </>
  );
}

async function CatalogList() {
  // Render at request time: the control plane is not reachable during `next build`.
  await connection();
  let apis: CatalogAPI[];
  try {
    apis = await catalog();
  } catch (e) {
    const msg = e instanceof ControlPlaneError ? `${e.code}: ${e.message}` : "unexpected error";
    return <p role="alert" className="text-sm text-red-700">The catalog is unavailable right now ({msg}).</p>;
  }
  if (apis.length === 0) return <p className="text-sm text-gray-500">No APIs are published yet.</p>;
  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      {apis.map((a) => (
        <article key={a.slug} data-testid={`catalog-${a.slug}`}>
          <Card>
            <div className="flex items-baseline justify-between gap-2">
              <h2 className="font-semibold">{a.name}</h2>
              <span className="text-xs uppercase tracking-wide text-gray-500">{a.category}</span>
            </div>
            <p className="mt-1 font-mono text-xs text-indigo-700">/{a.slug}</p>
            {a.description && <p className="mt-2 text-sm text-gray-600">{a.description}</p>}
            <ul className="mt-3 space-y-1 text-sm">
              {(a.plans ?? []).map((p) => (
                <li key={p.id} className="flex justify-between gap-2">
                  <span className="text-gray-700">{p.name}</span>
                  <span className="text-right text-gray-900">{planSummary(p)} <span className="text-xs text-gray-500">· {rateLimit(p.rate_limit_rps)}</span></span>
                </li>
              ))}
            </ul>
          </Card>
        </article>
      ))}
    </div>
  );
}
