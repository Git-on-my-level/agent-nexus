import { localWorkScope, canUseLocalWorkScope } from "$lib/localWorkScope.js";
import {
  loadLocalWork,
  saveLocalWork,
  localWorkPrincipal,
} from "$lib/localWorkStore.js";

/** Checkpoint the entire composer, including reply/anchor/attachment context.
 * `value` must be plain JSON; File objects need a separate blob adapter.
 * `onReady` gates canonical initialization until device restoration settles.
 * Editors whose clicks only open/close UI can opt out with checkpointClicks. */
export function durableForm(node, options) {
  let scope = "";
  let generation = 0;
  let fingerprint = JSON.stringify(options.value);
  let disposed = false;
  let paused = false;
  let restoreTicket = 0;
  function warn(error) {
    node.dispatchEvent(
      new CustomEvent("local-save-error", {
        detail: error.message,
        bubbles: true,
      }),
    );
  }
  function save() {
    if (!options.target || !canUseLocalWorkScope(scope)) return;
    void saveLocalWork(scope, "draft", options.target, options.value).catch(
      warn,
    );
  }
  function checkpoint(event) {
    if (event?.type === "click" && options.checkpointClicks === false) return;
    paused = false;
    const value = options.read?.() ?? options.value;
    generation += 1;
    fingerprint = JSON.stringify(value);
    if (options.target && canUseLocalWorkScope(scope))
      void saveLocalWork(scope, "draft", options.target, value).catch(warn);
  }
  async function restore() {
    const ticket = ++restoreTicket;
    const version = ++generation;
    const before = fingerprint;
    options.onReady?.(false);
    try {
      if (!options.target || !canUseLocalWorkScope(scope)) return;
      const value = await loadLocalWork(scope, "draft", options.target);
      if (
        disposed ||
        version !== generation ||
        fingerprint !== before ||
        value === null
      )
        return;
      paused = false;
      fingerprint = JSON.stringify(value);
      options.onRestore(value);
    } catch (error) {
      warn(error);
    } finally {
      if (!disposed && ticket === restoreTicket) options.onReady?.(true);
    }
  }
  function purge(event) {
    if (event.detail?.principal !== localWorkPrincipal(scope)) return;
    generation += 1;
    paused = true;
    options.onRestore(null, "privacy");
    fingerprint = JSON.stringify(options.read?.() ?? options.value);
  }
  function edit(event) {
    if (
      event.detail?.scope !== scope ||
      event.detail?.target !== options.target
    )
      return;
    generation += 1;
    paused = false;
    event.preventDefault();
    options.onRestore(event.detail.value);
  }
  function confirmed(event) {
    if (
      event.detail?.scope !== scope ||
      (event.detail?.draftTarget || event.detail?.target) !== options.target
    )
      return;
    if (
      JSON.stringify(options.read?.() ?? options.value) !==
      JSON.stringify(event.detail.draft)
    )
      return;
    generation += 1;
    paused = true;
    options.onRestore(null, "confirmed", event.detail);
    fingerprint = JSON.stringify(options.read?.() ?? options.value);
  }
  const unsubscribe = localWorkScope.subscribe((next) => {
    const changed = scope && scope !== next;
    scope = next;
    if (changed) {
      paused = true;
      options.onRestore(null, "privacy");
      fingerprint = JSON.stringify(options.read?.() ?? options.value);
    }
    void restore();
  });
  window.addEventListener("anx:local-work-purged", purge);
  window.addEventListener("anx:edit-form", edit);
  window.addEventListener("anx:mutation-confirmed", confirmed);
  node.addEventListener("input", checkpoint);
  node.addEventListener("change", checkpoint);
  node.addEventListener("click", checkpoint);
  return {
    update(next) {
      const switched = next.target !== options.target;
      const changed = JSON.stringify(next.value) !== fingerprint;
      options = next;
      if (switched) {
        paused = true;
        options.onRestore(null);
        fingerprint = JSON.stringify(options.read?.() ?? options.value);
        void restore();
      } else if (changed) {
        generation += 1;
        fingerprint = JSON.stringify(next.value);
        if (!paused) save();
      }
    },
    destroy() {
      disposed = true;
      generation += 1;
      unsubscribe();
      window.removeEventListener("anx:local-work-purged", purge);
      window.removeEventListener("anx:edit-form", edit);
      window.removeEventListener("anx:mutation-confirmed", confirmed);
      node.removeEventListener("input", checkpoint);
      node.removeEventListener("change", checkpoint);
      node.removeEventListener("click", checkpoint);
    },
  };
}
