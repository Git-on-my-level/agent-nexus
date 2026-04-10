import { json } from "@sveltejs/kit";

import { env as privateEnv } from "$env/dynamic/private";
import {
  publicPersonasFromBundle,
  readLocalDevIdentityBundle,
} from "$lib/server/devIdentityBundle.js";
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
    return json({ error: { code: "not_found" } }, { status: 404 });
  }

  const bundle = await readLocalDevIdentityBundle();
  if (!bundle?.personas?.length) {
    return json({ personas: [] }, { headers: { "cache-control": "no-store" } });
  }

  return json(
    { personas: publicPersonasFromBundle(bundle) },
    { headers: { "cache-control": "no-store" } },
  );
}
