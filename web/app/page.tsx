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
  // Every card has the same height and the same three bands (title,
  // description, request) with plans pinned to the bottom, so rows line up.
  return (
    <div className="grid auto-rows-fr gap-4 sm:grid-cols-2 lg:grid-cols-3">
      {apis.map((a) => {
        const plans = a.plans ?? [];
        const shown = plans.slice(0, 2);
        return (
          <article key={a.slug} data-testid={`catalog-${a.slug}`} className="h-full">
            <Card className="flex h-full flex-col">
              <div className="flex items-start justify-between gap-3">
                <h2 className="truncate font-semibold" title={a.name}>{a.name}</h2>
                <span className="shrink-0 rounded-full bg-gray-100 px-2 py-0.5 text-xs text-gray-600">{a.category}</span>
              </div>
              <p className="mt-2 line-clamp-2 min-h-10 text-sm text-gray-600">{a.description || "No description yet."}</p>
              <div className="mt-3 mb-4 rounded-md bg-gray-50 px-2.5 py-1.5">
                <p className="text-[11px] font-medium uppercase tracking-wide text-gray-500">Request</p>
                <p className="truncate font-mono text-xs text-gray-800" title={`${GATEWAY}/${a.slug}/…`}>{GATEWAY}/{a.slug}/…</p>
              </div>
              <ul className="mt-auto space-y-1 border-t border-gray-100 pt-3 text-sm">
                {shown.map((p) => (
                  <li key={p.id} className="flex items-baseline justify-between gap-2">
                    <span className="truncate text-gray-700">{p.name}</span>
                    <span className="shrink-0 text-right text-gray-900">{planSummary(p)} <span className="text-xs text-gray-500">· {rateLimit(p.rate_limit_rps)}</span></span>
                  </li>
                ))}
                {plans.length > shown.length && <li className="text-xs text-gray-500">+{plans.length - shown.length} more plan(s)</li>}
              </ul>
            </Card>
          </article>
        );
      })}
    </div>
  );
}
