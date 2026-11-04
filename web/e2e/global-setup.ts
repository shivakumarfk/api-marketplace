import { env } from "./env";

// Wait for every service the specs touch, so a cold `make up` doesn't flake.
export default async function globalSetup() {
  const targets = [`${env.web}/api/healthz`, `${env.controlPlane}/healthz`, `${env.gateway}/healthz`];
  const deadline = Date.now() + 90_000;
  for (const url of targets) {
    for (;;) {
      try {
        if ((await fetch(url)).ok) break;
      } catch {
        // not up yet
      }
      if (Date.now() > deadline) throw new Error(`${url} is not healthy; is the stack up (make up)?`);
      await new Promise((r) => setTimeout(r, 1000));
    }
  }
}
