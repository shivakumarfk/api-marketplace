import type { Metadata } from "next";
import Link from "next/link";
import { Suspense } from "react";
import { createAPI } from "@/app/admin/actions";
import { ApiForm } from "@/components/forms";
import { Card, PageTitle, Skeleton } from "@/components/ui";
import { listProviders } from "@/lib/controlplane";

export const metadata: Metadata = { title: "New API" };

export default function NewAPIPage() {
  return (
    <>
      <PageTitle sub={<>New APIs start as drafts. Add a plan, then publish. <Link href="/admin/providers" className="text-indigo-700 hover:underline">Manage providers</Link></>}>
        New API
      </PageTitle>
      <Card>
        <Suspense fallback={<Skeleton />}>
          <Form />
        </Suspense>
      </Card>
    </>
  );
}

async function Form() {
  const providers = await listProviders();
  return <ApiForm mode="create" providers={providers} action={createAPI} />;
}
