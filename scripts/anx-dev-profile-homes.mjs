#!/usr/bin/env node

import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { failWithPrefix, parseJson, requestJson } from "./seed-core-lib.mjs";

const prefix = "dev profile homes failed";
const repoRoot = path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const identityBundlePath =
  String(process.env.ANX_DEV_IDENTITY_BUNDLE ?? "").trim() ||
  path.join(repoRoot, "web-ui", ".dev", "local-identities.json");
const outputRoot =
  String(process.env.ANX_DEV_PROFILE_HOMES_DIR ?? "").trim() ||
  path.join(repoRoot, ".tmp", "anx-dev-profile-homes");
const baseUrl = String(
  process.env.ANX_CORE_BASE_URL ?? "http://127.0.0.1:8000",
).trim();
const includeHuman = process.env.ANX_DEV_PROFILE_INCLUDE_HUMAN === "1";
const onlyPersonas = new Set(
  String(process.env.ANX_DEV_PROFILE_PERSONAS ?? "")
    .split(",")
    .map((entry) => entry.trim())
    .filter(Boolean),
);

main().catch((error) => {
  failWithPrefix(prefix, error instanceof Error ? error.message : String(error));
});

async function main() {
  const bundle = parseJson(await readFile(identityBundlePath, "utf8"));
  const personas = Array.isArray(bundle?.personas) ? bundle.personas : [];
  const selected = personas.filter((persona) => {
    const personaID = String(persona?.persona_id ?? "").trim();
    if (!personaID || !String(persona?.refresh_token ?? "").trim()) {
      return false;
    }
    if (onlyPersonas.size > 0 && !onlyPersonas.has(personaID)) {
      return false;
    }
    return includeHuman || persona?.principal_kind !== "human";
  });

  if (selected.length === 0) {
    throw new Error(
      `no eligible seeded personas found in ${identityBundlePath}; run make serve with ANX_DEV_SEED_IDENTITIES=1 first`,
    );
  }

  await mkdir(outputRoot, { recursive: true });
  const entries = [];
  for (const persona of selected) {
    const personaID = String(persona.persona_id).trim();
    const tokens = await refreshSeededPersonaToken(persona);
    const homeDir = path.join(outputRoot, personaID);
    const profileDir = path.join(homeDir, ".config", "anx", "profiles");
    await mkdir(profileDir, { recursive: true });

    const now = new Date().toISOString();
    const profile = {
      version: 1,
      agent: personaID,
      base_url: baseUrl,
      username: persona.auth_username,
      agent_id: persona.agent_id,
      actor_id: persona.actor_id,
      key_id: "",
      private_key_path: "",
      access_token: tokens.access_token,
      refresh_token: tokens.refresh_token,
      token_type: tokens.token_type || "Bearer",
      access_token_expires_at: tokens.expires_at,
      core_instance_id: "core-local",
      updated_at: now,
      created_at: now,
    };
    const profilePath = path.join(profileDir, `${personaID}.json`);
    await writeFile(profilePath, `${JSON.stringify(profile, null, 2)}\n`);
    entries.push({
      persona_id: personaID,
      actor_id: persona.actor_id,
      home: homeDir,
      agent: personaID,
      profile_path: profilePath,
      command_env: `HOME=${homeDir}`,
      command_agent: `--agent ${personaID}`,
    });
  }

  const manifestPath = path.join(outputRoot, "manifest.json");
  await writeFile(
    manifestPath,
    `${JSON.stringify(
      {
        generated_at: new Date().toISOString(),
        source_bundle: identityBundlePath,
        base_url: baseUrl,
        personas: entries,
      },
      null,
      2,
    )}\n`,
  );

  console.log(`Wrote ${entries.length} dev profile homes to ${outputRoot}`);
  console.log(`Manifest: ${manifestPath}`);
  for (const entry of entries) {
    console.log(
      `${entry.persona_id}: HOME=${entry.home} anx --agent ${entry.agent} auth whoami`,
    );
  }
}

async function refreshSeededPersonaToken(persona) {
  const personaID = String(persona?.persona_id ?? "").trim() || "(unknown)";
  try {
    const body = await requestJson(baseUrl, "POST", "/auth/token", {
      grant_type: "refresh_token",
      refresh_token: persona.refresh_token,
    });
    const tokens = body?.tokens ?? {};
    const accessToken = String(tokens.access_token ?? "").trim();
    const refreshToken = String(tokens.refresh_token ?? "").trim();
    if (!accessToken || !refreshToken) {
      throw new Error("token response did not include access_token and refresh_token");
    }
    return {
      access_token: accessToken,
      refresh_token: refreshToken,
      token_type: String(tokens.token_type ?? "Bearer").trim() || "Bearer",
      expires_at: String(tokens.expires_at ?? "").trim(),
    };
  } catch (error) {
    const reason = error instanceof Error ? error.message : String(error);
    throw new Error(
      `persona ${personaID}: failed to exchange seeded refresh token. Re-run make serve to refresh web-ui/.dev/local-identities.json if this token was already used. ${reason}`,
    );
  }
}
