import type { Metadata } from "next";
import Link from "next/link";
import { Suspense } from "react";
import { Badge, Card, PageTitle, Skeleton, Table, Td } from "@/components/ui";
import { listAPIs, listProviders } from "@/lib/controlplane";

export const metadata: Metadata = { title: "APIs" };

export default function APIsPage() {
  return (
    <>
      <PageTitle sub="Every API in any status. Drafts and suspended APIs are not callable.">APIs</PageTitle>
      <Card actions={<Link href="/admin/apis/new" className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-500">New API</Link>}>
        <Suspense fallback={<Skeleton />}>
          <APITable />
        </Suspense>
      </Card>
    </>
  );
}

async function APITable() {
  const [apis, providers] = await Promise.all([listAPIs(), listProviders()]);
  const providerName = new Map(providers.map((p) => [p.id, p.name]));
  return (
    <Table head={["Slug", "Name", "Provider", "Status", "Active plans", "Upstream auth"]} empty="No APIs yet.">
      {apis.map((a) => (
        <tr key={a.id} data-testid={`api-row-${a.slug}`}>
          <Td><Link href={`/admin/apis/${a.slug}`} className="font-mono text-indigo-700 hover:underline">{a.slug}</Link></Td>
          <Td>{a.name}</Td>
          <Td>{providerName.get(a.provider_id) ?? a.provider_id}</Td>
          <Td><Badge status={a.status} /></Td>
          <Td>{a.plans.filter((p) => p.status === "active").length}</Td>
          <Td>{a.auth.type === "api_key" ? <>header <code className="font-mono">{a.auth.header}</code></> : "none"}</Td>
        </tr>
      ))}
    </Table>
  );
}
