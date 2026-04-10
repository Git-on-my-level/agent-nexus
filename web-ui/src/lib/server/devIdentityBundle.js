import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const bundlePath = path.join(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
  "..",
  "..",
  ".dev",
  "local-identities.json",
);

export async function readLocalDevIdentityBundle() {
  try {
    const raw = await readFile(bundlePath, "utf8");
    return JSON.parse(raw);
  } catch {
    return null;
  }
}

export function publicPersonasFromBundle(bundle) {
  return (bundle?.personas ?? []).map((p) => ({
    persona_id: p.persona_id,
    actor_id: p.actor_id,
    agent_id: p.agent_id,
    display_label: p.display_label,
    principal_kind: p.principal_kind,
    dev_bridge: p.dev_bridge,
  }));
}
