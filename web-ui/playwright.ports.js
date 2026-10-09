import { createHash } from "node:crypto";
import { cpus } from "node:os";
import { dirname } from "node:path";
import { fileURLToPath } from "node:url";

// Fixed e2e ports make parallel checkouts collide: with server reuse enabled, a
// run can silently exercise another worktree's servers. Derive one contiguous
// block per worktree instead, below the Linux (32768+) and macOS (49152+)
// ephemeral ranges so the OS never hands the same port to something else.
const BLOCK_FLOOR = 20000;
const BLOCK_SIZE = 10;
const BLOCKS = 1000;

// CI runs a single checkout per runner and pins nothing, so keep its ports.
const CI_PORTS = {
  webUi: 4173,
  basePath: 4176,
  core: 8000,
  pmUi: 4381,
  pmCore: 4382,
};
const SLOTS = { webUi: 0, basePath: 1, core: 2, pmUi: 3, pmCore: 4 };

const worktree = dirname(fileURLToPath(import.meta.url));
const digest = createHash("sha256").update(worktree).digest("hex").slice(0, 8);
const block = BLOCK_FLOOR + BLOCK_SIZE * (parseInt(digest, 16) % BLOCKS);

export const isCi = Boolean(process.env.CI);

/**
 * Resolve a server port: an explicit environment value wins, CI keeps its fixed
 * port, and everything else gets this worktree's slot. The resolved value is
 * written back to the environment because specs read these variables directly
 * and must agree with the servers the config starts.
 */
export function resolvePort(role, variable) {
  const configured = process.env[variable];
  if (configured) {
    const port = Number(configured);
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      throw new Error(`${variable} must be a port number, got "${configured}"`);
    }
    return port;
  }
  const port = isCi ? CI_PORTS[role] : block + SLOTS[role];
  process.env[variable] = String(port);
  return port;
}

/** Server reuse is opt-in: reuse hides stale SSR and cross-worktree servers. */
export function reuseExistingServer(variable) {
  return process.env[variable] === "1" || process.env[variable] === "true";
}

/**
 * Local worker cap. Each worker drives a browser against shared dev servers, so
 * Playwright's default (half the cores) starves the servers on a busy machine.
 */
export const localWorkers = Math.max(2, Math.floor(cpus().length / 4));
