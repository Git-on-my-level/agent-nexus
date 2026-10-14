import { get } from "svelte/store";
import { readerScope } from "$lib/readerScope.js";
import { authenticatedAgent } from "$lib/authSession.js";
import { devActorMode } from "$lib/workspaceContext.js";
import {
  localWorkPrincipal,
  rememberLocalWorkOwner,
  lastLocalWorkOwner,
} from "$lib/localWorkStore.js";

export const localWorkScope = readerScope;
export const localWorkScopeKey = () => get(readerScope);
export function canUseLocalWorkScope(scope) {
  return Boolean(
    localWorkPrincipal(scope) && (get(authenticatedAgent) || get(devActorMode)),
  );
}

// Collapse auth/workspace store updates from one navigation. Intermediate
// identities during hydration must not be mistaken for a principal switch.
if (typeof window !== "undefined") {
  let previousScope = get(readerScope);
  let scheduled = false;
  readerScope.subscribe(() => {
    if (scheduled) return;
    scheduled = true;
    queueMicrotask(() => {
      scheduled = false;
      const next = get(readerScope);
      const before = JSON.parse(previousScope);
      const after = JSON.parse(next);
      const sameWorkspace = before[0] === after[0] && before[1] === after[1];
      previousScope = next;
      // An unavailable/expired session is not explicit sign-out. Keep its
      // namespaced work private until the same owner signs in, or another
      // validated owner replaces it.
      const previous = lastLocalWorkOwner(next);
      if (!get(authenticatedAgent) && previous && JSON.parse(previous)[2])
        return;
      // Routing can publish a cached identity for a different workspace.
      // Its verified session read, rather than this cache, adopts the owner.
      if (
        canUseLocalWorkScope(next) &&
        (!get(authenticatedAgent) || sameWorkspace)
      )
        rememberLocalWorkOwner(next);
    });
  });
}
