import type { Metadata } from "next";
import { Suspense } from "react";
import { LoginForm } from "@/components/forms";
import { Card, Skeleton } from "@/components/ui";

export const metadata: Metadata = { title: "Admin login" };

export default function LoginPage({ searchParams }: PageProps<"/admin/login">) {
  return (
    <div className="mx-auto w-full max-w-sm">
      <Card title="Admin login">
        <Suspense fallback={<Skeleton />}>
          <Login searchParams={searchParams} />
        </Suspense>
      </Card>
    </div>
  );
}

async function Login({ searchParams }: Pick<PageProps<"/admin/login">, "searchParams">) {
  const sp = await searchParams;
  const one = (v: string | string[] | undefined) => (Array.isArray(v) ? v[0] : v);
  return <LoginForm next={one(sp.next)} expired={one(sp.expired) === "1"} />;
}
