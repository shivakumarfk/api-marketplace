// Addresses and seeded credentials of the local stack (deploy/seed/seed.sql).
export const env = {
  web: process.env.E2E_BASE_URL ?? "http://localhost:3000",
  controlPlane: process.env.CONTROL_PLANE_URL ?? "http://localhost:8081",
  gateway: process.env.GATEWAY_URL ?? "http://localhost:8080",
  adminToken: process.env.ADMIN_TOKEN ?? "dev-admin-token",
  // Upstream as seen from inside the Compose network (the gateway calls it).
  mockProviderURL: "http://mockprovider:9000",
  mockProviderHeader: "X-Provider-Key",
  mockProviderSecret: "dev-provider-secret",
  institutionOrg: "00000000-0000-0000-0000-0000000000d2",
  institutionKey: "mk_live_DemoInstitutionLive000000000000000000000001",
};

// unique gives every test its own names, so specs can run in parallel and be
// re-run on the same stack without `make reset`.
export const unique = (prefix = "e2e") => `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
