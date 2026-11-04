import { cookies } from "next/headers";
import { NextResponse, type NextRequest } from "next/server";
import { SESSION_COOKIE } from "@/lib/session";

// Clears the session. POST from the Log out button; GET when a page finds the
// token rejected (a Server Component cannot delete cookies while rendering).
async function logout(req: NextRequest) {
  (await cookies()).delete(SESSION_COOKIE);
  const url = new URL("/admin/login", req.url);
  if (req.nextUrl.searchParams.get("expired") === "1") url.searchParams.set("expired", "1");
  return NextResponse.redirect(url, 303);
}

export const GET = logout;
export const POST = logout;
