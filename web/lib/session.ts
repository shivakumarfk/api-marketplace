import "server-only";
import { cookies } from "next/headers";

// Stage 0 admin session: the admin token in an httpOnly cookie, readable only
// on the server. Google sign-in replaces this.
export const SESSION_COOKIE = "am_admin";
export const SESSION_MAX_AGE = 8 * 60 * 60; // seconds

export function cookieOptions() {
  return {
    httpOnly: true,
    sameSite: "strict" as const,
    // Browsers accept Secure cookies on http://localhost; set COOKIE_SECURE=false
    // only for plain-http hosts other than localhost.
    secure: process.env.COOKIE_SECURE !== "false",
    path: "/",
    maxAge: SESSION_MAX_AGE,
  };
}

export async function getAdminToken(): Promise<string | undefined> {
  return (await cookies()).get(SESSION_COOKIE)?.value || undefined;
}
