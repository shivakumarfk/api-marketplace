import Link from "next/link";

export default function AdminLayout({ children }: LayoutProps<"/admin">) {
  return (
    <div className="flex flex-col gap-6">
      <nav className="flex items-center gap-4 border-b border-gray-200 pb-3 text-sm">
        <span className="rounded bg-indigo-50 px-2 py-0.5 text-xs font-semibold uppercase tracking-wide text-indigo-700">Admin</span>
        <Link href="/admin/apis" className="text-gray-700 hover:text-gray-900">APIs</Link>
        <Link href="/admin/providers" className="text-gray-700 hover:text-gray-900">Providers</Link>
        <form action="/admin/logout" method="post" className="ml-auto">
          <button type="submit" className="text-gray-500 hover:text-gray-900">Log out</button>
        </form>
      </nav>
      {children}
    </div>
  );
}
