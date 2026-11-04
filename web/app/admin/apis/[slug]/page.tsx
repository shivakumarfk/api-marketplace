import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { Suspense } from "react";
import { createEndpoint, createPlan as createPlanAction, deleteEndpoint, publishAPI, retirePlan, suspendAPI, updateAPI, updateEndpoint } from "@/app/admin/actions";
import Link from "next/link";
import { ActionButton, ApiForm, EndpointForm, PlanForm } from "@/components/forms";
import { Badge, Card, PageTitle, Skeleton, Table, Td } from "@/components/ui";
import { getAPI, listProviders } from "@/lib/controlplane";
import { ControlPlaneError } from "@/lib/errors";
import { planSummary, rateLimit, rupees } from "@/lib/format";

export const metadata: Metadata = { title: "API" };

export default function APIPage({ params }: PageProps<"/admin/apis/[slug]">) {
  return (
    <Suspense fallback={<Skeleton />}>
      <APIDetail params={params} />
    </Suspense>
  );
}

async function APIDetail({ params }: Pick<PageProps<"/admin/apis/[slug]">, "params">) {
  const { slug } = await params;
  let api;
  try {
    api = await getAPI(slug);
  } catch (e) {
    if (e instanceof ControlPlaneError && e.status === 404) notFound();
    throw e;
  }
  const providers = await listProviders();
  const provider = providers.find((p) => p.id === api.provider_id);
  const gateway = process.env.GATEWAY_PUBLIC_URL ?? "http://localhost:8080";

  return (
    <div className="flex flex-col gap-6">
      <PageTitle sub={<>Provider: {provider?.name ?? api.provider_id}{provider && !provider.agreement_signed_at && <span className="ml-2 text-amber-700">(agreement not signed)</span>} · Gateway: <code className="font-mono">{gateway}/{api.slug}/…</code></>}>
        <span className="mr-3">{api.name}</span><Badge status={api.status} />
      </PageTitle>

      <Card title="Status">
        <div className="flex flex-wrap gap-6">
          {api.status !== "published" && (
            <ActionButton action={publishAPI.bind(null, api.slug)} label="Publish" variant="primary" testId="publish" />
          )}
          {api.status === "published" && (
            <ActionButton action={suspendAPI.bind(null, api.slug)} label="Suspend" variant="danger" testId="suspend"
              confirm="Suspend this API? Customer calls will start failing within a few seconds." />
          )}
        </div>
      </Card>

      <Card title="Plans">
        <Table head={["Name", "Kind", "Customer pays", "Provider cost", "Rate limit", "Status", ""]} empty="No plans yet. Add one below to publish.">
          {api.plans.map((p) => (
            <tr key={p.id} data-testid={`plan-row-${p.name}`}>
              <Td>{p.name}</Td>
              <Td>{p.kind}</Td>
              <Td>{planSummary(p)}</Td>
              <Td>{rupees(p.provider_cost_per_call)}/call</Td>
              <Td>{rateLimit(p.rate_limit_rps)}</Td>
              <Td><Badge status={p.status} /></Td>
              <Td>{p.status === "active" && <ActionButton action={retirePlan.bind(null, p.id)} label="Retire" variant="danger" testId={`retire-${p.name}`} />}</Td>
            </tr>
          ))}
        </Table>
        <div className="mt-6 border-t border-gray-100 pt-5">
          <h3 className="mb-3 text-sm font-semibold text-gray-900">Add plan</h3>
          <PlanForm action={createPlanAction.bind(null, api.slug)} />
        </div>
      </Card>

      <Card title="Endpoints" actions={api.status === "published"
        ? <Link href={`/apis/${api.slug}`} className="text-sm text-indigo-700 hover:underline">View public page →</Link>
        : <span className="text-xs text-gray-500">Public page appears once published</span>}>
        <p className="mb-4 text-sm text-gray-500">The public contract customers see: what to send and what comes back. Examples also power Try it.</p>
        <div className="flex flex-col divide-y divide-gray-100">
          {api.endpoints.length === 0 && <p className="pb-4 text-sm text-gray-500">No endpoints documented yet.</p>}
          {api.endpoints.map((e) => (
            <details key={e.id} className="py-3" data-testid={`endpoint-row-${e.method}-${e.path}`}>
              <summary className="flex cursor-pointer items-center gap-3">
                <span className="w-16 font-mono text-xs font-semibold">{e.method}</span>
                <code className="font-mono text-sm">{e.path}</code>
                <span className="truncate text-sm text-gray-600">{e.summary}</span>
                <span className="ml-auto text-xs text-gray-400">{e.params.length} params · {e.response_status}</span>
              </summary>
              <div className="mt-4 flex flex-col gap-4">
                <EndpointForm key={JSON.stringify(e)} action={updateEndpoint.bind(null, e.id)} endpoint={e} submitLabel="Save endpoint" />
                <ActionButton action={deleteEndpoint.bind(null, e.id)} label="Delete endpoint" variant="danger"
                  confirm={`Delete ${e.method} ${e.path} from the docs?`} testId={`delete-${e.method}-${e.path}`} />
              </div>
            </details>
          ))}
        </div>
        <div className="mt-4 border-t border-gray-100 pt-5">
          <h3 className="mb-3 text-sm font-semibold text-gray-900">Add endpoint</h3>
          <div data-testid="add-endpoint">
            <EndpointForm action={createEndpoint.bind(null, api.slug)} submitLabel="Add endpoint" />
          </div>
        </div>
      </Card>

      <Card title="Listing and upstream">
        <ApiForm key={JSON.stringify(api)} mode="update" providers={providers} api={api} action={updateAPI.bind(null, api.slug)} gateway={gateway} />
      </Card>
    </div>
  );
}
