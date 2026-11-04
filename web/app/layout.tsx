import type { Metadata } from "next";
import Link from "next/link";
import "./globals.css";

export const metadata: Metadata = {
  title: { default: "API Marketplace", template: "%s · API Marketplace" },
  description: "One key and one bill for third-party APIs, in INR.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className="h-full bg-gray-50 antialiased">
      <body className="flex min-h-full flex-col text-gray-900">
        <header className="border-b border-gray-200 bg-white">
          <nav className="mx-auto flex max-w-6xl items-center gap-6 px-6 py-3 text-sm">
            <Link href="/" className="font-semibold text-indigo-700">API Marketplace</Link>
            <Link href="/" className="text-gray-600 hover:text-gray-900">Catalog</Link>
            <Link href="/admin/apis" className="ml-auto text-gray-600 hover:text-gray-900">Admin</Link>
          </nav>
        </header>
        <main className="mx-auto w-full max-w-6xl flex-1 px-6 py-8">{children}</main>
      </body>
    </html>
  );
}
