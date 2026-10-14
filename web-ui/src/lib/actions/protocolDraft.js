import { derived } from "svelte/store";
import { localWorkScope, canUseLocalWorkScope } from "$lib/localWorkScope.js";
import { identityAttemptUncertain } from "$lib/identityAttemptJournal.js";
import { authenticatedAgent } from "$lib/authSession.js";
import { devActorMode } from "$lib/workspaceContext.js";
import {
  loadLocalWork,
  saveLocalWork,
  localWorkEpoch,
  localWorkPrincipal,
} from "$lib/localWorkStore.js";

// Explicitly declared non-secret fields only. Ceremonies never auto-send and
// credentials/challenges/private results never enter this store. Anonymous
// input has its own workspace namespace and is purged when an identity arrives.
const PUBLIC_PREFIX = "anx:public-input:v1:";
// Identity arrival also clears public work belonging to forms that were closed
// before sign-in. Restrict deletion to this workspace and this store's prefix.
let observingIdentity = false;
export function observePublicIdentity() {
  if (typeof window === "undefined" || observingIdentity) return;
  observingIdentity = true;
  derived(
    [localWorkScope, authenticatedAgent, devActorMode],
    ([scope]) => scope,
  ).subscribe((scope) => {
    if (!canUseLocalWorkScope(scope)) return;
    const prefix = `${PUBLIC_PREFIX}${JSON.stringify(JSON.parse(scope).slice(0, 2)).slice(0, -1)},`;
    try {
      for (const key of Object.keys(localStorage))
        if (key.startsWith(prefix)) localStorage.removeItem(key);
      for (const key of Object.keys(sessionStorage))
        if (key.startsWith(prefix)) sessionStorage.removeItem(key);
    } catch {
      /* A form's storage check still refuses an unjournaled send. */
    }
  });
}
function assertNonSecret(value) {
  if (!value || typeof value !== "object") return;
  for (const [field, nested] of Object.entries(value)) {
    if (
      /^(token|accessToken|refreshToken|pollToken|bootstrapToken|inviteToken|registrationToken|secret|secretValue|newValue|password|credential|credentials|challenge|sessionId|nonce|authorization|signature|privateKey|apiKey)$/i.test(
        field.replace(/[^a-z]/gi, ""),
      )
    )
      throw new Error(
        "Only non-secret fields may be saved for explicit resend.",
      );
    assertNonSecret(nested);
  }
}
export function protocolDraft(node, options) {
  observePublicIdentity();
  let scope = "",
    ticket = 0,
    generation = 0,
    disposed = false;
  let fingerprint = "";
  const pick = (value) =>
    Object.fromEntries(
      options.fields.map((field) => [field, value?.[field] ?? null]),
    );
  const key = () =>
    `${PUBLIC_PREFIX}${JSON.stringify([...JSON.parse(scope).slice(0, 2), options.target])}`;
  const value = () => pick(options.read());
  const tabKey = () => {
    const name = `${PUBLIC_PREFIX}tab`;
    let id = sessionStorage.getItem(name);
    if (!id) {
      id = crypto.randomUUID();
      sessionStorage.setItem(name, id);
    }
    return id;
  };
  const draftKey = () => `${key()}:draft:${tabKey()}`;
  const pointerKey = () => `${key()}:pending`;
  const journalKey = (requestKey) => `${key()}:request:${requestKey}`;
  function journals() {
    const prefix = `${key()}:request:`;
    return Object.keys(localStorage)
      .filter((name) => name.startsWith(prefix))
      .map((name) => JSON.parse(localStorage.getItem(name) || "null"))
      .filter(Boolean);
  }
  function pendingChoices() {
    options.onPending?.(journals().map(pick));
  }
  function publicStore(captured) {
    const requestKey = captured.request?.request_key;
    if (requestKey) {
      const name = journalKey(requestKey),
        known = JSON.parse(localStorage.getItem(name) || "null");
      if (
        known &&
        JSON.stringify(known.request) !== JSON.stringify(captured.request)
      )
        throw new Error("Saved request changed");
      localStorage.setItem(name, JSON.stringify(captured));
      sessionStorage.setItem(pointerKey(), requestKey);
    } else sessionStorage.removeItem(pointerKey());
    localStorage.setItem(draftKey(), JSON.stringify(captured));
    localStorage.setItem(key(), JSON.stringify(captured));
    pendingChoices();
  }
  function warning() {
    node.dispatchEvent(new CustomEvent("local-save-error", { bubbles: true }));
  }
  async function store(captured, capturedScope = scope) {
    assertNonSecret(captured);
    if (localWorkPrincipal(capturedScope))
      await saveLocalWork(capturedScope, "draft", options.target, captured);
    else {
      if (capturedScope !== scope) throw new Error("Workspace changed");
      publicStore(captured);
    }
  }
  function checkpoint() {
    generation++;
    const captured = value();
    fingerprint = JSON.stringify(captured);
    void store(captured).catch(warning);
  }
  const control = {
    async persist() {
      if (disposed) throw new Error("Identity changed");
      const captured = value(),
        capturedScope = scope,
        epoch = localWorkEpoch(scope);
      generation++;
      fingerprint = JSON.stringify(captured);
      await store(captured, capturedScope);
      if (
        disposed ||
        scope !== capturedScope ||
        epoch !== localWorkEpoch(scope)
      )
        throw new Error("Identity changed");
      return captured;
    },
    async clear(confirmed) {
      if (
        disposed ||
        JSON.stringify(value()) !== JSON.stringify(pick(confirmed))
      )
        return false;
      generation++;
      if (localWorkPrincipal(scope))
        await saveLocalWork(scope, "draft", options.target, null);
      else {
        // Another tab's checkpoint cannot be deleted by this confirmation.
        const encoded = JSON.stringify(pick(confirmed));
        if (localStorage.getItem(draftKey()) === encoded)
          localStorage.removeItem(draftKey());
        if (localStorage.getItem(key()) === encoded)
          localStorage.removeItem(key());
        const requestKey = confirmed.request?.request_key;
        if (
          requestKey &&
          localStorage.getItem(journalKey(requestKey)) === encoded
        )
          localStorage.removeItem(journalKey(requestKey));
        if (sessionStorage.getItem(pointerKey()) === requestKey)
          sessionStorage.removeItem(pointerKey());
        pendingChoices();
      }
      return true;
    },
  };
  fingerprint = JSON.stringify(value());
  options.onControl?.(control);
  const stop = localWorkScope.subscribe(async (next) => {
    const before = scope;
    scope = next;
    const current = ++ticket,
      version = ++generation;
    options.onReady?.(false);
    if (before && before !== scope) options.onRestore(null);
    try {
      // The public entry cannot be inherited by a signed-in person.
      if (localWorkPrincipal(scope)) {
        for (const name of Object.keys(localStorage))
          if (
            name === key() ||
            name.startsWith(`${key()}:request:`) ||
            name.startsWith(`${key()}:draft:`)
          )
            localStorage.removeItem(name);
        sessionStorage.removeItem(pointerKey());
        options.onPending?.([]);
      }
      const pointer =
        !localWorkPrincipal(scope) && sessionStorage.getItem(pointerKey());
      const fallback =
        !localWorkPrincipal(scope) &&
        JSON.parse(localStorage.getItem(key()) || "null");
      const loaded = localWorkPrincipal(scope)
        ? await loadLocalWork(scope, "draft", options.target)
        : JSON.parse(
            (pointer && localStorage.getItem(journalKey(pointer))) ||
              localStorage.getItem(draftKey()) ||
              "null",
          ) || (fallback?.request ? null : fallback);
      if (!localWorkPrincipal(scope)) pendingChoices();
      if (!disposed && current === ticket && version === generation && loaded) {
        const safe = pick(loaded);
        assertNonSecret(safe);
        if (options.target === "identity:create" && safe.request?.request_key)
          safe.uncertain = await identityAttemptUncertain(
            scope,
            safe.request.request_key,
            safe.uncertain === true,
          );
        if (!disposed && current === ticket && version === generation)
          options.onRestore(safe);
      }
      fingerprint = JSON.stringify(value());
    } catch {
      warning();
    } finally {
      if (!disposed && current === ticket) options.onReady?.(true);
    }
  });
  node.addEventListener("input", checkpoint);
  node.addEventListener("change", checkpoint);
  function purge(event) {
    if (event.detail?.principal !== localWorkPrincipal(scope)) return;
    generation++;
    options.onRestore(null);
    fingerprint = JSON.stringify(value());
  }
  async function changed(event) {
    if (localWorkPrincipal(scope)) return;
    if (event.key?.startsWith(`${key()}:request:`)) pendingChoices();
    const captured = value(),
      capturedScope = scope,
      version = generation;
    const requestKey = captured.request?.request_key;
    if (
      requestKey &&
      event.key?.startsWith(`${key()}:attempt:${requestKey}:`)
    ) {
      try {
        const uncertain = await identityAttemptUncertain(
          scope,
          requestKey,
          captured.uncertain === true,
        );
        if (!disposed && scope === capturedScope && generation === version)
          options.onRestore({ ...captured, uncertain });
      } catch {
        warning();
      }
    }
  }
  window.addEventListener("storage", changed);
  window.addEventListener("anx:local-work-purged", purge);
  return {
    update(next) {
      options = next;
      if (JSON.stringify(value()) !== fingerprint) checkpoint();
    },
    destroy() {
      disposed = true;
      ticket++;
      stop();
      node.removeEventListener("input", checkpoint);
      node.removeEventListener("change", checkpoint);
      window.removeEventListener("anx:local-work-purged", purge);
      window.removeEventListener("storage", changed);
    },
  };
}
