# Web console

Public catalog and admin console for the API marketplace. See the root [README](../README.md#web-console-web) for how to run it and what it covers.

- `app/`: routes. `app/admin/actions.ts` holds every Server Action.
- `lib/controlplane.ts`: the only module that calls the control plane (server-only).
- `lib/forms.ts`, `lib/format.ts`, `lib/errors.ts`: pure helpers, unit-tested in `lib/lib.test.ts`.
- `e2e/`: Playwright specs against the running stack.

Environment: `CONTROL_PLANE_URL` (server-side address of the control plane), `GATEWAY_PUBLIC_URL` (shown to users), `COOKIE_SECURE=false` only for plain-http hosts other than localhost.
