import type { Metadata } from "next";
import { Suspense } from "react";
import { signAgreement } from "@/app/admin/actions";
import { ActionButton, ProviderForm } from "@/components/forms";
import { Card, PageTitle, Skeleton, Table, Td } from "@/components/ui";
import { listProviders } from "@/lib/controlplane";
import { formatDate } from "@/lib/format";

export const metadata: Metadata = { title: "Providers" };

export default function ProvidersPage() {
  return (
    <div className="flex flex-col gap-6">
      <PageTitle sub="An API can only be published once its provider has signed the resale agreement.">Providers</PageTitle>
      <Card>
        <Suspense fallback={<Skeleton />}>
          <ProviderTable />
        </Suspense>
      </Card>
      <Card title="Add provider">
        <ProviderForm />
      </Card>
    </div>
  );
}

async function ProviderTable() {
  const providers = await listProviders();
  return (
    <Table head={["Name", "Contact", "Resale agreement", "Status"]} empty="No providers yet.">
      {providers.map((p) => (
        <tr key={p.id} data-testid={`provider-row-${p.name}`}>
          <Td>{p.name}</Td>
          <Td>{p.contact_email}</Td>
          <Td>
            {p.agreement_signed_at
              ? <span className="text-green-700">Signed {formatDate(p.agreement_signed_at)}</span>
              : <ActionButton action={signAgreement.bind(null, p.id)} label="Sign agreement" testId={`sign-${p.name}`} />}
          </Td>
          <Td>{p.status}</Td>
        </tr>
      ))}
    </Table>
  );
}
