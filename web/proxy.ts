import { NextResponse, type NextRequest } from "next/server";

// Cheap gate: no session cookie → login page. The control plane still checks
// the token on every call; a rejected token clears the cookie (see /admin/logout).
export function proxy(req: NextRequest) {
  const { pathname } = req.nextUrl;
  if (pathname === "/admin/login" || pathname === "/admin/logout") return NextResponse.next();
  if (!req.cookies.get("am_admin")?.value) {
    const url = new URL("/admin/login", req.url);
    if (pathname !== "/admin") url.searchParams.set("next", pathname);
    return NextResponse.redirect(url);
  }
  return NextResponse.next();
}

export const config = { matcher: ["/admin", "/admin/:path*"] };
