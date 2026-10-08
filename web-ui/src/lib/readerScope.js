import { derived, get } from "svelte/store";
import { authenticatedAgent } from "$lib/authSession.js";
import { selectedActorId } from "$lib/actorSession.js";
import {
  currentOrganizationSlug,
  currentWorkspaceSlug,
} from "$lib/workspaceContext.js";

/**
 * Who a read belongs to: the workspace it was read from and the reader who
 * read it.
 *
 * Core applies the caller's resource access on every statement of a read, so a
 * cached or remembered result belongs to one identity and to nobody else. A
 * surface that holds on to a read — the ask outcome cache, the evidence
 * document panel, the titles it hands back — compares this key and drops what
 * it has when it changes. One definition, so those surfaces cannot disagree
 * about what "the same reader" means; `inboxResponseQueue` keeps the same tuple
 * for a response in flight.
 */

function scopeKey(organization, workspace, agent, actor) {
  return JSON.stringify([
    organization,
    workspace,
    agent?.agent_id || "",
    agent?.actor_id || actor || "",
  ]);
}

/** Reactive, for a component that has to re-read when the reader changes. */
export const readerScope = derived(
  [
    currentOrganizationSlug,
    currentWorkspaceSlug,
    authenticatedAgent,
    selectedActorId,
  ],
  ([organization, workspace, agent, actor]) =>
    scopeKey(organization, workspace, agent, actor),
);

/** The same key, read once, for a module without a reactive context. */
export function readerScopeKey() {
  return scopeKey(
    get(currentOrganizationSlug),
    get(currentWorkspaceSlug),
    get(authenticatedAgent),
    get(selectedActorId),
  );
}
