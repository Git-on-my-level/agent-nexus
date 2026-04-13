import { json } from "@sveltejs/kit";

import { env as privateEnv } from "$env/dynamic/private";
import { readLocalDevIdentityBundle } from "$lib/server/devIdentityBundle.js";
import { loadWorkspaceCatalog } from "$lib/server/workspaceCatalog.js";

function allowDevIdentityRoutes() {
  if (privateEnv.NODE_ENV === "production") {
    return false;
  }
  const catalog = loadWorkspaceCatalog(privateEnv);
  return catalog.devActorMode === true;
}

export async function GET() {
  if (!allowDevIdentityRoutes()) {
    return json(
      { persona: null },
      { headers: { "cache-control": "no-store" } },
    );
  }

  const bundle = await readLocalDevIdentityBundle();
  const defaultHuman = (bundle?.personas ?? []).find(
    (p) =>
      String(p.principal_kind ?? "").toLowerCase() === "human" &&
      p.default === true,
  );

  if (!defaultHuman) {
    return json(
      { persona: null },
      { headers: { "cache-control": "no-store" } },
    );
  }

  return json(
    {
      persona: {
        persona_id: defaultHuman.persona_id,
        actor_id: defaultHuman.actor_id,
      },
    },
    { headers: { "cache-control": "no-store" } },
  );
}
