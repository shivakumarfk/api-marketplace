import type { ErrorInfo } from "./types";

// ControlPlaneError carries the control plane's stable error code (e.g.
// AGREEMENT_REQUIRED) so the UI can show it next to the action that failed.
export class ControlPlaneError extends Error {
  readonly status: number;
  readonly code: string;
  readonly requestId?: string;

  constructor(status: number, code: string, message: string, requestId?: string) {
    super(message);
    this.name = "ControlPlaneError";
    this.status = status;
    this.code = code;
    this.requestId = requestId;
  }

  info(): ErrorInfo {
    return { code: this.code, message: this.message, requestId: this.requestId };
  }
}

// parseError turns a non-2xx response body into a ControlPlaneError. The
// control plane answers {"error":{"code","message","request_id"}}; anything
// else (a proxy's HTML 502, an empty body) becomes a generic error.
export function parseError(status: number, body: string): ControlPlaneError {
  try {
    const parsed: unknown = JSON.parse(body);
    if (parsed && typeof parsed === "object" && "error" in parsed) {
      const e = (parsed as { error: unknown }).error;
      if (e && typeof e === "object") {
        const { code, message, request_id } = e as Record<string, unknown>;
        if (typeof code === "string" && typeof message === "string") {
          return new ControlPlaneError(status, code, message, typeof request_id === "string" ? request_id : undefined);
        }
      }
    }
  } catch {
    // not JSON: fall through
  }
  return new ControlPlaneError(status, `HTTP_${status}`, "the control plane returned an unexpected response");
}

export const UNREACHABLE = (cause: unknown) =>
  new ControlPlaneError(503, "CONTROL_PLANE_UNREACHABLE",
    `cannot reach the control plane${cause instanceof Error ? ` (${cause.message})` : ""}`);
